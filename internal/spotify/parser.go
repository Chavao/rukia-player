package spotify

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var (
	// Spotify alphanumeric base62 ID is typically 22 characters.
	base62Regex = regexp.MustCompile(`^[a-zA-Z0-9]{15,30}$`)
)

// ParsePlaylistID extracts and validates a Spotify playlist ID from:
// - A raw ID: "6UUCMxk575eDTwSWa0qQhB"
// - An ID with query string: "6UUCMxk575eDTwSWa0qQhB?si=fa799fec9a404660"
// - A web URL: "https://open.spotify.com/playlist/6UUCMxk575eDTwSWa0qQhB?si=fa799fec9a404660"
// - A Spotify URI: "spotify:playlist:6UUCMxk575eDTwSWa0qQhB"
func ParsePlaylistID(rawInput string) (string, error) {
	trimmed := strings.TrimSpace(rawInput)
	trimmed = strings.Trim(trimmed, `"'`)

	if trimmed == "" {
		return "", errors.New("empty playlist ID or URL")
	}

	// Handle URI: spotify:playlist:<id>
	if strings.HasPrefix(trimmed, "spotify:playlist:") {
		id := strings.TrimPrefix(trimmed, "spotify:playlist:")
		id = strings.Split(id, "?")[0]
		return validateID(id)
	}

	// Handle Web URL: https://open.spotify.com/playlist/<id>?si=...
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		u, err := url.Parse(trimmed)
		if err != nil {
			return "", fmt.Errorf("invalid URL: %w", err)
		}

		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		for i, part := range parts {
			if part == "playlist" && i+1 < len(parts) {
				return validateID(parts[i+1])
			}
		}

		return "", errors.New("URL does not contain a Spotify playlist path")
	}

	// Handle ID or ID with query string
	id := strings.Split(trimmed, "?")[0]
	return validateID(id)
}

func validateID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if !base62Regex.MatchString(id) {
		return "", fmt.Errorf("invalid Spotify playlist ID format: %q", id)
	}
	return id, nil
}

// FormatPlaylistURI returns the Spotify URI for a playlist ID.
func FormatPlaylistURI(playlistID string) string {
	return "spotify:playlist:" + playlistID
}
