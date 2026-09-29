package player

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	librespot "github.com/devgianlu/go-librespot"
	"github.com/devgianlu/go-librespot/daemon"
)

const (
	DefaultDeviceName = "rukia"
	DefaultMediaName  = "rukia Spotify Player"
)

// AppState placeholder for compatibility.
type AppState struct {
	DeviceId string
}

// FileStateStore manages persistent librespot credentials and state.
type FileStateStore struct {
	mu       sync.Mutex
	state    *librespot.AppState
	cacheDir string
}

// NewFileStateStore creates a state store targeting cacheDir.
func NewFileStateStore(cacheDir string) *FileStateStore {
	return &FileStateStore{cacheDir: cacheDir}
}

// Load retrieves stored application state and cached Spotify credentials.
func (s *FileStateStore) Load() (*librespot.AppState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state != nil {
		return s.state, nil
	}

	state := &librespot.AppState{}

	// Generate deterministic 40-char hex device ID
	hasher := sha1.New()
	hasher.Write([]byte("rukia-player-device-" + s.cacheDir))
	state.DeviceId = hex.EncodeToString(hasher.Sum(nil))

	// Search for credentials in rukia cache, then fallback candidates.
	candidatePaths := []string{
		filepath.Join(s.cacheDir, "credentials.json"),
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		rukiaCache := filepath.Join(home, ".cache", "rukia", "librespot", "credentials.json")
		if filepath.Clean(rukiaCache) != filepath.Clean(filepath.Join(s.cacheDir, "credentials.json")) {
			candidatePaths = append(candidatePaths, rukiaCache)
		}
		// NOTE: External client cache migration fallback.
		// Rukia checks ncspot's cache (~/.cache/ncspot/librespot/credentials.json)
		// as a fallback to allow users transitioning from ncspot to reuse credentials.
		ncspotCache := filepath.Join(home, ".cache", "ncspot", "librespot", "credentials.json")
		candidatePaths = append(candidatePaths, ncspotCache)
	}

	for _, p := range candidatePaths {
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

// Engine manages the embedded go-librespot player daemon and PulseAudio output.
type Engine struct {
	deviceName string
	app        *daemon.App
	cancel     context.CancelFunc
	errCh      chan error
	doneCh     chan struct{}
	mu         sync.Mutex
	running    bool
	volume     int // 0 to 100
}

// NewEngine creates a new player engine instance.
func NewEngine(deviceName string) *Engine {
	if deviceName == "" {
		deviceName = DefaultDeviceName
	}
	return &Engine{
		deviceName: deviceName,
		errCh:      make(chan error, 1),
		volume:     100,
	}
}

// DeviceName returns the advertised audio device / application name.
func (e *Engine) DeviceName() string {
	return e.deviceName
}

// Start launches the embedded Spotify Connect receiver and PulseAudio stream.
func (e *Engine) Start(parentCtx context.Context, username, accessToken string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return errors.New("player engine is already running")
	}

	if username == "" || accessToken == "" {
		return errors.New("username and access token are required to start audio engine")
	}

	// Set PulseAudio properties so pavucontrol-qt and PipeWire show "rukia"
	_ = os.Setenv("PULSE_PROP_application.name", e.deviceName)
	_ = os.Setenv("PULSE_PROP_application.process.binary", "rukia")
	_ = os.Setenv("PULSE_PROP_media.role", "music")
	_ = os.Setenv("PULSE_PROP_media.name", DefaultMediaName)

	cacheDir := defaultCacheDir()

	dCfg := &daemon.Config{
		DeviceName:      e.deviceName,
		DeviceType:      "computer",
		AudioBackend:    "pulseaudio",
		InitialVolume:   uint32(e.volume * 64 / 100),
		VolumeSteps:     64, // Prevents division by zero in daemon/player.go
		Bitrate:         160,
		ZeroconfEnabled: false,
		Credentials: daemon.CredentialsConfig{
			Type: "spotify_token",
			SpotifyToken: daemon.SpotifyTokenCredentials{
				Username:    username,
				AccessToken: accessToken,
			},
		},
	}

	store := NewFileStateStore(cacheDir)
	app, err := daemon.New(&daemon.Options{
		Logger:     &librespot.NullLogger{},
		Config:     dCfg,
		StateStore: store,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize audio daemon: %w", err)
	}

	ctx, cancel := context.WithCancel(parentCtx)
	e.cancel = cancel
	e.app = app
	e.doneCh = make(chan struct{})
	e.errCh = make(chan error, 1)
	e.running = true
	doneCh := e.doneCh
	errCh := e.errCh

	go func() {
		defer close(doneCh)
		err := app.Run(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			select {
			case errCh <- err:
			default:
			}
		}
	}()

	return nil
}

// SetVolume updates the engine initial volume.
func (e *Engine) SetVolume(percent int) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	e.volume = percent
}

// Volume returns the current volume percentage (0-100).
func (e *Engine) Volume() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.volume
}

// Errors returns a channel to monitor background engine failures.
func (e *Engine) Errors() <-chan error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.errCh
}

// Done closes when the current daemon run exits.
func (e *Engine) Done() <-chan struct{} {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.doneCh
}

// Running reports whether the daemon was started and has not been closed.
func (e *Engine) Running() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

// Close gracefully stops the player daemon and releases audio resources.
func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return nil
	}

	e.running = false
	if e.cancel != nil {
		e.cancel()
	}

	if e.app != nil {
		done := make(chan struct{})
		go func() {
			_ = e.app.Close()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		e.app = nil
	}

	return nil
}

// defaultCacheDir resolves the directory for librespot daemon state and cache.
// It prioritizes XDG_CACHE_HOME, then os.UserCacheDir, then os.UserHomeDir,
// and gracefully falls back to os.TempDir if discovery fails.
func defaultCacheDir() string {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "rukia", "librespot")
	}
	if cache, err := os.UserCacheDir(); err == nil && cache != "" {
		return filepath.Join(cache, "rukia", "librespot")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".cache", "rukia", "librespot")
	}
	return filepath.Join(os.TempDir(), "rukia", "librespot")
}
