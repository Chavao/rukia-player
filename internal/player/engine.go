package player

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	librespot "github.com/devgianlu/go-librespot"
	"github.com/devgianlu/go-librespot/daemon"
)

const (
	DefaultDeviceName = "rukia"
	DefaultMediaName  = "rukia Spotify Player"
)

// Engine manages the embedded go-librespot player daemon and PulseAudio output.
type Engine struct {
	deviceName string
	deviceID   string
	app        *daemon.App
	cancel     context.CancelFunc
	errCh      chan error
	doneCh     chan struct{}
	mu         sync.Mutex
	running    bool
	volume     int // 0 to 100
}

// NewEngine creates a new player engine instance.
func NewEngine(deviceName string, options ...EngineOption) *Engine {
	if deviceName == "" {
		deviceName = DefaultDeviceName
	}
	engine := &Engine{
		deviceName: deviceName,
		volume:     100,
	}
	for _, option := range options {
		option(engine)
	}
	return engine
}

// DeviceName returns the advertised audio device / application name.
func (e *Engine) DeviceName() string {
	return e.deviceName
}

// Start launches the embedded Spotify Connect receiver and PulseAudio stream.
func (e *Engine) Start(parentCtx context.Context, username, accessToken string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running || e.app != nil {
		return errors.New("player engine is already running")
	}

	if err := validateDeviceID(e.deviceID); err != nil {
		return err
	}

	if username == "" || accessToken == "" {
		return errors.New("username and access token are required to start audio engine")
	}

	// Set PulseAudio properties so pavucontrol-qt and PipeWire show "rukia".
	for key, value := range map[string]string{
		"PULSE_PROP_application.name":           e.deviceName,
		"PULSE_PROP_application.process.binary": "rukia",
		"PULSE_PROP_media.role":                 "music",
		"PULSE_PROP_media.name":                 DefaultMediaName,
	} {
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("setting PulseAudio property %s: %w", key, err)
		}
	}

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

	store := NewFileStateStore(cacheDir, e.deviceID)
	app, err := daemon.New(&daemon.Options{
		Logger:     &librespot.NullLogger{},
		Config:     dCfg,
		StateStore: store,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize audio daemon: %w", err)
	}

	ctx, cancel := context.WithCancel(parentCtx)
	e.startRun(ctx, cancel, app, app.Run)

	return nil
}

// startRun is called with e.mu held after the daemon has been initialized.
func (e *Engine) startRun(ctx context.Context, cancel context.CancelFunc, app *daemon.App, run func(context.Context) error) {
	e.cancel = cancel
	e.app = app
	e.errCh = make(chan error, 1)
	e.doneCh = make(chan struct{})
	e.running = true
	doneCh := e.doneCh
	errCh := e.errCh

	go func() {
		err := run(ctx)
		// Run owns daemon resource cleanup on cancellation, including an early
		// Run failure. Do not race the daemon's own Close with a second call.
		cancel()
		if err != nil && !errors.Is(err, context.Canceled) {
			select {
			case errCh <- err:
			default:
			}
		}
		e.mu.Lock()
		e.running = false
		close(doneCh)
		e.mu.Unlock()
	}()
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

// Errors returns the current run's error channel, or nil before the first run.
func (e *Engine) Errors() <-chan error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.errCh
}

// Done closes when the current daemon run exits, or is nil before the first run.
func (e *Engine) Done() <-chan struct{} {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.doneCh
}

// Running reports whether the daemon's Run method is still active.
func (e *Engine) Running() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
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
