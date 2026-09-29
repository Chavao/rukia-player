package auth

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
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
	if err != nil || refreshed.AccessToken != "refreshed" || calls != 2 {
		t.Fatalf("refresh=%v err=%v calls=%d", refreshed, err, calls)
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
