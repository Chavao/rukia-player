package ui

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Chavao/rukia-player/internal/player"
	"github.com/Chavao/rukia-player/internal/spotify"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type tickMsg time.Time
type pollMsg time.Time
type playbackStateMsg *spotify.PlaybackState
type playerErrorMsg struct{ err error }
type errMsg struct{ err error }
type playbackChangedMsg struct {
	version uint64
	playing bool
	err     error
}
type actionResultMsg struct {
	version    uint64
	action     string
	generation uint64
	err        error
}
type volumePersistMsg struct {
	generation uint64
	volume     int
}
type volumePersistedMsg struct {
	generation uint64
	err        error
	exiting    bool
}
type clearErrorMsg struct{ generation uint64 }

func clearErrorCmd(d time.Duration, generation uint64) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		return clearErrorMsg{generation: generation}
	})
}

// SpotifyController defines the playback and device control methods required by the UI.
type SpotifyController interface {
	GetPlaybackState(ctx context.Context) (*spotify.PlaybackState, error)
	Pause(ctx context.Context, deviceID string) error
	Resume(ctx context.Context, deviceID string) error
	PlayPlaylist(ctx context.Context, deviceID, playlistURI string, trackOffset int) error
	SetVolume(ctx context.Context, deviceID string, volumePercent int) error
	SetShuffle(ctx context.Context, deviceID string, state bool) error
	SetRepeat(ctx context.Context, deviceID string, state string) error
}

// VolumeSettings provides persisted volume without exposing configuration to the UI.
type VolumeSettings interface {
	CurrentVolume() int
	SetVolume(int) error
}

// Model is the main Bubble Tea application model.
type Model struct {
	ctx            context.Context
	warnings       <-chan error
	spotifyClient  SpotifyController
	playerEngine   *player.Engine
	user           *spotify.UserProfile
	playlist       *spotify.Playlist
	deviceID       string
	volumeSettings VolumeSettings
	trackIndex     map[string]int

	cursor                       int
	playingIdx                   int
	isPlaying                    bool
	confirmedPlaying             bool
	desiredPlaying               bool
	playbackPending              bool
	playbackReconcile            bool
	playbackAwaitingConfirmation bool
	playbackObservationCount     int
	playbackEpoch                uint64
	pollSequence                 uint64
	appliedPollSequence          uint64
	queuedTrack                  int
	requestedTrack               int
	confirmationTrack            int
	playbackVersion              atomic.Uint64
	requestedVersion             uint64
	failedVersion                uint64
	progressMs                   int
	volume                       int
	desiredVolume                int
	volumeGeneration             atomic.Uint64
	persistedVolumeGeneration    uint64
	resolvedVolumeGeneration     uint64
	volumeWriteMu                sync.Mutex
	volumePending                bool
	requestedVolume              int
	requestedVolumeGeneration    uint64
	volumeAwaitingConfirmation   bool
	volumeObservationCount       int
	volumeEpoch                  uint64
	exitPending                  bool
	repeatMode                   string
	shuffle                      bool

	showExitModal bool
	exitDialog    ExitDialog
	keys          KeyMap

	width                  int
	height                 int
	err                    error
	errorGeneration        uint64
	initialErrorGeneration uint64
}

// NewModel creates an initialized Bubble Tea model.
func NewModel(
	spotifyClient SpotifyController,
	playerEngine *player.Engine,
	user *spotify.UserProfile,
	playlist *spotify.Playlist,
	deviceID string,
	settings ...VolumeSettings,
) *Model {
	vol := 100
	var volumeSettings VolumeSettings
	if len(settings) > 0 && settings[0] != nil {
		volumeSettings = settings[0]
		vol = volumeSettings.CurrentVolume()
	}

	idxMap := make(map[string]int)
	if playlist != nil {
		for i, t := range playlist.Tracks {
			idxMap[t.ID] = i
		}
	}

	return &Model{
		ctx:               context.Background(),
		queuedTrack:       -1,
		requestedTrack:    -1,
		confirmationTrack: -1,
		spotifyClient:     spotifyClient,
		playerEngine:      playerEngine,
		user:              user,
		playlist:          playlist,
		deviceID:          deviceID,
		volumeSettings:    volumeSettings,
		trackIndex:        idxMap,
		cursor:            0,
		playingIdx:        0,
		isPlaying:         true,
		confirmedPlaying:  true,
		desiredPlaying:    true,
		progressMs:        0,
		volume:            vol,
		desiredVolume:     vol,
		repeatMode:        "off",
		exitDialog:        NewExitDialog(),
		keys:              DefaultKeyMap(),
		width:             80,
		height:            24,
	}
}

const defaultPollInterval = 4 * time.Second

// Init sets up the progress tick and initial device/playlist sync.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		pollCmd(defaultPollInterval),
		m.pollPlaybackCmd(),
		m.waitForPlayerErrorCmd(),
		m.waitForWarningCmd(),
		m.initialErrorTimer(),
	)
}

func (m *Model) waitForPlayerErrorCmd() tea.Cmd {
	if m.playerEngine == nil {
		return nil
	}
	errCh := m.playerEngine.Errors()
	doneCh := m.playerEngine.Done()
	if doneCh == nil {
		return nil
	}
	ctx := m.ctx
	return func() tea.Msg {
		select {
		case err := <-errCh:
			if err != nil {
				return playerErrorMsg{err: err}
			}
		case <-ctx.Done():
			return nil
		case <-doneCh:
			// The daemon may have sent its terminal error before closing Done.
			select {
			case err := <-errCh:
				if err != nil {
					return playerErrorMsg{err: err}
				}
			default:
			}
		}
		return nil
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func pollCmd(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return pollMsg(t)
	})
}

// Update processes incoming messages, keys, and timer ticks.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		if m.isPlaying {
			m.progressMs += 1000
			if m.playlist != nil && m.playingIdx >= 0 && m.playingIdx < len(m.playlist.Tracks) {
				currDur := m.playlist.Tracks[m.playingIdx].DurationMs
				if m.progressMs > currDur {
					m.progressMs = currDur
				}
			}
		}
		cmds = append(cmds, tickCmd())

	case pollMsg:
		cmds = append(cmds, pollCmd(defaultPollInterval), m.pollPlaybackCmd())

	case playbackPollResultMsg:
		if msg.epoch != m.playbackEpoch || msg.version != m.playbackVersion.Load() || msg.sequence <= m.appliedPollSequence {
			break
		}
		m.appliedPollSequence = msg.sequence
		if msg.err != nil {
			cmds = append(cmds, m.showError(msg.err, 3*time.Second), m.failedPlaybackPoll())
		} else {
			cmds = append(cmds, m.observePlaybackPoll(msg.state, msg.volumeGeneration, msg.volumeEpoch))
		}

	case playbackStateMsg:
		cmds = append(cmds, m.observePlayback((*spotify.PlaybackState)(msg)))

	case warningMsg:
		cmds = append(cmds, m.showError(msg.err, 5*time.Second), m.waitForWarningCmd())

	case playerErrorMsg:
		cmds = append(cmds, m.showError(fmt.Errorf("audio player error: %w", msg.err), 5*time.Second))

	case playbackChangedMsg:
		cmds = append(cmds, m.finishPlaybackCommand(msg.version, msg.playing, msg.err, "toggle playback"))

	case actionResultMsg:
		if msg.action == "play track" {
			cmds = append(cmds, m.finishPlaybackCommand(msg.version, true, msg.err, msg.action))
		} else if msg.action == "change volume" {
			cmds = append(cmds, m.finishVolumeCommand(msg))
		} else if msg.err != nil {
			cmds = append(cmds, m.showError(fmt.Errorf("failed to %s: %w", msg.action, msg.err), 3*time.Second))
			cmds = append(cmds, m.pollPlaybackCmd())
		}

	case volumePersistMsg:
		if msg.generation == m.volumeGeneration.Load() {
			cmds = append(cmds, m.persistVolumeCmd(msg.volume, false))
		}

	case volumePersistedMsg:
		if msg.generation != m.volumeGeneration.Load() {
			break
		}
		if msg.generation > m.resolvedVolumeGeneration {
			m.resolvedVolumeGeneration = msg.generation
		}
		if msg.err != nil {
			m.exitPending = false
			m.volumeAwaitingConfirmation = false
			cmds = append(cmds, m.showError(fmt.Errorf("failed to save volume: %w", msg.err), 3*time.Second))
		} else {
			if msg.generation > m.persistedVolumeGeneration {
				m.persistedVolumeGeneration = msg.generation
			}
			if msg.exiting {
				return m, tea.Quit
			}
		}

	case errMsg:
		cmds = append(cmds, m.showError(msg.err, 3*time.Second))

	case clearErrorMsg:
		if msg.generation == m.errorGeneration {
			m.err = nil
		}

	case tea.KeyMsg:
		// When exit modal is displayed, route keys exclusively to modal
		if m.showExitModal {
			if m.exitPending {
				return m, nil
			}
			switch {
			case key.Matches(msg, m.keys.Left), key.Matches(msg, m.keys.Right):
				m.exitDialog.Next()
			case key.Matches(msg, m.keys.Enter):
				if m.exitDialog.Selected == ExitOptionYes {
					if m.volumeSettings != nil {
						m.exitPending = true
						m.volumeGeneration.Add(1)
						return m, m.persistVolumeCmd(m.volume, true)
					}
					return m, tea.Quit
				}
				m.showExitModal = false
			case key.Matches(msg, m.keys.Cancel):
				m.showExitModal = false
			}
			return m, nil
		}

		// Main player keybindings
		switch {
		case key.Matches(msg, m.keys.Exit):
			m.showExitModal = true

		case key.Matches(msg, m.keys.Up):
			if m.cursor > 0 {
				m.cursor--
			}

		case key.Matches(msg, m.keys.Down):
			if m.playlist != nil && m.cursor < len(m.playlist.Tracks)-1 {
				m.cursor++
			}

		case key.Matches(msg, m.keys.Enter):
			if m.cursor == m.playingIdx {
				// Toggle Play/Pause
				cmds = append(cmds, m.togglePlayback())
			} else {
				cmds = append(cmds, m.selectTrack(m.cursor))
			}

		case key.Matches(msg, m.keys.Space):
			cmds = append(cmds, m.togglePlayback())

		case key.Matches(msg, m.keys.VolumeUp):
			if m.volume < 100 {
				m.volume += 5
				if m.volume > 100 {
					m.volume = 100
				}
				m.desiredVolume = m.volume
				cmds = append(cmds, m.changeVolume())
			}

		case key.Matches(msg, m.keys.VolumeDn):
			if m.volume > 0 {
				m.volume -= 5
				if m.volume < 0 {
					m.volume = 0
				}
				m.desiredVolume = m.volume
				cmds = append(cmds, m.changeVolume())
			}

		case key.Matches(msg, m.keys.Shuffle):
			m.shuffle = !m.shuffle
			cmds = append(cmds, m.setShuffleCmd(m.shuffle))

		case key.Matches(msg, m.keys.Repeat):
			if m.repeatMode == "off" {
				m.repeatMode = "context"
			} else {
				m.repeatMode = "off"
			}
			cmds = append(cmds, m.setRepeatCmd(m.repeatMode))
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) showError(err error, duration time.Duration) tea.Cmd {
	m.err = err
	m.errorGeneration++
	return clearErrorCmd(duration, m.errorGeneration)
}

// SetPlaybackInitialState overrides the initial playing and intent state.
func (m *Model) SetPlaybackInitialState(playing bool) {
	m.isPlaying = playing
	m.desiredPlaying = playing
	m.confirmedPlaying = playing
}

// SetInitialError sets an initial error to display on startup.
func (m *Model) SetInitialError(err error) {
	m.err = err
	m.errorGeneration++
	m.initialErrorGeneration = m.errorGeneration
}
