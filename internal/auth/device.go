package auth

import (
	"encoding/hex"
	"errors"
	"fmt"
)

// EnsureDeviceID persists the legacy identity once, then always returns the saved
// identity. The caller supplies the legacy ID to keep configuration independent
// of the player and its cache layout.
func (c *Config) EnsureDeviceID(legacyID string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.DeviceID != "" {
		if err := validateDeviceID(c.DeviceID); err != nil {
			return "", fmt.Errorf("invalid device_id in config: %w", err)
		}
		return c.DeviceID, nil
	}
	if legacyID == "" {
		return "", errors.New("legacy device ID is required for migration")
	}
	if err := validateDeviceID(legacyID); err != nil {
		return "", fmt.Errorf("invalid legacy device ID: %w", err)
	}
	c.DeviceID = legacyID
	if err := c.saveLocked(); err != nil {
		c.DeviceID = ""
		return "", fmt.Errorf("persisting Spotify Connect device ID: %w", err)
	}
	return c.DeviceID, nil
}

func validateDeviceID(id string) error {
	// Empty IDs represent pre-migration configurations.
	if id == "" {
		return nil
	}
	if len(id) != 40 {
		return errors.New("device ID must contain 40 hexadecimal characters")
	}
	if _, err := hex.DecodeString(id); err != nil {
		return errors.New("device ID must contain 40 hexadecimal characters")
	}
	return nil
}
