package player

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
)

// EngineOption configures the player without coupling it to application config.
type EngineOption func(*Engine)

// WithDeviceID selects the application's persisted Spotify Connect identity.
func WithDeviceID(deviceID string) EngineOption {
	return func(engine *Engine) { engine.deviceID = deviceID }
}

// LegacyDeviceID returns the exact identity advertised before identity migration.
// Application startup persists this value once and supplies it with WithDeviceID.
func LegacyDeviceID() string {
	return legacyDeviceID(defaultCacheDir())
}

func legacyDeviceID(cacheDir string) string {
	sum := sha1.Sum([]byte("rukia-player-device-" + cacheDir))
	return hex.EncodeToString(sum[:])
}

func validateDeviceID(id string) error {
	// An omitted constructor option preserves legacy callers.
	if id == "" {
		return nil
	}
	if len(id) != 40 {
		return errors.New("Spotify Connect device ID must contain 40 hexadecimal characters")
	}
	if _, err := hex.DecodeString(id); err != nil {
		return errors.New("Spotify Connect device ID must contain 40 hexadecimal characters")
	}
	return nil
}
