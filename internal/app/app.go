package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/Chavao/rukia-player/internal/auth"
	"github.com/Chavao/rukia-player/internal/player"
	"github.com/Chavao/rukia-player/internal/spotify"
	"github.com/Chavao/rukia-player/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

var Version = "dev"

// PrintUsage prints CLI usage documentation to w.
func PrintUsage(w io.Writer) {
	fmt.Fprintf(w, "rukia v%s - CLI Spotify Player\n\n", Version)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  rukia <playlist-id-or-url>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  rukia 6UUCMxk575eDTwSWa0qQhB")
	fmt.Fprintln(w, "  rukia '6UUCMxk575eDTwSWa0qQhB?si=fa799fec9a404660'")
	fmt.Fprintln(w, "  rukia https://open.spotify.com/playlist/6UUCMxk575eDTwSWa0qQhB")
	fmt.Fprintln(w, "  rukia spotify:playlist:6UUCMxk575eDTwSWa0qQhB")
}

// Run orchestrates configuration, authentication, player startup, and the TUI lifecycle.
func Run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("rukia", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var showVersion bool
	var showHelp bool
	fs.BoolVar(&showVersion, "v", false, "Show version")
	fs.BoolVar(&showVersion, "version", false, "Show version")
	fs.BoolVar(&showHelp, "h", false, "Show help")
	fs.BoolVar(&showHelp, "help", false, "Show help")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if showVersion {
		fmt.Printf("rukia v%s\n", Version)
		return nil
	}
	if showHelp {
		PrintUsage(os.Stdout)
		return nil
	}

	// 1. Load application config
	cfg, err := auth.LoadConfig()
	if err != nil {
		return fmt.Errorf("error loading configuration: %w", err)
	}

	// 2. Parse playlist argument
	var rawPlaylist string
	remainingArgs := fs.Args()
	if len(remainingArgs) > 0 {
		rawPlaylist = remainingArgs[0]
	} else if cfg.LastPlaylist != "" {
		rawPlaylist = cfg.LastPlaylist
		fmt.Printf("Resuming last played playlist: %s\n", rawPlaylist)
	} else {
		PrintUsage(os.Stdout)
		return nil
	}

	playlistID, err := spotify.ParsePlaylistID(rawPlaylist)
	if err != nil {
		return fmt.Errorf("invalid playlist: %w", err)
	}

	// 3. Prompt for credentials if first execution
	if err := auth.PromptCredentialsIfMissing(cfg); err != nil {
		return fmt.Errorf("credentials setup failed: %w", err)
	}

	// 4. Authenticate via OAuth 2.0 flow if no valid token
	oauthFlow := auth.NewOAuthFlow(cfg)

	if cfg.Token == nil || !cfg.Token.Valid() {
		if _, err := oauthFlow.RunInteractiveLogin(ctx); err != nil {
			return fmt.Errorf("spotify login failed: %w", err)
		}
	}

	// 5. Initialize authenticated Spotify client
	httpClient, _ := oauthFlow.Client(ctx, cfg.CurrentToken())
	spotifyClient := spotify.NewClient(httpClient)

	// Fetch current user
	user, err := spotifyClient.GetCurrentUser(ctx)
	if err != nil {
		var apiErr *spotify.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized {
			// If token was rejected with 401, re-authenticate once
			fmt.Printf("Session expired (401), re-authenticating: %v\n", err)
			if _, err := oauthFlow.RunInteractiveLogin(ctx); err != nil {
				return fmt.Errorf("authentication failed: %w", err)
			}
			httpClient, _ = oauthFlow.Client(ctx, cfg.CurrentToken())
			spotifyClient = spotify.NewClient(httpClient)
			user, err = spotifyClient.GetCurrentUser(ctx)
			if err != nil {
				return fmt.Errorf("failed to connect to Spotify: %w", err)
			}
		} else {
			return fmt.Errorf("failed to connect to Spotify: %w", err)
		}
	}

	// 6. Start PulseAudio audio sink
	playerEngine := player.NewEngine("rukia")
	if volume := cfg.CurrentVolume(); volume >= 0 {
		playerEngine.SetVolume(volume)
	}
	if err := playerEngine.Start(ctx, user.ID, cfg.CurrentToken().AccessToken); err != nil {
		fmt.Printf("Notice: PulseAudio audio sink initialization failed (%v)\n", err)
	}
	defer playerEngine.Close()

	// 7. Fetch playlist tracks
	fmt.Printf("Loading playlist %s...\n", playlistID)
	playlist, err := spotifyClient.GetPlaylist(ctx, playlistID)
	if err != nil {
		return fmt.Errorf("failed to load playlist: %w", err)
	}

	// Persist last played playlist
	_ = cfg.SetLastPlaylist(playlistID)

	// 8. Find target device (rukia or active device)
	deviceCtx, cancelDeviceDiscovery := context.WithTimeout(ctx, 5*time.Second)
	defer cancelDeviceDiscovery()
	targetDeviceID := discoverDevice(deviceCtx, spotifyClient, playerEngine.DeviceName(), 8, 500*time.Millisecond)

	// 9. Start initial playback
	if targetDeviceID != "" {
		_ = spotifyClient.TransferPlayback(ctx, targetDeviceID, true)
		time.Sleep(250 * time.Millisecond)
		_ = spotifyClient.PlayPlaylist(ctx, targetDeviceID, playlist.URI, 0)
	} else {
		// Attempt playback without explicit device
		_ = spotifyClient.PlayPlaylist(ctx, "", playlist.URI, 0)
	}

	// 10. Start Bubble Tea TUI
	model := ui.NewModel(spotifyClient, playerEngine, user, playlist, targetDeviceID, cfg)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	return nil
}

type deviceLister interface {
	GetDevices(context.Context) ([]spotify.Device, error)
}

func discoverDevice(ctx context.Context, client deviceLister, name string, maxAttempts int, interval time.Duration) string {
	var fallback string
	for attempt := 0; attempt < maxAttempts && ctx.Err() == nil; attempt++ {
		devices, err := client.GetDevices(ctx)
		if err == nil {
			for _, d := range devices {
				if d.Name == name {
					return d.ID
				}
			}
			// Remember an active device in case the local receiver never appears.
			if len(devices) > 0 {
				fallback = devices[0].ID
				for _, d := range devices {
					if d.IsActive {
						fallback = d.ID
						break
					}
				}
			}
		}
		if attempt == maxAttempts-1 {
			break
		}
		timer := time.NewTimer(interval)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return fallback
		}
	}
	return fallback
}
