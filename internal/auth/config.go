package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/oauth2"
)

const (
	DefaultRedirectURI = "https://127.0.0.1:8443/callback"
	DefaultVolume      = 100
	configDirName      = "rukia"
	configFileName     = "config.json"
)

// Config represents persistent application configuration, credentials, and OAuth tokens.
type Config struct {
	ClientID     string        `json:"client_id"`
	ClientSecret string        `json:"client_secret"`
	RedirectURI  string        `json:"redirect_uri"`
	Token        *oauth2.Token `json:"token,omitempty"`
	LastPlaylist string        `json:"last_playlist,omitempty"`
	Volume       int           `json:"volume"`
}

// DefaultConfig returns an initialized configuration with fallback defaults.
func DefaultConfig() *Config {
	return &Config{
		RedirectURI: DefaultRedirectURI,
		Volume:      DefaultVolume,
	}
}

// GetConfigDir returns the full path to the configuration directory and ensures it exists.
func GetConfigDir() (string, error) {
	baseDir, err := os.UserConfigDir()
	if err != nil {
		homeDir, hErr := os.UserHomeDir()
		if hErr != nil {
			return "", fmt.Errorf("unable to determine user configuration or home directory: %w", err)
		}
		baseDir = filepath.Join(homeDir, ".config")
	}

	appDir := filepath.Join(baseDir, configDirName)
	if err := os.MkdirAll(appDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create config directory %s: %w", appDir, err)
	}

	return appDir, nil
}

// GetConfigPath returns the absolute path to the config.json file.
func GetConfigPath() (string, error) {
	dir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configFileName), nil
}

type configDTO struct {
	ClientID     string        `json:"client_id"`
	ClientSecret string        `json:"client_secret"`
	RedirectURI  string        `json:"redirect_uri"`
	Token        *oauth2.Token `json:"token,omitempty"`
	LastPlaylist string        `json:"last_playlist,omitempty"`
	Volume       *int          `json:"volume"`
}

// LoadConfig loads configuration from ~/.config/rukia/config.json, applying environment overrides.
func LoadConfig() (*Config, error) {
	cfg := DefaultConfig()

	path, err := GetConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Apply environment variables even if file does not exist yet.
			applyEnvOverrides(cfg)
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	var dto configDTO
	if err := json.Unmarshal(data, &dto); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	cfg.ClientID = dto.ClientID
	cfg.ClientSecret = dto.ClientSecret
	cfg.RedirectURI = dto.RedirectURI
	cfg.Token = dto.Token
	cfg.LastPlaylist = dto.LastPlaylist

	if cfg.RedirectURI == "" {
		cfg.RedirectURI = DefaultRedirectURI
	}

	if dto.Volume != nil {
		vol := *dto.Volume
		if vol < 0 {
			vol = 0
		} else if vol > 100 {
			vol = 100
		}
		cfg.Volume = vol
	} else {
		cfg.Volume = DefaultVolume
	}

	applyEnvOverrides(cfg)
	return cfg, nil
}

// Save writes the configuration to disk with secure 0600 permissions.
func (c *Config) Save() error {
	path, err := GetConfigPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file %s: %w", path, err)
	}

	return nil
}

// HasCredentials returns true if ClientID and ClientSecret are set.
func (c *Config) HasCredentials() bool {
	return c.ClientID != "" && c.ClientSecret != ""
}

func applyEnvOverrides(cfg *Config) {
	if envID := os.Getenv("SPOTIFY_CLIENT_ID"); envID != "" {
		cfg.ClientID = envID
	}
	if envSecret := os.Getenv("SPOTIFY_CLIENT_SECRET"); envSecret != "" {
		cfg.ClientSecret = envSecret
	}
	if envRedirect := os.Getenv("SPOTIFY_REDIRECT_URI"); envRedirect != "" {
		cfg.RedirectURI = envRedirect
	}
}
