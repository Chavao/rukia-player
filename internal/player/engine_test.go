package player

import (
	"context"
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

	// Pause and resume on unstarted engine should be safe
	engine.Pause()
	engine.Resume()
}
