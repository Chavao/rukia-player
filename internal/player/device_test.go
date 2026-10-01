package player

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyDeviceIDPreservesOriginalAlgorithm(t *testing.T) {
	if got := legacyDeviceID("/tmp/legacy-cache"); got != "ebcf3b093748b6f5905eb662196cb126ae3d35ca" {
		t.Fatalf("legacy identity changed: %q", got)
	}
	t.Setenv("HOME", t.TempDir())
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	// Released main hashed this home-relative path before XDG cache support.
	sum := sha1.Sum([]byte("rukia-player-device-" + filepath.Join(home, ".cache", "rukia", "librespot")))
	want := hex.EncodeToString(sum[:])
	for _, cache := range []string{"", t.TempDir(), t.TempDir()} {
		t.Setenv("XDG_CACHE_HOME", cache)
		if got := LegacyDeviceID(); got != want {
			t.Fatalf("migration with XDG_CACHE_HOME=%q: got=%q want=%q", cache, got, want)
		}
	}
}

func TestSavedDeviceIDIndependentOfCacheDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	id := LegacyDeviceID()
	firstCache := defaultCacheDir()
	first, err := NewFileStateStore(firstCache, id).Load()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	secondCache := defaultCacheDir()
	if firstCache == secondCache {
		t.Fatal("test did not relocate cache storage")
	}
	if LegacyDeviceID() != id {
		t.Fatal("cache relocation changed the upgrade migration identity")
	}
	second, err := NewFileStateStore(secondCache, id).Load()
	if err != nil {
		t.Fatal(err)
	}
	if first.DeviceId != id || second.DeviceId != id {
		t.Fatalf("cache relocation changed saved identity: first=%q second=%q want=%q", first.DeviceId, second.DeviceId, id)
	}
	engine := NewEngine("test", WithDeviceID(id))
	if engine.deviceID != id {
		t.Fatalf("engine did not retain supplied identity: %q", engine.deviceID)
	}
}

func TestLegacyDeviceIDPreservesMissingHomeBehavior(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if _, err := os.UserHomeDir(); err == nil {
		t.Skip("platform resolves the home directory without HOME")
	}
	// Historical main joined an empty home when lookup failed.
	sum := sha1.Sum([]byte("rukia-player-device-" + filepath.Join(".cache", "rukia", "librespot")))
	if got, want := LegacyDeviceID(), hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("missing-home upgrade identity: got=%q want=%q", got, want)
	}
}

func TestFileStateStoreLegacyConstructorIsCompatible(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cacheDir := t.TempDir()
	state, err := NewFileStateStore(cacheDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.DeviceId != legacyDeviceID(cacheDir) {
		t.Fatalf("old constructor changed device identity: %q", state.DeviceId)
	}
	if _, err := hex.DecodeString(state.DeviceId); err != nil {
		t.Fatalf("legacy identity is not hexadecimal: %v", err)
	}
}

func TestInvalidSavedDeviceIDIsRejected(t *testing.T) {
	for _, id := range []string{"short", strings.Repeat("z", 40), strings.Repeat("a", 41)} {
		t.Run(id, func(t *testing.T) {
			if _, err := NewFileStateStore(t.TempDir(), id).Load(); err == nil {
				t.Fatal("invalid saved identity accepted by state store")
			}
			engine := NewEngine("test", WithDeviceID(id))
			if err := engine.Start(context.Background(), "username", "access-token"); err == nil {
				t.Fatal("invalid saved identity accepted by engine")
			}
			if engine.Done() != nil || engine.Running() {
				t.Fatal("validation failure launched daemon")
			}
		})
	}
}
