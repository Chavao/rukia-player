package player

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	librespot "github.com/devgianlu/go-librespot"
)

// FileStateStore manages persistent librespot credentials and state.
type FileStateStore struct {
	mu       sync.Mutex
	state    *librespot.AppState
	cacheDir string
	deviceID string
}

// NewFileStateStore creates a state store targeting cacheDir. An optional saved
// device ID takes precedence over the legacy cache-derived identity.
func NewFileStateStore(cacheDir string, deviceIDs ...string) *FileStateStore {
	store := &FileStateStore{cacheDir: cacheDir}
	for _, deviceID := range deviceIDs {
		store.deviceID = deviceID
	}
	return store
}

// Load retrieves stored application state and cached Spotify credentials.
func (s *FileStateStore) Load() (*librespot.AppState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state != nil {
		return s.state, nil
	}

	state := &librespot.AppState{}

	if err := validateDeviceID(s.deviceID); err != nil {
		return nil, err
	}
	state.DeviceId = s.deviceID
	if state.DeviceId == "" {
		state.DeviceId = legacyDeviceID(s.cacheDir)
	}

	// Search for credentials in rukia-player cache, then fallback candidates.
	candidatePaths := []string{
		filepath.Join(s.cacheDir, "credentials.json"),
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		rukiaCache := filepath.Join(home, ".cache", "rukia", "librespot", "credentials.json")
		if filepath.Clean(rukiaCache) != filepath.Clean(filepath.Join(s.cacheDir, "credentials.json")) {
			candidatePaths = append(candidatePaths, rukiaCache)
		}
	}

	for _, p := range candidatePaths {
		// Cached credentials are optional: unreadable or malformed candidates
		// are skipped so the daemon can use the supplied OAuth access token.
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var raw struct {
			Username string `json:"username"`
			AuthData string `json:"auth_data"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			continue
		}
		blob, err := base64.StdEncoding.DecodeString(raw.AuthData)
		if err != nil {
			continue
		}
		state.Credentials.Username = raw.Username
		state.Credentials.Data = blob
		break
	}

	s.state = state
	return s.state, nil
}

// Save stores application state and credentials back to disk.
func (s *FileStateStore) Save(state *librespot.AppState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state

	if len(state.Credentials.Data) == 0 {
		return nil
	}

	if err := os.MkdirAll(s.cacheDir, 0700); err != nil {
		return err
	}

	raw := struct {
		AuthType int    `json:"auth_type"`
		Username string `json:"username"`
		AuthData string `json:"auth_data"`
	}{
		AuthType: 1,
		Username: state.Credentials.Username,
		AuthData: base64.StdEncoding.EncodeToString(state.Credentials.Data),
	}

	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}

	credPath := filepath.Join(s.cacheDir, "credentials.json")
	if err := os.WriteFile(credPath, data, 0600); err != nil {
		return err
	}
	return os.Chmod(credPath, 0600)
}

// MemoryStateStore satisfies state persistence for unit testing.
type MemoryStateStore struct {
	mu    sync.Mutex
	state *librespot.AppState
}

// Load retrieves in-memory application state.
func (m *MemoryStateStore) Load() (*librespot.AppState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		m.state = &librespot.AppState{}
		hasher := sha1.New()
		hasher.Write([]byte("rukia-memory-state-store"))
		m.state.DeviceId = hex.EncodeToString(hasher.Sum(nil))
	}
	return m.state, nil
}

// Save stores in-memory application state.
func (m *MemoryStateStore) Save(s *librespot.AppState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = s
	return nil
}
