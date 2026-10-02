# Repository Guidelines

rukia-player is a Go terminal Spotify player.

## Project Structure & Module Organization

- The executable entry point is `cmd/rukia-player/main.go`.
- Application code is grouped under `internal/`:
    - `auth` handles Spotify authorization and configuration
    - `spotify` contains API and playlist parsing
    - `player` manages the audio engine
    - `ui` implements the Bubble Tea interface
    - `util` contains shared helpers.
- Tests live beside their packages in `*_test.go` files.

## Security & Configuration

Do not commit Spotify credentials, access tokens, or local configuration. Keep secrets in the application's local configuration flow, and avoid logging credentials or personal data. Validate external API input and handle service failures at their boundaries.

## Rules

Update the README.md file when the code has substantial changes.
