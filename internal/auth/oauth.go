package auth

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/spotify"
)

var SpotifyScopes = []string{
	"user-read-playback-state",
	"user-modify-playback-state",
	"user-read-currently-playing",
	"playlist-read-private",
	"playlist-read-collaborative",
	"streaming",
}

// ErrReauthenticationRequired means a stored refresh token cannot be used again.
var ErrReauthenticationRequired = errors.New("Spotify reauthentication required")

const silentRefreshTimeout = 8 * time.Second

// OAuthFlow handles Spotify OAuth 2.0 interactions.
type OAuthFlow struct {
	config         *oauth2.Config
	appCfg         *Config
	loginFn        func(context.Context) (*oauth2.Token, error)
	warnings       warningQueue
	saveToken      func(*oauth2.Token) error
	refreshTimeout time.Duration
}

// SetErrorLogger configures an error sink for background token operations.
func (o *OAuthFlow) SetErrorLogger(logger func(format string, args ...any)) {
	if logger == nil {
		o.SetWarningSink(nil)
		return
	}
	o.SetWarningSink(func(err error) { logger("warning: %v\n", err) })
}

// NewOAuthFlow creates an initialized OAuthFlow from the application Config.
func NewOAuthFlow(cfg *Config) *OAuthFlow {
	oauthConfig := &oauth2.Config{
		ClientID:    cfg.ClientID,
		RedirectURL: cfg.RedirectURI,
		Scopes:      SpotifyScopes,
		Endpoint:    spotify.Endpoint,
	}
	oauthConfig.Endpoint.AuthStyle = oauth2.AuthStyleInParams

	flow := &OAuthFlow{
		config:         oauthConfig,
		appCfg:         cfg,
		saveToken:      cfg.SetToken,
		refreshTimeout: silentRefreshTimeout,
	}
	flow.loginFn = flow.RunInteractiveLogin
	return flow
}

// GenerateRandomState creates a cryptographically secure hex state parameter.
func GenerateRandomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random state: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// GetAuthURL generates the Spotify authorization URL with state and a PKCE challenge.
func (o *OAuthFlow) GetAuthURL(state, verifier string) string {
	return o.config.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.S256ChallengeOption(verifier))
}

// Exchange swaps the authorization code for an OAuth2 token (access + refresh tokens).
func (o *OAuthFlow) Exchange(ctx context.Context, code, verifier string) (*oauth2.Token, error) {
	token, err := o.config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("failed to exchange token: %w", err)
	}
	return token, nil
}

// TokenSource returns a refreshing TokenSource backed by the OAuth config.
func (o *OAuthFlow) TokenSource(ctx context.Context, token *oauth2.Token) oauth2.TokenSource {
	if o.appCfg.AuthFlow != "pkce" && o.appCfg.ClientSecret != "" {
		legacy := *o.config
		legacy.ClientSecret = o.appCfg.ClientSecret
		legacy.Endpoint.AuthStyle = oauth2.AuthStyleInHeader
		return legacy.TokenSource(ctx, token)
	}
	return o.config.TokenSource(ctx, token)
}

// Client returns an authenticated HTTP client that automatically refreshes tokens and saves them.
// Runtime refreshes are derived from each outgoing request context, so a 2-3 second
// Spotify operation cannot be extended by the broader refresh timeout.
func (o *OAuthFlow) Client(ctx context.Context, token *oauth2.Token) (*http.Client, oauth2.TokenSource) {
	baseClient := oauth2.NewClient(ctx, nil)
	tokenClient := *baseClient
	persistingTS := &persistingTokenSource{
		flow:            o,
		parent:          ctx,
		tokenHTTPClient: &tokenClient,
		saveToken:       o.saveToken,
		lastTok:         token,
		warn:            o.warnings.publish,
	}

	baseTransport := baseClient.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	apiClient := *baseClient
	apiClient.Transport = &requestBoundOAuthTransport{
		base:   baseTransport,
		source: persistingTS,
	}
	return &apiClient, persistingTS
}

type requestBoundOAuthTransport struct {
	base   http.RoundTripper
	source *persistingTokenSource
}

func (t *requestBoundOAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tok, err := t.source.TokenContext(req.Context())
	if err != nil {
		return nil, err
	}
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	tok.SetAuthHeader(clone)
	return t.base.RoundTrip(clone)
}

type persistingTokenSource struct {
	mu              sync.Mutex
	gateOnce        sync.Once
	refreshGate     chan struct{}
	flow            *OAuthFlow
	parent          context.Context
	tokenHTTPClient *http.Client
	source          func(context.Context, *oauth2.Token) oauth2.TokenSource
	saveToken       func(*oauth2.Token) error
	lastTok         *oauth2.Token
	warn            func(error)
}

func (p *persistingTokenSource) Token() (*oauth2.Token, error) {
	return p.TokenContext(p.parent)
}

func (p *persistingTokenSource) TokenContext(ctx context.Context) (*oauth2.Token, error) {
	if tok := p.validCachedToken(); tok != nil {
		return tok, nil
	}

	p.gateOnce.Do(func() { p.refreshGate = make(chan struct{}, 1) })
	select {
	case p.refreshGate <- struct{}{}:
		defer func() { <-p.refreshGate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Another request may have completed the refresh while this request waited.
	if tok := p.validCachedToken(); tok != nil {
		return tok, nil
	}

	p.mu.Lock()
	var previous *oauth2.Token
	if p.lastTok != nil {
		copy := *p.lastTok
		previous = &copy
	}
	p.mu.Unlock()

	refreshCtx, cancel := context.WithTimeout(ctx, p.flow.refreshTimeout)
	defer cancel()
	if p.tokenHTTPClient != nil {
		refreshCtx = context.WithValue(refreshCtx, oauth2.HTTPClient, p.tokenHTTPClient)
	}

	source := p.source
	if source == nil {
		source = p.flow.TokenSource
	}
	tok, err := source(refreshCtx, previous).Token()
	if err != nil {
		if ctxErr := refreshCtx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}
	if tok.RefreshToken == "" && previous != nil && previous.RefreshToken != "" {
		tok.RefreshToken = previous.RefreshToken
	}

	changed := previous == nil || tok.AccessToken != previous.AccessToken || tok.RefreshToken != previous.RefreshToken || tok.TokenType != previous.TokenType || !tok.Expiry.Equal(previous.Expiry)
	last := *tok
	p.mu.Lock()
	p.lastTok = &last
	p.mu.Unlock()

	if changed && p.saveToken != nil {
		// Each refresh gets one persistence attempt. A filesystem failure is a
		// warning; it must not turn every authenticated request into another save.
		if err := p.saveToken(tok); err != nil {
			if p.warn != nil {
				p.warn(fmt.Errorf("failed to save refreshed Spotify token: %w", err))
			}
		} else if p.flow != nil {
			p.flow.warnings.resolve()
		}
	}

	return tok, nil
}

func (p *persistingTokenSource) validCachedToken() *oauth2.Token {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lastTok == nil || !p.lastTok.Valid() {
		return nil
	}
	copy := *p.lastTok
	return &copy
}

// RunInteractiveLogin runs the full authorization code flow:
// 1. Starts the HTTPS callback server
// 2. Launches the user's browser (or prints the URL)
// 3. Waits for the callback
// 4. Exchanges the code for tokens and saves to config
func (o *OAuthFlow) RunInteractiveLogin(ctx context.Context) (*oauth2.Token, error) {
	state, err := GenerateRandomState()
	if err != nil {
		return nil, err
	}
	verifier := oauth2.GenerateVerifier()

	loginCtx, cancel := context.WithCancel(ctx)
	callback, err := StartCallbackServer(loginCtx, o.appCfg.RedirectURI)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start HTTPS callback server: %w", err)
	}
	defer func() {
		cancel()
		<-callback.Done
	}()

	authURL := o.GetAuthURL(state, verifier)

	fmt.Println("Launching browser to authenticate with Spotify...")
	fmt.Printf("If your browser does not open automatically, visit this URL:\n\n%s\n\n", authURL)
	if err := OpenBrowser(authURL); err != nil {
		fmt.Println("Could not open the browser automatically; use the URL above.")
	}

	select {
	case res := <-callback.Results:
		if res.Error != nil {
			return nil, fmt.Errorf("login failed: %w", res.Error)
		}
		if res.State != state {
			return nil, errors.New("state mismatch in OAuth callback; possible CSRF")
		}

		token, err := o.Exchange(ctx, res.Code, verifier)
		if err != nil {
			return nil, err
		}

		if err := o.appCfg.SetPKCEToken(token); err != nil {
			return nil, fmt.Errorf("failed to save token to config: %w", err)
		}
		o.warnings.resolve()

		fmt.Println("Spotify authentication successful!")
		return token, nil

	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Minute):
		return nil, errors.New("authentication timed out after 5 minutes")
	}
}

// EnsureToken returns a valid OAuth token, refreshing it silently if expired,
// or initiating an interactive browser login if no token exists or if refresh is rejected.
func (o *OAuthFlow) EnsureToken(ctx context.Context) (*oauth2.Token, error) {
	tok, err := o.SilentRefresh(ctx)
	if errors.Is(err, ErrReauthenticationRequired) {
		return o.interactiveLogin(ctx)
	}
	return tok, err
}

// SilentRefresh returns a usable token without prompting for human interaction.
// Token endpoint failures only require login when OAuth reports invalid_grant.
func (o *OAuthFlow) SilentRefresh(ctx context.Context) (*oauth2.Token, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tok := o.appCfg.CurrentToken()
	if tok != nil && tok.Valid() {
		return tok, nil
	}
	if tok == nil || tok.RefreshToken == "" {
		return nil, ErrReauthenticationRequired
	}

	refreshCtx, cancel := context.WithTimeout(ctx, o.refreshTimeout)
	defer cancel()
	refreshed, err := o.TokenSource(refreshCtx, tok).Token()
	if err != nil {
		if err := refreshCtx.Err(); err != nil {
			return nil, fmt.Errorf("failed to refresh Spotify session: %w", err)
		}
		var retrieveErr *oauth2.RetrieveError
		if errors.As(err, &retrieveErr) && retrieveErr.ErrorCode == "invalid_grant" &&
			(retrieveErr.Response == nil || retrieveErr.Response.StatusCode != http.StatusTooManyRequests && retrieveErr.Response.StatusCode < http.StatusInternalServerError) {
			return nil, ErrReauthenticationRequired
		}
		return nil, fmt.Errorf("failed to refresh Spotify session: %w", err)
	}

	if refreshed.RefreshToken == "" && tok.RefreshToken != "" {
		refreshed.RefreshToken = tok.RefreshToken
	}

	if err := o.saveToken(refreshed); err != nil {
		o.warnings.publish(fmt.Errorf("failed to save refreshed Spotify token: %w", err))
	} else {
		o.warnings.resolve()
	}

	return refreshed, nil
}

func (o *OAuthFlow) interactiveLogin(ctx context.Context) (*oauth2.Token, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if o.loginFn != nil {
		return o.loginFn(ctx)
	}
	return o.RunInteractiveLogin(ctx)
}

// OpenBrowser attempts to open a URL in the system's default browser.
func OpenBrowser(targetURL string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", targetURL)
	case "darwin":
		cmd = exec.Command("open", targetURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", targetURL)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return cmd.Start()
}

// PromptCredentialsIfMissing prompts the user for the Spotify Client ID if it is missing.
func PromptCredentialsIfMissing(cfg *Config) error {
	if cfg.HasCredentials() {
		return nil
	}

	reader := bufio.NewReader(os.Stdin)

	fmt.Println("=================================================================")
	fmt.Println("rukia - Spotify CLI Player Setup")
	fmt.Println("Spotify Client ID not found.")
	fmt.Println("Please register an application at https://developer.spotify.com/dashboard")
	fmt.Printf("Ensure Redirect URI is set to: %s\n", cfg.RedirectURI)
	fmt.Println("=================================================================")

	fmt.Print("Enter Spotify Client ID: ")
	clientID, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read client ID: %w", err)
	}
	cfg.ClientID = strings.TrimSpace(clientID)

	if !cfg.HasCredentials() {
		return errors.New("client ID cannot be empty")
	}

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	fmt.Println("Credentials saved successfully to", cfgDirDisplay())
	return nil
}

func cfgDirDisplay() string {
	p, err := GetConfigPath()
	if err != nil {
		return "~/.config/rukia/config.json"
	}
	return p
}
