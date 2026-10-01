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

func expiredRefreshConfig() *Config {
	return &Config{ClientID: "client-id", AuthFlow: "pkce", Token: &oauth2.Token{
		AccessToken: "expired", RefreshToken: "refresh", Expiry: time.Now().Add(-time.Hour),
	}}
}

func TestSilentRefreshClassifiesTokenEndpointFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		code   string
		login  bool
	}{
		{"revoked", http.StatusBadRequest, "invalid_grant", true},
		{"other OAuth failure", http.StatusBadRequest, "invalid_client", false},
		{"rate limit", http.StatusTooManyRequests, "temporarily_unavailable", false},
		{"internal failure", http.StatusInternalServerError, "server_error", false},
		{"bad gateway", http.StatusBadGateway, "server_error", false},
		{"unavailable", http.StatusServiceUnavailable, "temporarily_unavailable", false},
		{"rate limit with misleading grant code", http.StatusTooManyRequests, "invalid_grant", false},
		{"server failure with misleading grant code", http.StatusInternalServerError, "invalid_grant", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				fmt.Fprintf(w, `{"error":%q}`, tc.code)
			}))
			defer server.Close()
			flow := NewOAuthFlow(expiredRefreshConfig())
			flow.config.Endpoint.TokenURL = server.URL
			flow.saveToken = func(*oauth2.Token) error { t.Error("failed refresh should not save"); return nil }
			_, err := flow.SilentRefresh(context.Background())
			if errors.Is(err, ErrReauthenticationRequired) != tc.login {
				t.Fatalf("reauthentication classification = %v, want %v; err=%v", errors.Is(err, ErrReauthenticationRequired), tc.login, err)
			}
			if !tc.login {
				var retrieveErr *oauth2.RetrieveError
				if !errors.As(err, &retrieveErr) || retrieveErr.Response.StatusCode != tc.status {
					t.Fatalf("lost operational token endpoint failure: %v", err)
				}
			}
			var loginCalls int
			flow.loginFn = func(ctx context.Context) (*oauth2.Token, error) {
				loginCalls++
				// A short silent-refresh budget must not constrain human login.
				if _, deadline := ctx.Deadline(); deadline {
					t.Error("interactive login inherited silent-refresh deadline")
				}
				return &oauth2.Token{AccessToken: "interactive"}, nil
			}
			_, err = flow.EnsureToken(context.Background())
			if tc.login && (err != nil || loginCalls != 1) || !tc.login && (err == nil || loginCalls != 0) {
				t.Fatalf("loginCalls=%d err=%v", loginCalls, err)
			}
			if requests.Load() != 2 {
				t.Fatalf("token endpoint requests=%d, want 2 (one per refresh attempt)", requests.Load())
			}
		})
	}
}

func TestEnsureTokenValidWithoutRefreshToken(t *testing.T) {
	flow := NewOAuthFlow(&Config{Token: &oauth2.Token{AccessToken: "valid", Expiry: time.Now().Add(time.Hour)}})
	flow.loginFn = func(context.Context) (*oauth2.Token, error) { t.Fatal("valid token triggered login"); return nil, nil }
	flow.saveToken = func(*oauth2.Token) error { t.Fatal("valid token triggered persistence"); return nil }
	token, err := flow.EnsureToken(context.Background())
	if err != nil || token.AccessToken != "valid" {
		t.Fatalf("token=%v err=%v", token, err)
	}
}

func TestEnsureTokenPersistsRefreshExactlyOnce(t *testing.T) {
	for _, persistFails := range []bool{false, true} {
		t.Run(fmt.Sprintf("persistence_failure=%v", persistFails), func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			if persistFails {
				t.Setenv("XDG_CONFIG_HOME", "/dev/null/cannot-exist")
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"access_token":"refreshed","token_type":"Bearer","expires_in":3600}`)
			}))
			defer server.Close()
			cfg := expiredRefreshConfig()
			flow := NewOAuthFlow(cfg)
			flow.config.Endpoint.TokenURL = server.URL
			var saves int
			flow.saveToken = func(token *oauth2.Token) error { saves++; return cfg.SetToken(token) }
			flow.loginFn = func(context.Context) (*oauth2.Token, error) {
				t.Fatal("successful refresh triggered login")
				return nil, nil
			}
			for i := 0; i < 2; i++ {
				token, err := flow.EnsureToken(context.Background())
				if err != nil || token == nil || token.AccessToken != "refreshed" || token.RefreshToken != "refresh" {
					t.Fatalf("token=%v err=%v", token, err)
				}
			}
			if saves != 1 || requests.Load() != 1 || cfg.CurrentToken().AccessToken != "refreshed" {
				t.Fatalf("saves=%d requests=%d current token=%v", saves, requests.Load(), cfg.CurrentToken())
			}
			select {
			case warning := <-flow.Warnings():
				if !persistFails || warning == nil {
					t.Fatalf("unexpected warning: %v", warning)
				}
			default:
				if persistFails {
					t.Fatal("startup persistence warning was lost")
				}
			}
			_, runtimeSource := flow.Client(context.Background(), cfg.CurrentToken())
			if _, err := runtimeSource.Token(); err != nil || saves != 1 {
				t.Fatalf("runtime client re-saved startup token: saves=%d err=%v", saves, err)
			}
		})
	}
}

type refreshRoundTripper func(*http.Request) (*http.Response, error)

func (f refreshRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestEnsureTokenNetworkFailureDoesNotStartLogin(t *testing.T) {
	networkErr := errors.New("network unavailable")
	var requests int
	client := &http.Client{Transport: refreshRoundTripper(func(r *http.Request) (*http.Response, error) {
		requests++
		return nil, networkErr
	})}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, client)
	flow := NewOAuthFlow(expiredRefreshConfig())
	flow.loginFn = func(context.Context) (*oauth2.Token, error) {
		t.Fatal("network failure triggered login")
		return nil, nil
	}
	if token, err := flow.EnsureToken(ctx); token != nil || !errors.Is(err, networkErr) || requests != 1 {
		t.Fatalf("token=%v err=%v requests=%d", token, err, requests)
	}
}

func TestEnsureTokenRefreshCancellation(t *testing.T) {
	for _, preCanceled := range []bool{false, true} {
		t.Run(fmt.Sprintf("pre_canceled=%v", preCanceled), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered := make(chan struct{})
			client := &http.Client{Transport: refreshRoundTripper(func(r *http.Request) (*http.Response, error) {
				close(entered)
				<-r.Context().Done()
				return nil, r.Context().Err()
			})}
			ctx = context.WithValue(ctx, oauth2.HTTPClient, client)
			flow := NewOAuthFlow(expiredRefreshConfig())
			flow.loginFn = func(context.Context) (*oauth2.Token, error) {
				t.Error("canceled refresh triggered login")
				return nil, nil
			}
			if preCanceled {
				cancel()
			}
			finished := make(chan error, 1)
			go func() { _, err := flow.EnsureToken(ctx); finished <- err }()
			if !preCanceled {
				select {
				case <-entered:
				case <-time.After(time.Second):
					t.Fatal("refresh did not start")
				}
				cancel()
			}
			select {
			case err := <-finished:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("err=%v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("refresh ignored parent cancellation")
			}
		})
	}
}

func TestEnsureTokenRefreshDeadlineStopsBlockedEndpoint(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	flow := NewOAuthFlow(expiredRefreshConfig())
	flow.config.Endpoint.TokenURL = server.URL
	// This timeout is the behavior being tested; endpoint entry is established
	// by a barrier rather than assuming a sleep schedules the HTTP handler.
	flow.refreshTimeout = 100 * time.Millisecond
	flow.loginFn = func(context.Context) (*oauth2.Token, error) { t.Error("deadline triggered login"); return nil, nil }
	finished := make(chan error, 1)
	go func() { _, err := flow.EnsureToken(context.Background()); finished <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("refresh did not reach endpoint")
	}
	select {
	case err := <-finished:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("refresh escaped its startup latency budget")
	}
}

func TestEnsureTokenRefreshRequestsHaveExplicitDeadline(t *testing.T) {
	flow := NewOAuthFlow(expiredRefreshConfig())
	client := &http.Client{Transport: refreshRoundTripper(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > silentRefreshTimeout {
			t.Error("refresh request lacks bounded startup deadline")
		}
		return nil, errors.New("observed request")
	})}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, client)
	flow.loginFn = func(context.Context) (*oauth2.Token, error) {
		t.Fatal("operational failure triggered login")
		return nil, nil
	}
	if _, err := flow.EnsureToken(ctx); err == nil {
		t.Fatal("expected transport failure")
	}
}

func TestRuntimeRefreshRequestsHavePerOperationDeadline(t *testing.T) {
	for _, clientTimeout := range []time.Duration{0, time.Second, 2 * silentRefreshTimeout} {
		t.Run(clientTimeout.String(), func(t *testing.T) {
			flow := NewOAuthFlow(expiredRefreshConfig())
			wantTimeout := silentRefreshTimeout
			if clientTimeout > 0 && clientTimeout < wantTimeout {
				wantTimeout = clientTimeout
			}
			client := &http.Client{Timeout: clientTimeout, Transport: refreshRoundTripper(func(r *http.Request) (*http.Response, error) {
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) > wantTimeout {
					t.Error("runtime token request lacks the shortest refresh/client timeout")
				}
				return nil, errors.New("observed token request")
			})}
			ctx := context.WithValue(context.Background(), oauth2.HTTPClient, client)
			apiClient, source := flow.Client(ctx, flow.appCfg.CurrentToken())
			if _, err := source.Token(); err == nil {
				t.Fatal("expected token transport failure")
			}
			if client.Timeout != clientTimeout || apiClient.Timeout != clientTimeout {
				t.Fatalf("token deadline changed caller/API client timeout: caller=%v API=%v", client.Timeout, apiClient.Timeout)
			}
		})
	}
}

func TestRuntimeRefreshDeadlineStopsBlockedEndpoint(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	flow := NewOAuthFlow(expiredRefreshConfig())
	flow.config.Endpoint.TokenURL = server.URL
	flow.refreshTimeout = 100 * time.Millisecond
	_, source := flow.Client(context.Background(), flow.appCfg.CurrentToken())
	finished := make(chan error, 1)
	go func() { _, err := source.Token(); finished <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("runtime refresh did not reach endpoint")
	}
	select {
	case err := <-finished:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime refresh escaped its operation timeout")
	}
}

func TestRuntimeRefreshParentCancellationStopsConcurrentCalls(t *testing.T) {
	entered := make(chan struct{}, 1)
	client := &http.Client{Transport: refreshRoundTripper(func(r *http.Request) (*http.Response, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), oauth2.HTTPClient, client))
	defer cancel()
	flow := NewOAuthFlow(expiredRefreshConfig())
	_, source := flow.Client(ctx, flow.appCfg.CurrentToken())
	finished := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() { _, err := source.Token(); finished <- err }()
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("runtime refresh did not start")
	}
	cancel()
	for i := 0; i < 3; i++ {
		select {
		case err := <-finished:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err=%v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("pending token call ignored parent cancellation")
		}
	}
}
