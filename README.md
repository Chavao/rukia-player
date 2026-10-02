# rukia-player

<img width="1916" height="821" alt="image" src="https://github.com/user-attachments/assets/42a0f5b2-d4c8-43d7-a365-56de6ab648e0" />


A lightweight, terminal-based Spotify music player written in Golang using [Bubble Tea](https://github.com/charmbracelet/bubbletea), [Lip Gloss](https://github.com/charmbracelet/lipgloss), and [go-librespot](https://github.com/devgianlu/go-librespot).

The application compiles to the command-line binary `rukia-player` and starts playing a specified Spotify playlist immediately upon invocation.

## Why `rukia-player`?

The project is named after Rukia Kuchiki from *Bleach*. The connection is
thematic rather than literal: Rukia is strongly associated with precision,
control, deliberate movement, and the named dances of Sode no Shirayuki.

Those ideas map naturally to a music player. Playback is also built around
sequence and timing: tracks move forward, pause, resume, repeat, shuffle, and
transition while remaining under explicit user control.

| Theme | Rukia / Bleach reference | How it maps to the player |
| --- | --- | --- |
| Rhythm and sequence | Sode no Shirayuki's techniques are expressed as named dances | A playlist is an ordered sequence of tracks, with playback moving through that sequence according to the listener's actions |
| Precision and control | Rukia's techniques rely on deliberate, controlled execution | Playback controls are direct and predictable: play, pause, track selection, shuffle, repeat, and volume |
| Flow between movements | Each dance is a distinct technique within the same fighting style | Individual tracks and playback states remain separate operations while forming one continuous listening session |
| Ice and visual identity | Sode no Shirayuki is an ice-type Zanpakuto | The cyan terminal interface and restrained dark palette give `rukia-player` a cold, minimal visual identity |
| Restraint over excess | Rukia's style emphasizes technique and composure | `rukia-player` is intentionally lightweight and focused, providing the essential music-player experience without unnecessary interface complexity |

The name reflects the experience the project aims for: **controlled playback,
rhythm, precision, and minimal distraction**.

The *Bleach* references are part of the project's identity rather than its
internal vocabulary. Packages remain conventional names such as `player`,
`spotify`, `auth`, and `ui`, keeping the codebase understandable regardless of
whether someone is familiar with the series.

## Features

- **Native Terminal Playback**: Embeds a Spotify Connect receiver using `go-librespot` with PulseAudio/PipeWire audio output.
- **Spotify Web API Integration**: OAuth 2.0 authorization code flow with PKCE, automatic token refresh, structured error parsing, and GET retries that respect Spotify's rate limits. Playback controls retry only selected transient failures with a bounded budget.
- **OAuth Callback Server**: Loopback-restricted callback handler with explicit listener shutdown. Existing HTTPS configurations continue to use an ephemeral in-memory TLS certificate.
- **Flexible Playlist Arguments**: Supports raw playlist IDs, IDs with query parameters, full Spotify URLs (`open.spotify.com`), and Spotify URIs.
- **Cyan TUI Theme**: Header stats, track listing (Artist - Album, Title, Duration), bold cyan selection text on a dark blue background, shuffle/repeat badges, and bottom progress bar with transient error notices.
- **Exact Playlist Selection**: Playing a selected row starts that occurrence, including songs repeated in the same playlist. The playing checkmark stays on the acknowledged occurrence while the cursor moves independently.
- **Unplayable Track Handling**: Tracks restricted or unavailable in the active account's market are visually dimmed in the track table and automatically bypassed during keyboard navigation.
- **Exit Confirmation Dialog**: Modal dialog (`Ctrl+q`, `q`, `Ctrl+c`) with `<No>` and `<Yes>` confirmation buttons.
- **Session Reuse & Migration**: Seamlessly reuses existing cached credentials from ncspot (`~/.cache/ncspot/librespot/credentials.json`) to minimize re-authentication friction for transitioning users.

## Prerequisites

1. **Spotify Premium Account**: Required by Spotify for playback control and streaming.
2. **Spotify Developer Application**:
   - Register an application at [Spotify Developer Dashboard](https://developer.spotify.com/dashboard).
   - Set **Redirect URI** to: `http://127.0.0.1:8443/callback` (or your configured loopback IP and port).
   - Select **Web API**.

## Installation & Build

Build the binary using `make`:

```bash
git clone https://github.com/chavao/rukia-player.git
cd rukia-player
make check  # runs formatting, vet, and race detection
make build
```

The compiled executable will be placed in `./bin/rukia-player`.

To install globally to your `$GOPATH/bin`:

```bash
make install
```

## Quick Start

Run `rukia-player` with your desired playlist ID or URL:

```bash
rukia-player '6UUCMxk575eDTwSWa0qQhB?si=fa799fec9a404660'
```

Or using standard Spotify playlist formats:

```bash
# Pure ID
rukia-player 6UUCMxk575eDTwSWa0qQhB

# Web URL
rukia-player https://open.spotify.com/playlist/6UUCMxk575eDTwSWa0qQhB

# Spotify URI
rukia-player spotify:playlist:6UUCMxk575eDTwSWa0qQhB
```

### First-Time Configuration

On first launch, if no Client ID is configured, `rukia-player` interactively requests:
- **Client ID**

The Client ID and access and refresh tokens are stored in `~/.config/rukia/config.json` with restricted permissions (`0600`). A client secret is not needed for new PKCE logins. Existing configurations with a client secret continue to refresh legacy tokens; the stored secret is removed after a new PKCE login.

Expired sessions are refreshed silently with an eight second timeout. A revoked refresh token prompts for login; rate limits, server failures, and network errors are reported without launching the browser. Token persistence warnings appear in the TUI and as a final reminder after the terminal is restored, so quitting cannot discard a warning waiting for display.

Spotify Connect identity is stored as `device_id` in the same configuration. On the first launch after upgrading, Rukia saves the existing device identity derived from the current cache location. Later cache directory changes preserve that identity. An invalid stored identity is reported as a configuration error; restore a valid value or remove the field to migrate from the current cache location again.

Alternatively, credentials can be provided via environment variables:
- `SPOTIFY_CLIENT_ID`
- `SPOTIFY_CLIENT_SECRET` (optional, for legacy token refresh)
- `SPOTIFY_REDIRECT_URI` (optional, defaults to `http://127.0.0.1:8443/callback`)

Existing configured HTTPS redirect URIs remain supported. Register the exact URI in the Spotify Developer Dashboard. Spotify requires a literal loopback IP for new redirect URI registrations.

Both `127.0.0.1` and `::1` are supported for loopback callbacks, including HTTPS certificate hostnames. `localhost` redirect URIs are rejected.

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

Shuffle displays `[?]` until Spotify reports its state. Pressing `s` before synchronization queues your intent: an odd number of presses toggles the observed state once; an even number leaves it unchanged. Later Spotify shuffle changes are reflected in the badge.

`Enter` pauses or resumes the playing occurrence when its row is selected; selecting another row starts that exact playlist position. If Spotify reports a repeated song without a confirmed local selection, Rukia leaves the row checkmark unresolved and continues showing the current song in the bottom bar. Spotify does not report occurrence identity, so external jumps or automatic transitions between identical songs cannot distinguish their rows. Exact selection uses the playlist as loaded; playlist edits made elsewhere require reloading it.

## Architecture

- **`cmd/rukia/`**: Application entry point.
- **`internal/app/`**: Application lifecycle orchestration, CLI argument parsing, and error boundaries.
- **`internal/auth/`**: Spotify OAuth 2.0 PKCE flow, callback server, optional in-memory TLS certificate generation, configuration persistence.
- **`internal/spotify/`**: Spotify Web API client, playlist metadata and track pagination, player controls.
- **`internal/player/`**: Embedded `go-librespot` daemon lifecycle and PulseAudio sink.
- **`internal/ui/`**: Bubble Tea model, Lip Gloss styles, exit modal overlay, and renderers.
- **`internal/util/`**: Time, duration, and progress bar formatting utilities.

## License

MIT
