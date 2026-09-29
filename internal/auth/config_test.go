package auth

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/oauth2"
)

func TestConfigLoadAndSave(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error loading non-existent config: %v", err)
	}

	if cfg.RedirectURI != DefaultRedirectURI {
		t.Errorf("expected default redirect URI %s, got %s", DefaultRedirectURI, cfg.RedirectURI)
	}
	if cfg.HasCredentials() {
		t.Error("expected new config to not have credentials")
	}

	cfg.ClientID = "test_client_id"
	cfg.ClientSecret = "test_client_secret"
	cfg.LastPlaylist = "37i9dQZF1DXcBWIGoYBM5M"
	cfg.Token = &oauth2.Token{
		AccessToken: "mock_access_token",
	}

	if err := cfg.Save(); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	cfgPath := filepath.Join(tempDir, configDirName, configFileName)
	fi, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("config file was not created: %v", err)
	}
	// Check permissions 0600
	if fi.Mode().Perm() != 0600 {
		t.Errorf("expected file mode 0600, got %v", fi.Mode().Perm())
	}

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}

	if loaded.ClientID != "test_client_id" || loaded.ClientSecret != "test_client_secret" {
		t.Errorf("loaded credentials mismatch")
	}
	if loaded.LastPlaylist != "37i9dQZF1DXcBWIGoYBM5M" {
		t.Errorf("loaded playlist mismatch: %s", loaded.LastPlaylist)
	}
	if loaded.Token == nil || loaded.Token.AccessToken != "mock_access_token" {
		t.Errorf("loaded token mismatch")
	}
}

func TestConfigEnvOverrides(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempDir)
	t.Setenv("SPOTIFY_CLIENT_ID", "env_id")
	t.Setenv("SPOTIFY_CLIENT_SECRET", "env_secret")
	t.Setenv("SPOTIFY_REDIRECT_URI", "https://127.0.0.1:9999/callback")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("failed to load config with env: %v", err)
	}

	if cfg.ClientID != "env_id" {
		t.Errorf("expected client ID 'env_id', got %s", cfg.ClientID)
	}
	if cfg.ClientSecret != "env_secret" {
		t.Errorf("expected client secret 'env_secret', got %s", cfg.ClientSecret)
	}
	if cfg.RedirectURI != "https://127.0.0.1:9999/callback" {
		t.Errorf("expected redirect URI 'https://127.0.0.1:9999/callback', got %s", cfg.RedirectURI)
	}
	if !cfg.HasCredentials() {
		t.Error("expected HasCredentials to be true with env overrides")
	}
}

func TestConfigZeroVolumeMute(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	cfg := DefaultConfig()
	cfg.Volume = 0
	if err := cfg.Save(); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if loaded.Volume != 0 {
		t.Errorf("expected volume 0 (mute) to be preserved, got %d", loaded.Volume)
	}
}

