package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"rukia/internal/auth"
	"rukia/internal/player"
	"rukia/internal/spotify"
	"rukia/internal/ui"
)

const version = "1.0.0"

func printUsage() {
	fmt.Printf("rukia v%s - CLI Spotify Player\n\n", version)
	fmt.Println("Usage:")
	fmt.Println("  rukia <playlist-id-or-url>")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  rukia 6UUCMxk575eDTwSWa0qQhB")
	fmt.Println("  rukia '6UUCMxk575eDTwSWa0qQhB?si=fa799fec9a404660'")
	fmt.Println("  rukia https://open.spotify.com/playlist/6UUCMxk575eDTwSWa0qQhB")
	fmt.Println("  rukia spotify:playlist:6UUCMxk575eDTwSWa0qQhB")
}

func main() {
	var showVersion bool
	var showHelp bool
	flag.BoolVar(&showVersion, "v", false, "Show version")
	flag.BoolVar(&showVersion, "version", false, "Show version")
	flag.BoolVar(&showHelp, "h", false, "Show help")
	flag.BoolVar(&showHelp, "help", false, "Show help")
	flag.Parse()

	if showVersion {
		fmt.Printf("rukia v%s\n", version)
		return
	}
	if showHelp {
		printUsage()
		return
	}

	// 1. Load application config
	cfg, err := auth.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading configuration: %v\n", err)
		os.Exit(1)
	}

	// 2. Parse playlist argument
	var rawPlaylist string
	args := flag.Args()
	if len(args) > 0 {
		rawPlaylist = args[0]
	} else if cfg.LastPlaylist != "" {
		rawPlaylist = cfg.LastPlaylist
		fmt.Printf("Resuming last played playlist: %s\n", rawPlaylist)
	} else {
		printUsage()
		os.Exit(0)
	}

	playlistID, err := spotify.ParsePlaylistID(rawPlaylist)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// 3. Prompt for credentials if first execution
	if err := auth.PromptCredentialsIfMissing(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Credentials setup failed: %v\n", err)
		os.Exit(1)
	}

	// 4. Authenticate via OAuth 2.0 flow if no valid token
	ctx := context.Background()
	oauthFlow := auth.NewOAuthFlow(cfg)

	if cfg.Token == nil || !cfg.Token.Valid() {
		token, err := oauthFlow.RunInteractiveLogin(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Spotify login failed: %v\n", err)
			os.Exit(1)
		}
		cfg.Token = token
	}

	// 5. Initialize authenticated Spotify client
	httpClient, _ := oauthFlow.Client(ctx, cfg.Token)
	spotifyClient := spotify.NewClient(httpClient)

	// Fetch current user
	user, err := spotifyClient.GetCurrentUser(ctx)
	if err != nil {
		// If token was rejected, re-authenticate once
		fmt.Printf("Session expired, re-authenticating: %v\n", err)
		token, err := oauthFlow.RunInteractiveLogin(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Authentication failed: %v\n", err)
			os.Exit(1)
		}
		cfg.Token = token
		httpClient, _ = oauthFlow.Client(ctx, cfg.Token)
		spotifyClient = spotify.NewClient(httpClient)
		user, err = spotifyClient.GetCurrentUser(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to connect to Spotify: %v\n", err)
			os.Exit(1)
		}
	}

	// 6. Start PulseAudio audio sink
	playerEngine := player.NewEngine("rukia")
	if cfg.Volume > 0 {
		playerEngine.SetVolume(cfg.Volume)
	}
	if err := playerEngine.Start(ctx, user.ID, cfg.Token.AccessToken); err != nil {
		fmt.Printf("Notice: PulseAudio audio sink initialization failed (%v)\n", err)
	}
	defer playerEngine.Close()

	// 7. Fetch playlist tracks
	fmt.Printf("Loading playlist %s...\n", playlistID)
	playlist, err := spotifyClient.GetPlaylist(ctx, playlistID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load playlist: %v\n", err)
		os.Exit(1)
	}

	// Persist last played playlist
	cfg.LastPlaylist = playlistID
	_ = cfg.Save()

	// 8. Find target device (rukia or active device)
	var targetDeviceID string
	// Allow a moment for the connect receiver to register
	for attempts := 0; attempts < 8; attempts++ {
		devices, err := spotifyClient.GetDevices(ctx)
		if err == nil {
			for _, d := range devices {
				if d.Name == playerEngine.DeviceName() {
					targetDeviceID = d.ID
					break
				}
			}
			if targetDeviceID != "" {
				break
			}
			// If rukia not found yet but active device exists, pick active
			if attempts == 7 && len(devices) > 0 {
				for _, d := range devices {
					if d.IsActive {
						targetDeviceID = d.ID
						break
					}
				}
				if targetDeviceID == "" {
					targetDeviceID = devices[0].ID
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

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
	model := ui.NewModel(spotifyClient, playerEngine, user, playlist, targetDeviceID)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}
