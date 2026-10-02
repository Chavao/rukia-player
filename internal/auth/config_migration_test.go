package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigPathUsesPlayerDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	path, err := GetConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".config", "rukia", "player", "config.json"); path != want {
		t.Fatalf("config path = %q, want %q", path, want)
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Fatalf("config directory mode = %v, want 0700", info.Mode().Perm())
	}
}

func TestConfigMigratesLegacySettings(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("SPOTIFY_CLIENT_ID", "environment-client")
	t.Setenv("SPOTIFY_CLIENT_SECRET", "")
	t.Setenv("SPOTIFY_REDIRECT_URI", "")
	legacyPath := filepath.Join(base, "rukia", "config.json")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"client_id":"saved-client","client_secret":"saved-secret","auth_flow":"pkce","device_id":"` + strings.Repeat("a", 40) + `","redirect_uri":"http://127.0.0.1:9999/callback","volume":0,"last_playlist":"saved-playlist","token":{"access_token":"saved-access","refresh_token":"saved-refresh"}}`)
	if err := os.WriteFile(legacyPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClientID != "environment-client" {
		t.Fatal("environment override was not applied")
	}
	t.Setenv("SPOTIFY_CLIENT_ID", "")
	// A later change to the old file must not replace the migrated config.
	if err := os.WriteFile(legacyPath, []byte(`{"client_id":"obsolete-client"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClientID != "saved-client" || cfg.ClientSecret != "saved-secret" || cfg.AuthFlow != "pkce" || cfg.DeviceID != strings.Repeat("a", 40) || cfg.RedirectURI != "http://127.0.0.1:9999/callback" || cfg.Volume != 0 || cfg.LastPlaylist != "saved-playlist" || cfg.Token == nil || cfg.Token.AccessToken != "saved-access" || cfg.Token.RefreshToken != "saved-refresh" {
		t.Fatal("migration did not preserve saved settings")
	}
	info, err := os.Stat(filepath.Join(base, "rukia", "player", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("migrated config mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestConfigRejectsInvalidLegacyConfig(t *testing.T) {
	for _, data := range []string{`{`, `{"device_id":"invalid"}`} {
		t.Run(data, func(t *testing.T) {
			base := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", base)
			legacyPath := filepath.Join(base, "rukia", "config.json")
			if err := os.MkdirAll(filepath.Dir(legacyPath), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(legacyPath, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadConfig(); err == nil {
				t.Fatal("invalid legacy config was accepted")
			}
			if _, err := os.Stat(filepath.Join(base, "rukia", "player", "config.json")); !os.IsNotExist(err) {
				t.Fatalf("invalid config was migrated: %v", err)
			}
		})
	}
}
