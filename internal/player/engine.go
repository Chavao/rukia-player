package player

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	librespot "github.com/devgianlu/go-librespot"
	"github.com/devgianlu/go-librespot/daemon"
)

const (
	DefaultDeviceName = "rukia"
	DefaultDeviceType = "computer"
)

// MemoryStateStore satisfies librespot.StateStore in-memory.
type MemoryStateStore struct {
	mu    sync.Mutex
	state *librespot.AppState
}

// Load retrieves stored application state.
func (m *MemoryStateStore) Load() (*librespot.AppState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		m.state = &librespot.AppState{}
	}
	return m.state, nil
}

// Save stores application state.
func (m *MemoryStateStore) Save(s *librespot.AppState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = s
	return nil
}

// Engine manages the embedded go-librespot Connect receiver.
type Engine struct {
	deviceName string
	app        *daemon.App
	cancel     context.CancelFunc
	errCh      chan error
	mu         sync.Mutex
	running    bool
}

// NewEngine creates a new player engine instance.
func NewEngine(deviceName string) *Engine {
	if deviceName == "" {
		deviceName = DefaultDeviceName
	}
	return &Engine{
		deviceName: deviceName,
		errCh:      make(chan error, 1),
	}
}

// DeviceName returns the advertised Spotify Connect device name.
func (e *Engine) DeviceName() string {
	return e.deviceName
}

// Start launches the embedded Spotify Connect receiver daemon in the background.
func (e *Engine) Start(parentCtx context.Context, username string, accessToken string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return errors.New("player engine is already running")
	}

	if username == "" || accessToken == "" {
		return errors.New("username and access token are required to start audio engine")
	}

	ctx, cancel := context.WithCancel(parentCtx)
	e.cancel = cancel

	cfg := &daemon.Config{
		DeviceName:      e.deviceName,
		DeviceType:      DefaultDeviceType,
		AudioBackend:    "pulseaudio",
		InitialVolume:   65535, // 100% volume
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

	opts := &daemon.Options{
		Logger:     &librespot.NullLogger{},
		Config:     cfg,
		StateStore: &MemoryStateStore{},
	}

	app, err := daemon.New(opts)
	if err != nil {
		cancel()
		return fmt.Errorf("failed to initialize librespot daemon: %w", err)
	}

	e.app = app
	e.running = true

	go func() {
		err := app.Run(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			select {
			case e.errCh <- err:
			default:
			}
		}
	}()

	return nil
}

// Errors returns a channel to monitor background engine failures.
func (e *Engine) Errors() <-chan error {
	return e.errCh
}

// Close gracefully stops the player daemon and releases all audio resources.
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
		// Give the daemon a moment to terminate cleanly
		done := make(chan struct{})
		go func() {
			_ = e.app.Close()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}

	return nil
}
