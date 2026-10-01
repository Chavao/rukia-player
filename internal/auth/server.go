package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"html"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"time"
)

// CallbackResult holds the outcome of the OAuth redirect callback.
type CallbackResult struct {
	Code  string
	State string
	Error error
}

// CallbackServer exposes the callback result and listener lifecycle.
type CallbackServer struct {
	Results <-chan CallbackResult
	Addr    string
	Done    <-chan struct{}
}

// GenerateSelfSignedCert generates an in-memory TLS certificate for 127.0.0.1 / localhost.
func GenerateSelfSignedCert() (tls.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to generate private key: %w", err)
	}

	notBefore := time.Now().Add(-1 * time.Hour)
	notAfter := notBefore.Add(24 * 365 * time.Hour)

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to generate serial number: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"rukia-player"},
			CommonName:   "127.0.0.1",
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to create certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to load x509 key pair: %w", err)
	}

	return cert, nil
}

// StartHTTPSCallbackServer starts a background HTTPS server to capture the Spotify OAuth callback.
func StartHTTPSCallbackServer(ctx context.Context, redirectURLStr string) (<-chan CallbackResult, error) {
	callback, err := StartCallbackServer(ctx, redirectURLStr)
	if err != nil {
		return nil, err
	}
	return callback.Results, nil
}

// StartHTTPSCallbackServerWithAddr starts a background HTTPS or HTTP server to capture the Spotify OAuth callback
// and returns the result channel and the bound listener address (useful for dynamic port allocation with :0).
func StartHTTPSCallbackServerWithAddr(ctx context.Context, redirectURLStr string) (<-chan CallbackResult, string, error) {
	callback, err := StartCallbackServer(ctx, redirectURLStr)
	if err != nil {
		return nil, "", err
	}
	return callback.Results, callback.Addr, nil
}

// StartCallbackServer starts the callback listener and exposes its termination.
func StartCallbackServer(ctx context.Context, redirectURLStr string) (*CallbackServer, error) {
	u, err := url.Parse(redirectURLStr)
	if err != nil {
		return nil, fmt.Errorf("invalid redirect URI: %w", err)
	}

	hostname := u.Hostname()
	if hostname != "127.0.0.1" && hostname != "::1" {
		return nil, fmt.Errorf("insecure redirect URI host: %s; Spotify requires loopback IP (127.0.0.1 or ::1)", hostname)
	}

	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, fmt.Errorf("unsupported redirect URI scheme: %s; must be http or https", u.Scheme)
	}

	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	hostPort := net.JoinHostPort(hostname, port)

	var listener net.Listener
	if u.Scheme == "https" {
		cert, err := GenerateSelfSignedCert()
		if err != nil {
			return nil, fmt.Errorf("failed to generate TLS cert: %w", err)
		}

		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
		}

		listener, err = tls.Listen("tcp", hostPort, tlsConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to listen on %s: %w", hostPort, err)
		}
	} else {
		listener, err = net.Listen("tcp", hostPort)
		if err != nil {
			return nil, fmt.Errorf("failed to listen on %s: %w", hostPort, err)
		}
	}

	resultCh := make(chan CallbackResult, 1)
	doneCh := make(chan struct{})
	serveDone := make(chan struct{})
	publish := func(result CallbackResult) {
		select {
		case resultCh <- result:
		default:
		}
	}

	mux := http.NewServeMux()
	server := &http.Server{
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	callbackPath := u.Path
	if callbackPath == "" {
		callbackPath = "/callback"
	}

	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		authError := query.Get("error")
		code := query.Get("code")
		state := query.Get("state")

		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		if authError != "" {
			w.WriteHeader(http.StatusBadRequest)
			escapedErr := html.EscapeString(authError)
			fmt.Fprintf(w, `<!DOCTYPE html><html><body style="font-family: sans-serif; background: #0f141c; color: #ff6b6b; padding: 40px; text-align: center;"><h2>Authentication Failed</h2><p>%s</p><p>You may close this tab.</p></body></html>`, escapedErr)
			publish(CallbackResult{
				Error: fmt.Errorf("spotify auth error: %s", authError),
				State: state,
			})
			return
		}

		if code == "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `<!DOCTYPE html><html><body style="font-family: sans-serif; background: #0f141c; color: #ff6b6b; padding: 40px; text-align: center;"><h2>Invalid Request</h2><p>Missing authorization code.</p></body></html>`)
			publish(CallbackResult{
				Error: errors.New("missing authorization code in callback"),
				State: state,
			})
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `<!DOCTYPE html><html><body style="font-family: sans-serif; background: #0f141c; color: #00e5ff; padding: 40px; text-align: center;"><h2>Authentication Successful!</h2><p>You can close this tab and return to the terminal.</p></body></html>`)

		publish(CallbackResult{
			Code:  code,
			State: state,
		})
	})

	go func() {
		_ = server.Serve(listener)
		close(serveDone)
	}()

	go func() {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = server.Shutdown(shutdownCtx)
			cancel()
			_ = listener.Close()
		case <-serveDone:
		}
		<-serveDone
		close(doneCh)
	}()

	return &CallbackServer{Results: resultCh, Addr: listener.Addr().String(), Done: doneCh}, nil
}
