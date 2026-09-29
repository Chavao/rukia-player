package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/oauth2"
)

const (
	DefaultRedirectURI = "http://127.0.0.1:8443/callback"
	DefaultVolume      = 100
	configDirName      = "rukia"
	configFileName     = "config.json"
)

// Config represents persistent application configuration, credentials, and OAuth tokens.
type Config struct {
	mu           sync.Mutex
	ClientID     string        `json:"client_id"`
	ClientSecret string        `json:"client_secret,omitempty"`
	AuthFlow     string        `json:"auth_flow,omitempty"`
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
	ClientSecret string        `json:"client_secret,omitempty"`
	AuthFlow     string        `json:"auth_flow,omitempty"`
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
	cfg.AuthFlow = dto.AuthFlow
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
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.saveLocked()
}

// CurrentVolume returns the saved player volume.
func (c *Config) CurrentVolume() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Volume
}

// SetVolume persists a volume change together with the latest token.
func (c *Config) SetVolume(volume int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Volume = volume
	return c.saveLocked()
}

// SetToken persists a refreshed OAuth token together with other config changes.
func (c *Config) SetToken(token *oauth2.Token) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Token = token
	return c.saveLocked()
}

// SetPKCEToken records a new PKCE token and its refresh method together.
func (c *Config) SetPKCEToken(token *oauth2.Token) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Token = token
	c.AuthFlow = "pkce"
	c.ClientSecret = ""
	return c.saveLocked()
}

// CurrentToken returns the latest OAuth token.
func (c *Config) CurrentToken() *oauth2.Token {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Token
}

// SetLastPlaylist persists the most recently played playlist.
func (c *Config) SetLastPlaylist(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.LastPlaylist = id
	return c.saveLocked()
}

func (c *Config) saveLocked() error {
	path, err := GetConfigPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}

	f, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("failed to write config file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close config file: %w", err)
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("failed to replace config file %s: %w", path, err)
	}

	return nil
}

// HasCredentials returns true when the public Spotify Client ID is set.
func (c *Config) HasCredentials() bool {
	return c.ClientID != ""
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
