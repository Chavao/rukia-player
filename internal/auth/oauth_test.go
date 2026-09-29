package auth

import (
	"net/url"
	"testing"
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
		ClientID:     "my_client_id",
		ClientSecret: "my_client_secret",
		RedirectURI:  "https://127.0.0.1:8443/callback",
	}

	flow := NewOAuthFlow(cfg)
	authURL := flow.GetAuthURL("sample_state")

	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("failed to parse auth URL: %v", err)
	}

	q := parsed.Query()
	if q.Get("client_id") != "my_client_id" {
		t.Errorf("expected client_id 'my_client_id', got %s", q.Get("client_id"))
	}
	if q.Get("redirect_uri") != "https://127.0.0.1:8443/callback" {
		t.Errorf("expected redirect_uri 'https://127.0.0.1:8443/callback', got %s", q.Get("redirect_uri"))
	}
	if q.Get("state") != "sample_state" {
		t.Errorf("expected state 'sample_state', got %s", q.Get("state"))
	}
	if q.Get("response_type") != "code" {
		t.Errorf("expected response_type 'code', got %s", q.Get("response_type"))
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

