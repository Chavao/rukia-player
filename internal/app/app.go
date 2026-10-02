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
func Run(ctx context.Context, args []string) (runErr error) {
	ctx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
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

	// 4. Authenticate via OAuth 2.0 flow
	oauthFlow := auth.NewOAuthFlow(cfg)
	defer func() {
		// Run has returned from Bubble Tea before this runs. Retain a final
		// persistence reminder even if a UI waiter received it just before quit.
		if err := printWarning(os.Stderr, oauthFlow.LatestWarning()); err != nil {
			runErr = errors.Join(runErr, err)
		}
	}()
	if _, err := oauthFlow.EnsureToken(ctx); err != nil {
		return fmt.Errorf("spotify login failed: %w", err)
	}

	startupCtx, cancelStartup := context.WithTimeout(ctx, 30*time.Second)
	defer cancelStartup()

	// 5. Initialize authenticated Spotify client
	httpClient, _ := oauthFlow.Client(ctx, cfg.CurrentToken())
	spotifyClient := spotify.NewClient(httpClient)

	// Fetch current user
	userCtx, cancelUser := context.WithTimeout(startupCtx, 5*time.Second)
	user, err := spotifyClient.GetCurrentUser(userCtx)
	cancelUser()
	if err != nil {
		var apiErr *spotify.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized {
			// If token was rejected with 401, re-authenticate once
			fmt.Printf("Session expired (401), re-authenticating: %v\n", err)
			if _, err := oauthFlow.RunInteractiveLogin(ctx); err != nil {
				return fmt.Errorf("authentication failed: %w", err)
			}
			// Human interaction does not consume the network startup budget.
			cancelStartup()
			startupCtx, cancelStartup = context.WithTimeout(ctx, 30*time.Second)
			defer cancelStartup()
			httpClient, _ = oauthFlow.Client(ctx, cfg.CurrentToken())
			spotifyClient = spotify.NewClient(httpClient)
			retryUserCtx, cancelRetryUser := context.WithTimeout(startupCtx, 5*time.Second)
			user, err = spotifyClient.GetCurrentUser(retryUserCtx)
			cancelRetryUser()
			if err != nil {
				return formatStartupError("failed to connect to Spotify", err)
			}
		} else {
			return formatStartupError("failed to connect to Spotify", err)
		}
	}

	// 6. Start PulseAudio audio sink
	playerEngine, err := configuredEngine(cfg)
	if err != nil {
		return fmt.Errorf("failed to configure Spotify Connect identity: %w", err)
	}
	if volume := cfg.CurrentVolume(); volume >= 0 {
		playerEngine.SetVolume(volume)
	}
	if err := playerEngine.Start(ctx, user.ID, cfg.CurrentToken().AccessToken); err != nil {
		fmt.Printf("Notice: PulseAudio audio sink initialization failed (%v)\n", err)
	}
	defer func() {
		// Cancel pending UI and OAuth requests before releasing the audio daemon.
		cancelRun()
		if err := playerEngine.Close(); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("failed to shut down audio player: %w", err))
		}
	}()

	// 7. Fetch playlist tracks
	fmt.Printf("Loading playlist %s...\n", playlistID)
	playlistCtx, cancelPlaylist := context.WithTimeout(startupCtx, 15*time.Second)
	playlist, err := spotifyClient.GetPlaylist(playlistCtx, playlistID)
	cancelPlaylist()
	if err != nil {
		return formatStartupError("failed to load playlist", err)
	}

	// Persist last played playlist
	if err := cfg.SetLastPlaylist(playlistID); err != nil {
		return fmt.Errorf("failed to save last playlist: %w", err)
	}

	// 8. Find target device (rukia or active device)
	deviceCtx, cancelDeviceDiscovery := context.WithTimeout(startupCtx, 5*time.Second)
	defer cancelDeviceDiscovery()
	targetDeviceID := discoverDevice(deviceCtx, spotifyClient, playerEngine.DeviceName(), 8, 500*time.Millisecond)

	// 9. Start initial playback
	playbackCtx, cancelPlayback := context.WithTimeout(startupCtx, 5*time.Second)
	initialTrackOffset := 0
	if len(playlist.Tracks) > 0 {
		initialTrackOffset = playlist.Tracks[0].PlaylistPosition
	}
	playbackErr := startInitialPlayback(playbackCtx, spotifyClient, targetDeviceID, playlist.URI, initialTrackOffset)
	cancelPlayback()
	if playbackErr != nil {
		fmt.Printf("Notice: could not start initial playback (%v). Starting TUI for manual playback.\n", playbackErr)
	}

	// 10. Start Bubble Tea TUI
	model := ui.NewModel(spotifyClient, playerEngine, user, playlist, targetDeviceID, cfg)
	model.SetWarningChannel(ctx, oauthFlow.Warnings())
	if playbackErr != nil {
		model.SetPlaybackInitialState(false)
		model.SetInitialError(playbackErr)
	} else if len(playlist.Tracks) > 0 {
		model.SetInitialPlaybackTrack(0)
	}
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(ctx))

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	return nil
}

func printWarning(w io.Writer, warning error) error {
	if warning != nil {
		if _, err := fmt.Fprintf(w, "Warning: %v\n", warning); err != nil {
			return fmt.Errorf("failed to display Spotify token persistence warning: %w", err)
		}
	}
	return nil
}

func configuredEngine(cfg *auth.Config) (*player.Engine, error) {
	deviceID, err := cfg.EnsureDeviceID(player.LegacyDeviceID())
	if err != nil {
		return nil, err
	}
	return player.NewEngine("rukia", player.WithDeviceID(deviceID)), nil
}

func formatStartupError(phase string, err error) error {
	var apiErr *spotify.APIError
	if errors.As(err, &apiErr) {
		if apiErr.StatusCode == http.StatusTooManyRequests && apiErr.RetryAfter > 0 {
			return fmt.Errorf("%s: Spotify rate limit reached (Retry-After: %v); please wait before retrying: %w", phase, apiErr.RetryAfter, err)
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: request timed out while connecting to Spotify: %w", phase, err)
	}
	return fmt.Errorf("%s: %w", phase, err)
}

type deviceLister interface {
	GetDevices(context.Context) ([]spotify.Device, error)
}

type playbackStarter interface {
	TransferPlayback(context.Context, string, bool) error
	PlayPlaylist(context.Context, string, string, int) error
}

var initialPlaybackDelay = 250 * time.Millisecond

func startInitialPlayback(ctx context.Context, client playbackStarter, deviceID, playlistURI string, trackOffset int) error {
	var transferErr error
	if deviceID != "" {
		transferErr = client.TransferPlayback(ctx, deviceID, true)
		if initialPlaybackDelay > 0 {
			select {
			case <-time.After(initialPlaybackDelay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	playErr := client.PlayPlaylist(ctx, deviceID, playlistURI, trackOffset)
	if playErr != nil {
		if transferErr != nil {
			return fmt.Errorf("failed to start playlist: %w", errors.Join(
				fmt.Errorf("transfer playback: %w", transferErr),
				fmt.Errorf("play playlist: %w", playErr),
			))
		}
		return fmt.Errorf("failed to start playlist: %w", playErr)
	}
	return nil
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
			fallback = ""
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
