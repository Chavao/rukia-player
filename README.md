# rukia-player (`rukia`)

A lightweight, terminal-based Spotify music player written in Golang using [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Lip Gloss](https://github.com/charmbracelet/lipgloss), and [go-librespot](https://github.com/devgianlu/go-librespot).

The application compiles to the command-line binary `rukia` and starts playing a specified Spotify playlist immediately upon invocation.

## Features

- **Native Terminal Playback**: Embeds a Spotify Connect receiver using `go-librespot` with PulseAudio/PipeWire audio output.
- **Spotify Web API Integration**: Full OAuth 2.0 authorization code flow with automatic token persistence and refresh.
- **HTTPS Callback Server**: Built-in HTTPS callback handler with ephemeral in-memory TLS certificate on `https://127.0.0.1:8443/callback`.
- **Flexible Playlist Arguments**: Supports raw playlist IDs, IDs with query parameters, full Spotify URLs, and Spotify URIs.
- **Cyan TUI Theme**: Designed to match the Spotify CLI theme with header stats, track listing (Artist - Album, Title, Duration), and bottom progress bar.
- **Exit Confirmation Dialog**: Modal dialog (`Ctrl+q`, `q`, `Ctrl+c`) with `<No>` and `<Yes>` confirmation buttons.

## Prerequisites

1. **Spotify Premium Account**: Required by Spotify for playback control and streaming.
2. **Spotify Developer Application**:
   - Register an application at [Spotify Developer Dashboard](https://developer.spotify.com/dashboard).
   - Set **Redirect URI** to: `https://127.0.0.1:8443/callback` (or your configured port).
   - Select **Web API**.

## Installation & Build

Build the binary using `make`:

```bash
git clone https://github.com/chavao/rukia-player.git
cd rukia-player
make build
```

The compiled executable will be placed in `./bin/rukia`.

To install globally to your `$GOPATH/bin`:

```bash
go install ./cmd/rukia
```

## Quick Start

Run `rukia` with your desired playlist ID or URL:

```bash
rukia '6UUCMxk575eDTwSWa0qQhB?si=fa799fec9a404660'
```

Or using standard Spotify playlist formats:

```bash
# Pure ID
rukia 6UUCMxk575eDTwSWa0qQhB

# Web URL
rukia https://open.spotify.com/playlist/6UUCMxk575eDTwSWa0qQhB

# Spotify URI
rukia spotify:playlist:6UUCMxk575eDTwSWa0qQhB
```

### First-Time Configuration

On first launch, if no credentials are configured, `rukia` interactively requests:
- **Client ID**
- **Client Secret**

These credentials, along with access and refresh tokens, are securely stored in `~/.config/rukia/config.json` with restricted permissions (`0600`).

Alternatively, credentials can be provided via environment variables:
- `SPOTIFY_CLIENT_ID`
- `SPOTIFY_CLIENT_SECRET`
- `SPOTIFY_REDIRECT_URI` (optional, defaults to `https://127.0.0.1:8443/callback`)

## Keyboard Controls

| Key | Action |
| --- | --- |
| `↑` / `k` | Move cursor up in track list |
| `↓` / `j` | Move cursor down in track list |
| `Enter` | Contextual Play/Pause (toggles current track or plays selected track) |
| `Space` | Toggle Play / Pause |
| `+` / `=` | Increase volume |
| `-` / `_` | Decrease volume |
| `s` | Toggle shuffle |
| `r` | Toggle repeat mode |
| `Ctrl+q` / `q` / `Ctrl+c` | Open exit confirmation dialog |
| `←` / `→` | Toggle between `<No>` and `<Yes>` in dialog |
| `Enter` (in dialog) | Confirm selected option |
| `Esc` | Cancel dialog and return to player |

## Architecture

- **`cmd/rukia/`**: Application entry point, CLI flag and argument handling.
- **`internal/auth/`**: Spotify OAuth 2.0 flow, in-memory TLS certificate generation, HTTPS callback server, configuration persistence.
- **`internal/spotify/`**: Spotify Web API client, playlist metadata and track pagination, player controls.
- **`internal/player/`**: Embedded `go-librespot` daemon lifecycle and PulseAudio sink.
- **`internal/ui/`**: Bubble Tea model, Lip Gloss styles, exit modal overlay, and renderers.
- **`internal/util/`**: Time, duration, and progress bar formatting utilities.

## License

MIT
