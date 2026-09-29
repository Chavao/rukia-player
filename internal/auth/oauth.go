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

	"github.com/charmbracelet/x/term"
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

// OAuthFlow handles Spotify OAuth 2.0 interactions.
type OAuthFlow struct {
	config *oauth2.Config
	appCfg *Config
}

// NewOAuthFlow creates an initialized OAuthFlow from the application Config.
func NewOAuthFlow(cfg *Config) *OAuthFlow {
	oauthConfig := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURI,
		Scopes:       SpotifyScopes,
		Endpoint:     spotify.Endpoint,
	}

	return &OAuthFlow{
		config: oauthConfig,
		appCfg: cfg,
	}
}

// GenerateRandomState creates a cryptographically secure hex state parameter.
func GenerateRandomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random state: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// GetAuthURL generates the Spotify authorization URL with state.
func (o *OAuthFlow) GetAuthURL(state string) string {
	return o.config.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

// Exchange swaps the authorization code for an OAuth2 token (access + refresh tokens).
func (o *OAuthFlow) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	token, err := o.config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange token: %w", err)
	}
	return token, nil
}

// TokenSource returns a refreshing TokenSource backed by the OAuth config.
func (o *OAuthFlow) TokenSource(ctx context.Context, token *oauth2.Token) oauth2.TokenSource {
	return o.config.TokenSource(ctx, token)
}

// Client returns an authenticated HTTP client that automatically refreshes tokens and saves them.
func (o *OAuthFlow) Client(ctx context.Context, token *oauth2.Token) (*http.Client, oauth2.TokenSource) {
	ts := o.TokenSource(ctx, token)
	// Wrap token source to persist refreshed token if it changes
	persistingTS := &persistingTokenSource{
		src:     ts,
		cfg:     o.appCfg,
		lastTok: token,
	}
	return oauth2.NewClient(ctx, persistingTS), persistingTS
}

type persistingTokenSource struct {
	mu      sync.Mutex
	src     oauth2.TokenSource
	cfg     *Config
	lastTok *oauth2.Token
}

func (p *persistingTokenSource) Token() (*oauth2.Token, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	tok, err := p.src.Token()
	if err != nil {
		return nil, err
	}

	if p.lastTok == nil || tok.AccessToken != p.lastTok.AccessToken {
		p.lastTok = tok
		_ = p.cfg.SetToken(tok)
	}

	return tok, nil
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

	loginCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	callbackCh, err := StartHTTPSCallbackServer(loginCtx, o.appCfg.RedirectURI)
	if err != nil {
		return nil, fmt.Errorf("failed to start HTTPS callback server: %w", err)
	}

	authURL := o.GetAuthURL(state)

	fmt.Println("Launching browser to authenticate with Spotify...")
	fmt.Printf("If your browser does not open automatically, visit this URL:\n\n%s\n\n", authURL)
	_ = OpenBrowser(authURL)

	select {
	case res := <-callbackCh:
		if res.Error != nil {
			return nil, fmt.Errorf("login failed: %w", res.Error)
		}
		if res.State != state {
			return nil, errors.New("state mismatch in OAuth callback; possible CSRF")
		}

		token, err := o.Exchange(ctx, res.Code)
		if err != nil {
			return nil, err
		}

		if err := o.appCfg.SetToken(token); err != nil {
			return nil, fmt.Errorf("failed to save token to config: %w", err)
		}

		fmt.Println("Spotify authentication successful!")
		return token, nil

	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Minute):
		return nil, errors.New("authentication timed out after 5 minutes")
	}
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

// PromptCredentialsIfMissing prompts the user via stdin for Spotify Client ID and Client Secret if not already set.
func PromptCredentialsIfMissing(cfg *Config) error {
	if cfg.HasCredentials() {
		return nil
	}

	reader := bufio.NewReader(os.Stdin)

	fmt.Println("=================================================================")
	fmt.Println("rukia - Spotify CLI Player Setup")
	fmt.Println("Spotify Client credentials not found.")
	fmt.Println("Please register an application at https://developer.spotify.com/dashboard")
	fmt.Printf("Ensure Redirect URI is set to: %s\n", cfg.RedirectURI)
	fmt.Println("=================================================================")

	fmt.Print("Enter Spotify Client ID: ")
	clientID, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read client ID: %w", err)
	}
	cfg.ClientID = strings.TrimSpace(clientID)

	fmt.Print("Enter Spotify Client Secret: ")
	var clientSecret string
	if term.IsTerminal(os.Stdin.Fd()) {
		byteSecret, err := term.ReadPassword(os.Stdin.Fd())
		fmt.Println()
		if err != nil {
			return fmt.Errorf("failed to read client secret: %w", err)
		}
		clientSecret = string(byteSecret)
	} else {
		line, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read client secret: %w", err)
		}
		clientSecret = line
	}
	cfg.ClientSecret = strings.TrimSpace(clientSecret)

	if !cfg.HasCredentials() {
		return errors.New("client ID and client secret cannot be empty")
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
