package player

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileStateStoreIgnoresExternalClientCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	externalDir := filepath.Join(home, ".cache", "ncspot", "librespot")
	if err := os.MkdirAll(externalDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(externalDir, "credentials.json"), []byte(`{"username":"external-user","auth_data":"ZXh0ZXJuYWwtY3JlZGVudGlhbHM="}`), 0600); err != nil {
		t.Fatal(err)
	}
	state, err := NewFileStateStore(t.TempDir()).Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Credentials.Username != "" || len(state.Credentials.Data) != 0 {
		t.Fatal("state store imported external client credentials")
	}
}
