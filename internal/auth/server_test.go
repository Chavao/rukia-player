package auth

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
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

func TestHTTPSCallbackServerEphemeralPort(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	redirectURI := "https://127.0.0.1:0/callback"
	resCh, addr, err := StartHTTPSCallbackServerWithAddr(ctx, redirectURI)
	if err != nil {
		t.Fatalf("failed to start HTTPS server: %v", err)
	}

	// Make an HTTPS request ignoring self-signed cert
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr, Timeout: 3 * time.Second}

	resp, err := client.Get(fmt.Sprintf("https://%s/callback?code=test_code_123&state=xyz", addr))
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

	callback, err := StartCallbackServer(ctx1, "https://127.0.0.1:0/callback")
	if err != nil {
		t.Fatalf("failed to start first HTTPS server: %v", err)
	}

	cancel1()
	select {
	case <-callback.Done:
	case <-time.After(3 * time.Second):
		t.Fatal("callback server did not terminate after cancellation")
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	// Starting server on same allocated port should succeed now that port is released
	_, err = StartHTTPSCallbackServer(ctx2, fmt.Sprintf("https://%s/callback", callback.Addr))
	if err != nil {
		t.Fatalf("failed to start second HTTPS server on same port after cancel: %v", err)
	}
}

func TestStartHTTPSCallbackServerSecurity(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. External host should be rejected
	_, err := StartHTTPSCallbackServer(ctx, "https://evil.com:8443/callback")
	if err == nil || !strings.Contains(err.Error(), "insecure redirect URI host") {
		t.Errorf("expected insecure host error, got %v", err)
	}

	// 1b. localhost should be rejected (Spotify requires loopback IP literal)
	_, err = StartHTTPSCallbackServer(ctx, "http://localhost:8443/callback")
	if err == nil || !strings.Contains(err.Error(), "insecure redirect URI host") {
		t.Errorf("expected localhost to be rejected as insecure, got %v", err)
	}

	// 2. Unsupported scheme should be rejected
	_, err = StartHTTPSCallbackServer(ctx, "ftp://127.0.0.1:8443/callback")
	if err == nil || !strings.Contains(err.Error(), "unsupported redirect URI scheme") {
		t.Errorf("expected unsupported scheme error, got %v", err)
	}

	// 3. HTML escaping test
	resCh, addr, err := StartHTTPSCallbackServerWithAddr(ctx, "https://127.0.0.1:0/callback")
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr, Timeout: 3 * time.Second}

	resp, err := client.Get(fmt.Sprintf("https://%s/callback?error=%%3Cscript%%3Ealert(1)%%3C/script%%3E", addr))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if strings.Contains(string(body), "<script>") {
		t.Errorf("body contained unescaped HTML: %s", string(body))
	}
	if !strings.Contains(string(body), "&lt;script&gt;") {
		t.Errorf("body missing escaped HTML: %s", string(body))
	}

	// Drain result channel
	<-resCh
}

func TestCallbackServerIPv6(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Ephemeral IPv6 port
	server, err := StartCallbackServer(ctx, "http://[::1]:0/callback")
	if err != nil {
		t.Skipf("IPv6 loopback not supported on this host: %v", err)
	}
	if strings.Contains(server.Addr, "[[::1]]") {
		t.Errorf("invalid double-bracketed IPv6 address: %s", server.Addr)
	}
}
