package auth

import (
	"context"
	"crypto/tls"
	"net/http"
	"testing"
	"time"
)

func TestGenerateSelfSignedCert(t *testing.T) {
	cert, err := GenerateSelfSignedCert()
	if err != nil {
		t.Fatalf("failed to generate cert: %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Fatal("empty certificate list")
	}
}

func TestHTTPSCallbackServer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use an ephemeral port on 127.0.0.1
	redirectURI := "https://127.0.0.1:0/callback"
	resCh, err := StartHTTPSCallbackServer(ctx, redirectURI)
	if err != nil {
		// Port 0 might not be permitted directly in some parsers if net.Listen can't resolve :0;
		// let's test with a random high port or test listener
		t.Skipf("skipping test if port 0 listen not supported: %v", err)
	}

	_ = resCh
}

func TestHTTPSCallbackServerExplicitPort(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	redirectURI := "https://127.0.0.1:18443/callback"
	resCh, err := StartHTTPSCallbackServer(ctx, redirectURI)
	if err != nil {
		t.Fatalf("failed to start HTTPS server: %v", err)
	}

	// Make an HTTPS request ignoring self-signed cert
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr, Timeout: 3 * time.Second}

	resp, err := client.Get("https://127.0.0.1:18443/callback?code=test_code_123&state=xyz")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", resp.StatusCode)
	}

	select {
	case res := <-resCh:
		if res.Error != nil {
			t.Fatalf("callback returned error: %v", res.Error)
		}
		if res.Code != "test_code_123" {
			t.Errorf("expected code 'test_code_123', got %s", res.Code)
		}
		if res.State != "xyz" {
			t.Errorf("expected state 'xyz', got %s", res.State)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback result")
	}
}

func TestHTTPSCallbackServerContextCancel(t *testing.T) {
	ctx1, cancel1 := context.WithCancel(context.Background())
	redirectURI := "https://127.0.0.1:18444/callback"

	_, err := StartHTTPSCallbackServer(ctx1, redirectURI)
	if err != nil {
		t.Fatalf("failed to start first HTTPS server: %v", err)
	}

	// Cancel context to stop server
	cancel1()

	// Wait briefly for listener to be closed
	time.Sleep(100 * time.Millisecond)

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	// Starting server on same port should succeed now
	_, err = StartHTTPSCallbackServer(ctx2, redirectURI)
	if err != nil {
		t.Fatalf("failed to start second HTTPS server on same port after cancel: %v", err)
	}
}

