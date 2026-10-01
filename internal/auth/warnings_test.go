package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestRuntimeRefreshPersistenceFailureRetainsWarningAndToken(t *testing.T) {
	var endpointCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpointCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"runtime-refreshed","token_type":"Bearer","expires_in":3600}`)
	}))
	defer server.Close()
	flow := NewOAuthFlow(expiredRefreshConfig())
	flow.config.Endpoint.TokenURL = server.URL
	persistenceErr := errors.New("disk unavailable")
	var saves int
	flow.saveToken = func(*oauth2.Token) error { saves++; return persistenceErr }
	observed := make(chan error, 1)
	flow.SetWarningSink(func(err error) { observed <- err })
	_, source := flow.Client(context.Background(), flow.appCfg.CurrentToken())
	for i := 0; i < 3; i++ {
		token, err := source.Token()
		if err != nil || token.AccessToken != "runtime-refreshed" || token.RefreshToken != "refresh" {
			t.Fatalf("token=%v err=%v", token, err)
		}
	}
	if saves != 1 || endpointCalls.Load() != 1 {
		t.Fatalf("saves=%d endpointCalls=%d", saves, endpointCalls.Load())
	}
	select {
	case warning := <-observed:
		if !errors.Is(warning, persistenceErr) {
			t.Fatalf("warning=%v", warning)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime persistence warning did not reach configured sink")
	}
	// The application subscribes after startup; the warning must survive that.
	select {
	case warning := <-flow.Warnings():
		if !errors.Is(warning, persistenceErr) {
			t.Fatalf("queued warning=%v", warning)
		}
	default:
		t.Fatal("warning disappeared before application subscription")
	}
}

func TestWarningSinkCannotBlockTokenRefresh(t *testing.T) {
	flow := NewOAuthFlow(DefaultConfig())
	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	flow.SetWarningSink(func(error) { close(entered); <-release })
	_, source := flow.Client(context.Background(), nil)
	pts := source.(*persistingTokenSource)
	pts.source = func(context.Context, *oauth2.Token) oauth2.TokenSource {
		return &staticTokenSource{tok: &oauth2.Token{AccessToken: "refreshed"}}
	}
	pts.saveToken = func(*oauth2.Token) error { return errors.New("cannot save") }
	finished := make(chan error, 1)
	go func() { _, err := pts.Token(); finished <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("warning sink was not invoked")
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("token refresh failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked warning sink blocked token refresh")
	}
	flow.SetWarningSink(nil)
	// Removing the observer during shutdown leaves channel delivery available.
	flow.warnings.publish(errors.New("shutdown warning"))
}

func TestWarningQueuePreservesLatestWithoutConsumer(t *testing.T) {
	flow := NewOAuthFlow(DefaultConfig())
	if warning := flow.LatestWarning(); warning != nil {
		t.Fatalf("new flow has warning: %v", warning)
	}
	flow.SetWarningSink(nil)
	latest := errors.New("latest failure")
	for i := 0; i < 100; i++ {
		flow.warnings.publish(fmt.Errorf("warning %d", i))
	}
	flow.warnings.publish(latest)
	if got := <-flow.Warnings(); got != latest {
		t.Fatalf("got=%v want=%v", got, latest)
	}
	// A Bubble Tea command may receive a warning just before the program exits
	// without applying its message. The final reminder must still be available.
	if got := flow.LatestWarning(); got != latest {
		t.Fatalf("consuming channel discarded final persistence reminder: %v", got)
	}
	flow.warnings.publish(nil)
	select {
	case warning := <-flow.Warnings():
		t.Fatalf("nil publication queued warning=%v", warning)
	default:
	}
	if got := flow.LatestWarning(); got != latest {
		t.Fatalf("nil publication discarded retained warning: %v", got)
	}
}

func TestPersistingTokenSourceSavesTokenRotationOnce(t *testing.T) {
	for _, fails := range []bool{false, true} {
		t.Run(fmt.Sprintf("persistence_failure=%v", fails), func(t *testing.T) {
			initial := &oauth2.Token{AccessToken: "same-access", RefreshToken: "old-refresh", Expiry: time.Now().Add(-time.Hour)}
			rotated := *initial
			rotated.RefreshToken = "rotated-refresh"
			rotated.Expiry = time.Now().Add(time.Hour)
			var saves int
			flow := NewOAuthFlow(DefaultConfig())
			current := &rotated
			pts := &persistingTokenSource{
				flow:    flow,
				parent:  context.Background(),
				lastTok: initial,
				source:  func(context.Context, *oauth2.Token) oauth2.TokenSource { return &staticTokenSource{tok: current} },
				saveToken: func(*oauth2.Token) error {
					saves++
					if fails {
						return errors.New("disk unavailable")
					}
					return nil
				},
			}
			for i := 0; i < 3; i++ {
				if _, err := pts.Token(); err != nil {
					t.Fatal(err)
				}
			}
			if saves != 1 {
				t.Fatalf("rotated token persistence attempts=%d want=1", saves)
			}
			newer := rotated
			newer.Expiry = rotated.Expiry.Add(time.Hour)
			current = &newer
			pts.mu.Lock()
			pts.lastTok.Expiry = time.Now().Add(-time.Hour)
			pts.mu.Unlock()
			if _, err := pts.Token(); err != nil {
				t.Fatal(err)
			}
			if saves != 2 {
				t.Fatalf("new token persistence attempts=%d want=2", saves)
			}
		})
	}
}
