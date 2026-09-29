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
	u, err := url.Parse(redirectURLStr)
	if err != nil {
		return nil, fmt.Errorf("invalid redirect URI: %w", err)
	}

	hostPort := u.Host
	if !hasPort(hostPort) {
		if u.Scheme == "https" {
			hostPort = net.JoinHostPort(hostPort, "443")
		} else {
			hostPort = net.JoinHostPort(hostPort, "80")
		}
	}

	cert, err := GenerateSelfSignedCert()
	if err != nil {
		return nil, fmt.Errorf("failed to generate TLS cert: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	listener, err := tls.Listen("tcp", hostPort, tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", hostPort, err)
	}

	resultCh := make(chan CallbackResult, 1)

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
			fmt.Fprintf(w, `<!DOCTYPE html><html><body style="font-family: sans-serif; background: #0f141c; color: #ff6b6b; padding: 40px; text-align: center;"><h2>Authentication Failed</h2><p>%s</p><p>You may close this tab.</p></body></html>`, authError)
			resultCh <- CallbackResult{
				Error: fmt.Errorf("spotify auth error: %s", authError),
				State: state,
			}
			return
		}

		if code == "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `<!DOCTYPE html><html><body style="font-family: sans-serif; background: #0f141c; color: #ff6b6b; padding: 40px; text-align: center;"><h2>Invalid Request</h2><p>Missing authorization code.</p></body></html>`)
			resultCh <- CallbackResult{
				Error: errors.New("missing authorization code in callback"),
				State: state,
			}
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `<!DOCTYPE html><html><body style="font-family: sans-serif; background: #0f141c; color: #00e5ff; padding: 40px; text-align: center;"><h2>Authentication Successful!</h2><p>You can close this tab and return to the terminal.</p></body></html>`)

		resultCh <- CallbackResult{
			Code:  code,
			State: state,
		}
	})

	go func() {
		_ = server.Serve(listener)
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		_ = listener.Close()
	}()

	return resultCh, nil
}

func hasPort(host string) bool {
	_, _, err := net.SplitHostPort(host)
	return err == nil
}
