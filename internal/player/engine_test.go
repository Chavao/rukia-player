package player

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestMemoryStateStore(t *testing.T) {
	store := &MemoryStateStore{}
	state, err := store.Load()
	if err != nil {
		t.Fatalf("unexpected error loading state: %v", err)
	}
	if state == nil {
		t.Fatal("expected non-nil state")
	}

	state.DeviceId = "dev-12345"
	if err := store.Save(state); err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("failed to reload state: %v", err)
	}
	if loaded.DeviceId != "dev-12345" {
		t.Errorf("expected deviceId 'dev-12345', got %s", loaded.DeviceId)
	}
}

func TestEngineLifecycle(t *testing.T) {
	engine := NewEngine("rukia-test")
	if engine.DeviceName() != "rukia-test" {
		t.Errorf("expected device name 'rukia-test', got %s", engine.DeviceName())
	}

	// Verify missing credentials error
	err := engine.Start(context.Background(), "", "")
	if err == nil {
		t.Fatal("expected error on empty credentials")
	}

	// Verify close when not running does not panic
	if err := engine.Close(); err != nil {
		t.Fatalf("unexpected error closing stopped engine: %v", err)
	}
}

func TestEngineDefaultName(t *testing.T) {
	engine := NewEngine("")
	if engine.DeviceName() != DefaultDeviceName {
		t.Errorf("expected default device name %s, got %s", DefaultDeviceName, engine.DeviceName())
	}
}

func TestEngineVolume(t *testing.T) {
	engine := NewEngine("rukia-vol-test")
	if engine.Volume() != 100 {
		t.Errorf("expected initial volume 100, got %d", engine.Volume())
	}

	engine.SetVolume(75)
	if engine.Volume() != 75 {
		t.Errorf("expected volume 75, got %d", engine.Volume())
	}

	// Boundary checks
	engine.SetVolume(150)
	if engine.Volume() != 100 {
		t.Errorf("expected clamped volume 100, got %d", engine.Volume())
	}

	engine.SetVolume(-20)
	if engine.Volume() != 0 {
		t.Errorf("expected clamped volume 0, got %d", engine.Volume())
	}
}

func TestEngineErrorsChannel(t *testing.T) {
	engine := NewEngine("rukia-err-test")
	if engine.Errors() == nil {
		t.Fatal("expected non-nil errors channel")
	}
}

func TestFileStateStore(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "rukia-state-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	store := NewFileStateStore(tempDir)
	state, err := store.Load()
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}

	if len(state.DeviceId) != 40 {
		t.Errorf("expected 40-character hex device ID, got %d chars (%s)", len(state.DeviceId), state.DeviceId)
	}

	state.Credentials.Username = "testuser"
	state.Credentials.Data = []byte("test-data-blob")
	if err := store.Save(state); err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	// Reload from new instance pointing to same directory
	reloadedStore := NewFileStateStore(tempDir)
	reloadedState, err := reloadedStore.Load()
	if err != nil {
		t.Fatalf("failed to reload state: %v", err)
	}

	if reloadedState.Credentials.Username != "testuser" {
		t.Errorf("expected username 'testuser', got %s", reloadedState.Credentials.Username)
	}
	if string(reloadedState.Credentials.Data) != "test-data-blob" {
		t.Errorf("expected blob 'test-data-blob', got %s", string(reloadedState.Credentials.Data))
	}
}

func TestDefaultCacheDir(t *testing.T) {
	tempCache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tempCache)

	dir := defaultCacheDir()
	if !strings.HasPrefix(dir, tempCache) {
		t.Errorf("expected cacheDir to use XDG_CACHE_HOME %s, got %s", tempCache, dir)
	}

	t.Setenv("XDG_CACHE_HOME", "")
	dirFallback := defaultCacheDir()
	if dirFallback == "" {
		t.Fatal("expected non-empty fallback cache dir")
	}
}


