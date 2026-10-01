package auth

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/oauth2"
)

const testDeviceID = "0123456789abcdef0123456789abcdef01234567"

func TestDeviceIDMigrationPersistsLegacyIdentity(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := GetConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"client_id":"old-client","volume":0,"last_playlist":"old-playlist"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	id, err := cfg.EnsureDeviceID(testDeviceID)
	if err != nil || id != testDeviceID {
		t.Fatalf("migration: id=%q err=%v", id, err)
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	loaded, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	id, err = loaded.EnsureDeviceID(strings.Repeat("a", 40))
	if err != nil || id != testDeviceID {
		t.Fatalf("cache relocation replaced persisted identity: id=%q err=%v", id, err)
	}
	if loaded.ClientID != "old-client" || loaded.Volume != 0 || loaded.LastPlaylist != "old-playlist" {
		t.Fatalf("migration lost existing config: %+v", loaded)
	}
}

func TestNewInstallationDeviceIDIsStable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DeviceID != "" {
		t.Fatal("new installation already has a device identity")
	}
	if _, err := cfg.EnsureDeviceID(testDeviceID); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	id, err := loaded.EnsureDeviceID("")
	if err != nil || id != testDeviceID {
		t.Fatalf("new installation identity not durable: id=%q err=%v", id, err)
	}
}

func TestDeviceIDValidation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, id := range []string{"short", strings.Repeat("z", 40), strings.Repeat("a", 41), " " + testDeviceID} {
		t.Run(id, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.DeviceID = id
			if _, err := cfg.EnsureDeviceID(testDeviceID); err == nil {
				t.Fatal("invalid saved identity was replaced silently")
			}
			if cfg.DeviceID != id {
				t.Fatal("invalid saved identity was modified")
			}
			if err := cfg.Save(); err == nil {
				t.Fatal("invalid saved identity accepted by Save")
			}
			path, err := GetConfigPath()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(`{"device_id":"`+id+`"}`), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadConfig(); err == nil {
				t.Fatal("invalid persisted identity accepted by LoadConfig")
			}
		})
	}
	for _, id := range []string{"", "invalid"} {
		cfg := DefaultConfig()
		if _, err := cfg.EnsureDeviceID(id); err == nil || cfg.DeviceID != "" {
			t.Fatalf("invalid migration input %q accepted or changed config: %v", id, err)
		}
	}
}

func TestEmptyPersistedDeviceIDMigrates(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := GetConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"device_id":""}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if id, err := cfg.EnsureDeviceID(testDeviceID); err != nil || id != testDeviceID {
		t.Fatalf("empty saved identity migration: id=%q err=%v", id, err)
	}
}

func TestDeviceIDMigrationSaveFailureRollsBack(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := GetConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	// A directory at the target path deterministically rejects atomic rename.
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	if id, err := cfg.EnsureDeviceID(testDeviceID); err == nil || id != "" || cfg.DeviceID != "" {
		t.Fatalf("failed migration changed identity: id=%q config=%q err=%v", id, cfg.DeviceID, err)
	}
	entries, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".config-*"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed migration left temporary files: entries=%v err=%v", entries, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if id, err := cfg.EnsureDeviceID(testDeviceID); err != nil || id != testDeviceID {
		t.Fatalf("retry after migration failure: id=%q err=%v", id, err)
	}
}

func TestConcurrentDeviceIDMigrationAndConfigSaves(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := DefaultConfig()
	start := make(chan struct{})
	errs := make(chan error, 4)
	ids := make(chan string, 2)
	var wg sync.WaitGroup
	for _, id := range []string{testDeviceID, strings.Repeat("a", 40)} {
		wg.Go(func() {
			<-start
			deviceID, err := cfg.EnsureDeviceID(id)
			ids <- deviceID
			errs <- err
		})
	}
	wg.Go(func() { <-start; errs <- cfg.SetVolume(0) })
	wg.Go(func() { <-start; errs <- cfg.SetToken(&oauth2.Token{AccessToken: "concurrent-token"}) })
	close(start)
	wg.Wait()
	for range 4 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	first, second := <-ids, <-ids
	if first == "" || first != second {
		t.Fatalf("concurrent migrations disagree: %q vs %q", first, second)
	}
	loaded, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DeviceID != first || loaded.Volume != 0 || loaded.Token == nil || loaded.Token.AccessToken != "concurrent-token" {
		t.Fatalf("concurrent updates were lost: device=%q volume=%d token=%v", loaded.DeviceID, loaded.Volume, loaded.Token)
	}
}
