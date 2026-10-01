package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestGenerateRandomState(t *testing.T) {
	s1, err := GenerateRandomState()
	if err != nil {
		t.Fatalf("failed to generate random state: %v", err)
	}
	s2, err := GenerateRandomState()
	if err != nil {
		t.Fatalf("failed to generate random state: %v", err)
	}
	if s1 == "" || s2 == "" {
		t.Fatal("generated empty state")
	}
	if s1 == s2 {
		t.Fatal("generated identical states; expected random uniqueness")
	}
}

func TestGetAuthURL(t *testing.T) {
	cfg := &Config{
		ClientID:    "my_client_id",
		RedirectURI: "http://127.0.0.1:8443/callback",
	}

	flow := NewOAuthFlow(cfg)
	verifier := oauth2.GenerateVerifier()
	authURL := flow.GetAuthURL("sample_state", verifier)

	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("failed to parse auth URL: %v", err)
	}

	q := parsed.Query()
	if q.Get("client_id") != "my_client_id" {
		t.Errorf("expected client_id 'my_client_id', got %s", q.Get("client_id"))
	}
	if q.Get("redirect_uri") != "http://127.0.0.1:8443/callback" {
		t.Errorf("expected redirect_uri 'http://127.0.0.1:8443/callback', got %s", q.Get("redirect_uri"))
	}
	if q.Get("state") != "sample_state" {
		t.Errorf("expected state 'sample_state', got %s", q.Get("state"))
	}
	if q.Get("response_type") != "code" {
		t.Errorf("expected response_type 'code', got %s", q.Get("response_type"))
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") != oauth2.S256ChallengeFromVerifier(verifier) {
		t.Errorf("invalid PKCE challenge: %v", q)
	}
}

func TestPKCEExchangeAndRefresh(t *testing.T) {
	verifier := oauth2.GenerateVerifier()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Header.Get("Authorization") != "" || r.Form.Get("client_id") != "client-id" || r.Form.Get("client_secret") != "" {
			t.Errorf("unexpected PKCE authentication: header=%q form=%v", r.Header.Get("Authorization"), r.Form)
		}
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			if r.Form.Get("code_verifier") != verifier || r.Form.Get("redirect_uri") != DefaultRedirectURI {
				t.Errorf("invalid PKCE exchange: %v", r.Form)
			}
			fmt.Fprint(w, `{"access_token":"initial","refresh_token":"refresh","token_type":"Bearer","expires_in":1}`)
		case "refresh_token":
			if r.Form.Get("refresh_token") != "refresh" {
				t.Errorf("invalid PKCE refresh: %v", r.Form)
			}
			fmt.Fprint(w, `{"access_token":"refreshed","token_type":"Bearer","expires_in":3600}`)
		default:
			t.Errorf("unexpected grant: %v", r.Form)
		}
	}))
	defer server.Close()
	cfg := &Config{ClientID: "client-id", RedirectURI: DefaultRedirectURI, AuthFlow: "pkce"}
	flow := NewOAuthFlow(cfg)
	flow.config.Endpoint.TokenURL = server.URL
	tok, err := flow.Exchange(context.Background(), "code", verifier)
	if err != nil {
		t.Fatal(err)
	}
	tok.Expiry = time.Now().Add(-time.Second)
	refreshed, err := flow.TokenSource(context.Background(), tok).Token()
	if err != nil || refreshed.AccessToken != "refreshed" || calls.Load() != 2 {
		t.Fatalf("refresh=%v err=%v calls=%d", refreshed, err, calls.Load())
	}
}

func TestLegacyTokenRefreshUsesSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte("client-id:legacy-secret"))
		if r.Header.Get("Authorization") != want {
			t.Errorf("legacy Authorization = %q, want %q", r.Header.Get("Authorization"), want)
		}
		fmt.Fprint(w, `{"access_token":"refreshed","token_type":"Bearer","expires_in":3600}`)
	}))
	defer server.Close()
	cfg := &Config{ClientID: "client-id", ClientSecret: "legacy-secret"}
	flow := NewOAuthFlow(cfg)
	flow.config.Endpoint.TokenURL = server.URL
	tok := &oauth2.Token{AccessToken: "expired", RefreshToken: "refresh", Expiry: time.Now().Add(-time.Second)}
	if _, err := flow.TokenSource(context.Background(), tok).Token(); err != nil {
		t.Fatal(err)
	}
}

func TestPromptCredentialsIfMissingAlreadySet(t *testing.T) {
	cfg := &Config{
		ClientID:     "already_set_id",
		ClientSecret: "already_set_secret",
	}

	err := PromptCredentialsIfMissing(cfg)
	if err != nil {
		t.Fatalf("expected nil error when credentials already set, got: %v", err)
	}
}

func TestSpotifyScopesLeastPrivilege(t *testing.T) {
	for _, sc := range SpotifyScopes {
		if sc == "user-library-read" {
			t.Errorf("found unused scope 'user-library-read'")
		}
	}
}

func TestEnsureTokenValid(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	validToken := &oauth2.Token{
		AccessToken:  "valid-access-token",
		RefreshToken: "refresh-token",
		Expiry:       time.Now().Add(1 * time.Hour),
	}
	cfg := &Config{
		ClientID: "test-client",
		Token:    validToken,
	}
	flow := NewOAuthFlow(cfg)
	loginCalled := false
	flow.loginFn = func(ctx context.Context) (*oauth2.Token, error) {
		loginCalled = true
		return nil, errors.New("login should not be called")
	}

	tok, err := flow.EnsureToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok.AccessToken != validToken.AccessToken {
		t.Fatalf("expected token %s, got %s", validToken.AccessToken, tok.AccessToken)
	}
	if loginCalled {
		t.Fatal("interactive login was unexpectedly called for valid token")
	}
}

func TestEnsureTokenMissingOrNoRefreshToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	tests := []struct {
		name  string
		token *oauth2.Token
	}{
		{"nil token", nil},
		{"empty refresh token", &oauth2.Token{AccessToken: "expired", RefreshToken: "", Expiry: time.Now().Add(-time.Hour)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				ClientID: "test-client",
				Token:    tt.token,
			}
			flow := NewOAuthFlow(cfg)
			expectedTok := &oauth2.Token{AccessToken: "interactive-token", RefreshToken: "new-refresh"}
			loginCalled := false
			flow.loginFn = func(ctx context.Context) (*oauth2.Token, error) {
				loginCalled = true
				return expectedTok, nil
			}

			tok, err := flow.EnsureToken(context.Background())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !loginCalled {
				t.Fatal("expected interactive login to be called")
			}
			if tok.AccessToken != expectedTok.AccessToken {
				t.Fatalf("expected token %s, got %s", expectedTok.AccessToken, tok.AccessToken)
			}
		})
	}
}

func TestEnsureTokenExpiredSuccess(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("grant_type") != "refresh_token" {
			t.Errorf("unexpected grant_type: %s", r.Form.Get("grant_type"))
		}
		fmt.Fprint(w, `{"access_token":"new-access-token","refresh_token":"new-refresh-token","token_type":"Bearer","expires_in":3600}`)
	}))
	defer server.Close()

	cfg := &Config{
		ClientID: "test-client",
		Token: &oauth2.Token{
			AccessToken:  "old-expired-token",
			RefreshToken: "old-refresh-token",
			Expiry:       time.Now().Add(-1 * time.Hour),
		},
	}
	flow := NewOAuthFlow(cfg)
	flow.config.Endpoint.TokenURL = server.URL

	loginCalled := false
	flow.loginFn = func(ctx context.Context) (*oauth2.Token, error) {
		loginCalled = true
		return nil, errors.New("login should not be called")
	}

	tok, err := flow.EnsureToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loginCalled {
		t.Fatal("interactive login was unexpectedly called when refresh succeeded")
	}
	if tok.AccessToken != "new-access-token" {
		t.Fatalf("expected new-access-token, got %s", tok.AccessToken)
	}
	if cfg.CurrentToken().AccessToken != "new-access-token" {
		t.Fatalf("expected config to be updated with new token, got %s", cfg.CurrentToken().AccessToken)
	}
}

func TestEnsureTokenExpiredRevoked(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid_grant","error_description":"Refresh token revoked"}`)
	}))
	defer server.Close()

	cfg := &Config{
		ClientID: "test-client",
		Token: &oauth2.Token{
			AccessToken:  "expired-token",
			RefreshToken: "revoked-refresh-token",
			Expiry:       time.Now().Add(-1 * time.Hour),
		},
	}
	flow := NewOAuthFlow(cfg)
	flow.config.Endpoint.TokenURL = server.URL

	loginCalled := false
	interactiveTok := &oauth2.Token{AccessToken: "re-authenticated-token"}
	flow.loginFn = func(ctx context.Context) (*oauth2.Token, error) {
		loginCalled = true
		return interactiveTok, nil
	}

	tok, err := flow.EnsureToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !loginCalled {
		t.Fatal("expected interactive login fallback when token is revoked")
	}
	if tok.AccessToken != interactiveTok.AccessToken {
		t.Fatalf("expected token %s, got %s", interactiveTok.AccessToken, tok.AccessToken)
	}
}

func TestEnsureTokenExpiredNetworkError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// Start and immediately close a server so connections fail
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	cfg := &Config{
		ClientID: "test-client",
		Token: &oauth2.Token{
			AccessToken:  "expired-token",
			RefreshToken: "any-refresh-token",
			Expiry:       time.Now().Add(-1 * time.Hour),
		},
	}
	flow := NewOAuthFlow(cfg)
	flow.config.Endpoint.TokenURL = server.URL

	loginCalled := false
	flow.loginFn = func(ctx context.Context) (*oauth2.Token, error) {
		loginCalled = true
		return nil, errors.New("login should not be called")
	}

	tok, err := flow.EnsureToken(context.Background())
	if err == nil {
		t.Fatal("expected error due to network failure, got nil")
	}
	if tok != nil {
		t.Fatalf("expected nil token on network failure, got %v", tok)
	}
	if loginCalled {
		t.Fatal("interactive login should NOT be called on network failure")
	}
	if !strings.Contains(err.Error(), "failed to refresh Spotify session") {
		t.Fatalf("expected error message to contain 'failed to refresh Spotify session', got: %v", err)
	}
}

type staticTokenSource struct {
	tok *oauth2.Token
}

func (s *staticTokenSource) Token() (*oauth2.Token, error) {
	return s.tok, nil
}

func TestPersistingTokenSourceErrorLogger(t *testing.T) {
	// Setup a config pointing to an invalid path so SetToken fails
	t.Setenv("XDG_CONFIG_HOME", "/dev/null/cannot_exist")
	cfg := DefaultConfig()

	loggedWarnings := make(chan string, 1)
	flow := NewOAuthFlow(cfg)
	flow.SetErrorLogger(func(format string, args ...any) {
		loggedWarnings <- fmt.Sprintf(format, args...)
	})

	initialTok := &oauth2.Token{AccessToken: "token-1", Expiry: time.Now().Add(-time.Hour)}
	newTok := &oauth2.Token{AccessToken: "token-2", Expiry: time.Now().Add(time.Hour)}

	pts := &persistingTokenSource{
		flow:      flow,
		parent:    context.Background(),
		source:    func(context.Context, *oauth2.Token) oauth2.TokenSource { return &staticTokenSource{tok: newTok} },
		saveToken: cfg.SetToken,
		lastTok:   initialTok,
		warn:      flow.warnings.publish,
	}

	tok, err := pts.Token()
	if err != nil {
		t.Fatalf("Token() should return token even if persist fails: %v", err)
	}
	if tok.AccessToken != "token-2" {
		t.Fatalf("expected token-2, got %s", tok.AccessToken)
	}
	select {
	case warning := <-loggedWarnings:
		if !strings.Contains(warning, "failed to save refreshed Spotify token") {
			t.Fatalf("expected persistence warning, got %q", warning)
		}
	case <-time.After(time.Second):
		t.Fatal("logger did not receive persistence warning")
	}

	// An absent observer must not interfere with token refresh.
	flow.SetErrorLogger(nil)
	pts.lastTok = initialTok
	if _, err := pts.Token(); err != nil {
		t.Fatalf("unexpected error with nil logger: %v", err)
	}
	pts.warn = nil
	pts.lastTok = initialTok
	if _, err := pts.Token(); err != nil {
		t.Fatalf("unexpected error with nil warning publisher: %v", err)
	}
}
