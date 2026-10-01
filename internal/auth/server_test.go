package auth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
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
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"127.0.0.1", "::1"} {
		if err := leaf.VerifyHostname(host); err != nil {
			t.Fatalf("missing SAN for %s: %v", host, err)
		}
	}
	if err := leaf.VerifyHostname("192.0.2.1"); err == nil {
		t.Fatal("certificate trusted unsupported external IP")
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

func TestHTTPSCallbackServerIPv6VerifiesCertificate(t *testing.T) {
	probe, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	server, err := StartCallbackServer(ctx, "https://[::1]:0/callback")
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); <-server.Done })
	// Retrieve the server's generated self-signed certificate to trust it, then
	// use ordinary TLS verification for the actual IPv6 callback request.
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", server.Addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	cert := conn.ConnectionState().PeerCertificates[0]
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	response, err := client.Get("https://" + server.Addr + "/callback?code=ipv6-code&state=ipv6-state")
	if err != nil {
		t.Fatalf("IPv6 callback failed certificate verification: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", response.StatusCode)
	}
	select {
	case result := <-server.Results:
		if result.Error != nil || result.Code != "ipv6-code" || result.State != "ipv6-state" {
			t.Fatalf("callback=%+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("IPv6 callback did not publish result")
	}
}

func TestCallbackServerFullResultChannelDoesNotBlockRequestsOrShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server, err := StartCallbackServer(ctx, "http://127.0.0.1:0/callback")
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); <-server.Done })
	transport := &http.Transport{DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	for _, query := range []string{"code=first&state=state", "code=second", "error=access_denied", "state=missing-code"} {
		response, err := client.Get("http://" + server.Addr + "/callback?" + query)
		if err != nil {
			t.Fatalf("callback blocked with full result channel: %v", err)
		}
		// Consume the complete response instead of closing unread bodies and
		// allowing the transport to open speculative replacement connections.
		if _, err := io.Copy(io.Discard, response.Body); err != nil {
			t.Fatal(err)
		}
		if err := response.Body.Close(); err != nil {
			t.Fatal(err)
		}
	}
	cancel()
	select {
	case <-server.Done:
	case <-time.After(3 * time.Second):
		t.Fatal("callback shutdown blocked on result publication")
	}
	if result := <-server.Results; result.Code != "first" {
		t.Fatalf("original callback result was overwritten: %+v", result)
	}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		t.Fatalf("Done closed before callback listener released: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCallbackServerCancellationClosesIncompleteRequestBeforeDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server, err := StartCallbackServer(ctx, "http://127.0.0.1:0/callback")
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); <-server.Done })
	conn, err := net.DialTimeout("tcp", server.Addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	// The handler publishes the callback before net/http drains this deliberately
	// incomplete request body. The result is a barrier proving an active request.
	if _, err := fmt.Fprint(conn, "GET /callback?code=incomplete-body&state=state HTTP/1.1\r\nHost: 127.0.0.1\r\nContent-Length: 128\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-server.Results:
	case <-time.After(time.Second):
		t.Fatal("incomplete request did not reach callback handler")
	}
	cancel()
	select {
	case <-server.Done:
	case <-time.After(3 * time.Second):
		t.Fatal("callback shutdown escaped its graceful deadline")
	}
	if _, err := io.ReadAll(conn); err != nil {
		t.Fatalf("active callback connection remained open after Done: %v", err)
	}
}
