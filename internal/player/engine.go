package player

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/jfreymuth/pulse"
	"github.com/jfreymuth/pulse/proto"
)

const (
	DefaultDeviceName = "rukia"
	DefaultMediaName  = "rukia Spotify Player"
)

// AppState placeholder for compatibility.
type AppState struct {
	DeviceId string
}

// MemoryStateStore satisfies state persistence for backward compatibility.
type MemoryStateStore struct {
	mu    sync.Mutex
	state *AppState
}

// Load retrieves stored application state.
func (m *MemoryStateStore) Load() (*AppState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		m.state = &AppState{}
	}
	return m.state, nil
}

// Save stores application state.
func (m *MemoryStateStore) Save(s *AppState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = s
	return nil
}

// Engine manages the PulseAudio playback stream and system audio integration for rukia.
type Engine struct {
	deviceName string
	client     *pulse.Client
	stream     *pulse.PlaybackStream
	volChan    chan proto.ChannelVolumes
	volEvents  chan int
	errCh      chan error
	mu         sync.Mutex
	running    bool
	isPaused   bool
	volume     int // 0 to 100
}

// NewEngine creates a new player engine instance.
func NewEngine(deviceName string) *Engine {
	if deviceName == "" {
		deviceName = DefaultDeviceName
	}
	return &Engine{
		deviceName: deviceName,
		volChan:    make(chan proto.ChannelVolumes, 8),
		volEvents:  make(chan int, 8),
		errCh:      make(chan error, 1),
		volume:     100,
	}
}

// DeviceName returns the advertised audio device / application name.
func (e *Engine) DeviceName() string {
	return e.deviceName
}

// Start launches the PulseAudio playback stream so rukia appears in pavucontrol.
func (e *Engine) Start(ctx context.Context, username, accessToken string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return errors.New("player engine is already running")
	}

	if username == "" || accessToken == "" {
		return errors.New("username and access token are required to start audio engine")
	}

	client, err := pulse.NewClient(
		pulse.ClientApplicationName(e.deviceName),
	)
	if err != nil {
		return fmt.Errorf("failed to connect to PulseAudio/PipeWire: %w", err)
	}

	stream, err := client.NewPlayback(
		pulse.Float32Reader(func(out []float32) (int, error) {
			// Zero out buffer so stream remains active and responsive in mixer
			for i := range out {
				out[i] = 0
			}
			return len(out), nil
		}),
		pulse.PlaybackSampleRate(44100),
		pulse.PlaybackStereo,
		pulse.PlaybackMediaName(DefaultMediaName),
		pulse.PlaybackVolumeChanges(e.volChan),
		pulse.PlaybackRawOption(func(req *proto.CreatePlaybackStream) {
			if req.Properties == nil {
				req.Properties = make(proto.PropList)
			}
			req.Properties["application.name"] = proto.PropListString(e.deviceName)
			req.Properties["application.process.binary"] = proto.PropListString("rukia")
			req.Properties["media.role"] = proto.PropListString("music")
		}),
	)
	if err != nil {
		client.Close()
		return fmt.Errorf("failed to create PulseAudio playback stream: %w", err)
	}

	e.client = client
	e.stream = stream
	e.running = true

	// Set initial volume
	rawVol := proto.Volume(float64(e.volume) / 100.0 * 65536.0)
	_ = stream.SetVolume(proto.ChannelVolumes{rawVol, rawVol})

	stream.Start()

	// Monitor volume changes from pavucontrol / system mixer
	go func() {
		for v := range e.volChan {
			if len(v) > 0 {
				pct := int(float64(v[0]) / 65536.0 * 100.0)
				if pct < 0 {
					pct = 0
				}
				if pct > 100 {
					pct = 100
				}

				e.mu.Lock()
				e.volume = pct
				e.mu.Unlock()

				select {
				case e.volEvents <- pct:
				default:
				}
			}
		}
	}()

	return nil
}

// Pause pauses the PulseAudio stream.
func (e *Engine) Pause() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running && e.stream != nil && !e.isPaused {
		e.stream.Pause()
		e.isPaused = true
	}
}

// Resume unpauses the PulseAudio stream.
func (e *Engine) Resume() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running && e.stream != nil && e.isPaused {
		e.stream.Resume()
		e.isPaused = false
	}
}

// SetVolume updates the stream volume in PulseAudio and pavucontrol.
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

	if e.running && e.stream != nil {
		rawVol := proto.Volume(float64(percent) / 100.0 * 65536.0)
		_ = e.stream.SetVolume(proto.ChannelVolumes{rawVol, rawVol})
	}
}

// Volume returns the current volume percentage (0-100).
func (e *Engine) Volume() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.volume
}

// VolumeEvents provides a channel receiving volume adjustments from pavucontrol.
func (e *Engine) VolumeEvents() <-chan int {
	return e.volEvents
}

// Errors returns a channel to monitor background engine failures.
func (e *Engine) Errors() <-chan error {
	return e.errCh
}

// Close gracefully stops the player stream and releases PulseAudio resources.
func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return nil
	}

	e.running = false
	if e.stream != nil {
		e.stream.Close()
		e.stream = nil
	}
	if e.client != nil {
		e.client.Close()
		e.client = nil
	}

	return nil
}
