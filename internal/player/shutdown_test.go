package player

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestEngineShutdownTimeoutPreservesRunOwnership(t *testing.T) {
	engine := NewEngine("test")
	ctx, cancel := context.WithCancel(context.Background())
	release := make(chan struct{})
	engine.mu.Lock()
	engine.startRun(ctx, cancel, nil, func(ctx context.Context) error {
		<-release
		return ctx.Err()
	})
	engine.mu.Unlock()
	done, errs := engine.Done(), engine.Errors()
	// An already expired deadline forces the timeout without timing assumptions.
	closeCtx, closeCancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer closeCancel()
	if err := engine.close(closeCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown timeout not reported: %v", err)
	}
	if ctx.Err() != context.Canceled || !engine.Running() || engine.Done() != done || engine.Errors() != errs {
		t.Fatal("timeout lost active run ownership or changed lifecycle channels")
	}
	if err := engine.Start(context.Background(), "username", "token"); err == nil {
		t.Fatal("timeout allowed another run while old daemon was active")
	}
	close(release)
	if err := engine.Close(); err != nil {
		t.Fatalf("retrying shutdown after run exit: %v", err)
	}
	if engine.Running() || engine.cancel != nil || engine.app != nil {
		t.Fatal("completed shutdown did not release run ownership")
	}
	if engine.Done() != done || engine.Errors() != errs {
		t.Fatal("shutdown replaced final run channels")
	}
}

func TestConcurrentEngineShutdownWaitsForSameRun(t *testing.T) {
	engine := NewEngine("test")
	ctx, cancel := context.WithCancel(context.Background())
	engine.mu.Lock()
	engine.startRun(ctx, cancel, nil, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	engine.mu.Unlock()
	done := engine.Done()
	start := make(chan struct{})
	errs := make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { <-start; errs <- engine.Close() })
	}
	close(start)
	wg.Wait()
	for range 8 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-done:
	default:
		t.Fatal("Close returned before Run finished")
	}
}

func TestEarlyEngineRunFailureCancelsDaemonContext(t *testing.T) {
	engine := NewEngine("test")
	ctx, cancel := context.WithCancel(context.Background())
	runErr := errors.New("early daemon failure")
	engine.mu.Lock()
	engine.startRun(ctx, cancel, nil, func(context.Context) error { return runErr })
	engine.mu.Unlock()
	select {
	case <-engine.Done():
	case <-time.After(time.Second):
		t.Fatal("early failed run never completed")
	}
	if ctx.Err() != context.Canceled {
		t.Fatal("early Run failure did not trigger daemon-owned cleanup")
	}
	if err := <-engine.Errors(); !errors.Is(err, runErr) {
		t.Fatalf("early error lost: %v", err)
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
}
