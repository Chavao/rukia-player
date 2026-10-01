package player

import (
	"context"
	"fmt"
	"time"
)

// Close cancels the current run and waits for Run to finish. The daemon owns
// resource closure on cancellation and internally discards its close errors;
// calling daemon.Close here would race its cancellation goroutine.
// A timeout leaves the run owned by the engine until it actually finishes.
func (e *Engine) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return e.close(ctx)
}

func (e *Engine) close(ctx context.Context) error {
	e.mu.Lock()
	cancel, done := e.cancel, e.doneCh
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done == nil {
		return nil
	}

	select {
	case <-done:
		e.mu.Lock()
		// Another caller may have closed this run and started a new one.
		if e.doneCh == done {
			e.app, e.cancel = nil, nil
		}
		e.mu.Unlock()
		return nil
	case <-ctx.Done():
		return fmt.Errorf("waiting for audio daemon shutdown: %w", ctx.Err())
	}
}
