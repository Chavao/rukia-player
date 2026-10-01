package ui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Chavao/rukia-player/internal/player"
	"github.com/Chavao/rukia-player/internal/spotify"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	action string
	err    error
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
	spotifyClient  SpotifyController
	playerEngine   *player.Engine
	user           *spotify.UserProfile
	playlist       *spotify.Playlist
	deviceID       string
	volumeSettings VolumeSettings
	trackIndex     map[string]int

	cursor                    int
	playingIdx                int
	isPlaying                 bool
	confirmedPlaying          bool
	desiredPlaying            bool
	playbackPending           bool
	playbackReconcile         bool
	playbackVersion           atomic.Uint64
	requestedVersion          uint64
	failedVersion             uint64
	progressMs                int
	volume                    int
	desiredVolume             int
	volumeGeneration          atomic.Uint64
	persistedVolumeGeneration uint64
	volumeWriteMu             sync.Mutex
	exitPending               bool
	repeatMode                string
	shuffle                   bool

	showExitModal bool
	exitDialog    ExitDialog
	keys          KeyMap

	width           int
	height          int
	err             error
	errorGeneration uint64
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
		spotifyClient:    spotifyClient,
		playerEngine:     playerEngine,
		user:             user,
		playlist:         playlist,
		deviceID:         deviceID,
		volumeSettings:   volumeSettings,
		trackIndex:       idxMap,
		cursor:           0,
		playingIdx:       0,
		isPlaying:        true,
		confirmedPlaying: true,
		desiredPlaying:   true,
		progressMs:       0,
		volume:           vol,
		desiredVolume:    vol,
		repeatMode:       "off",
		exitDialog:       NewExitDialog(),
		keys:             DefaultKeyMap(),
		width:            80,
		height:           24,
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
	return func() tea.Msg {
		select {
		case err := <-errCh:
			if err != nil {
				return playerErrorMsg{err: err}
			}
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

func (m *Model) pollPlaybackCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		state, err := m.spotifyClient.GetPlaybackState(ctx)
		if err != nil {
			return errMsg{err: err}
		}
		return playbackStateMsg(state)
	}
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

	case playbackStateMsg:
		if msg != nil {
			if !m.playbackPending {
				previouslyConfirmed := m.confirmedPlaying
				m.confirmedPlaying = msg.IsPlaying
				if m.playbackReconcile {
					m.playbackReconcile = false
					if m.desiredPlaying != m.confirmedPlaying && m.playbackVersion.Load() > m.failedVersion {
						cmds = append(cmds, m.startPlaybackCommand())
					} else {
						m.isPlaying = m.confirmedPlaying
						m.desiredPlaying = m.confirmedPlaying
					}
				} else {
					if m.desiredPlaying == previouslyConfirmed {
						m.desiredPlaying = msg.IsPlaying
					}
					m.isPlaying = msg.IsPlaying
				}
			}
			m.progressMs = msg.ProgressMs
			m.repeatMode = msg.RepeatState
			m.shuffle = msg.ShuffleState
			if msg.Device != nil && m.volumeGeneration.Load() == m.persistedVolumeGeneration {
				m.volume = msg.Device.VolumePercent
				m.desiredVolume = msg.Device.VolumePercent
			}
			if msg.Item != nil && m.trackIndex != nil {
				if idx, ok := m.trackIndex[msg.Item.ID]; ok {
					m.playingIdx = idx
				}
			}
		}

	case playerErrorMsg:
		cmds = append(cmds, m.showError(fmt.Errorf("audio player error: %w", msg.err), 5*time.Second))

	case playbackChangedMsg:
		m.playbackPending = false
		if msg.err != nil {
			if msg.version == 0 || msg.version >= m.playbackVersion.Load() {
				cmds = append(cmds, m.showError(fmt.Errorf("failed to toggle playback: %w", msg.err), 3*time.Second))
				m.failedVersion = m.requestedVersion
				m.playbackReconcile = true
				cmds = append(cmds, m.pollPlaybackCmd())
			} else if m.desiredPlaying != m.confirmedPlaying {
				cmds = append(cmds, m.startPlaybackCommand())
			}
		} else {
			m.confirmedPlaying = msg.playing
			if m.desiredPlaying != m.confirmedPlaying {
				cmds = append(cmds, m.startPlaybackCommand())
			}
		}

	case actionResultMsg:
		if msg.action == "play track" {
			m.playbackPending = false
			if msg.err != nil {
				m.failedVersion = m.requestedVersion
				m.playbackReconcile = true
			} else {
				m.confirmedPlaying = true
				if m.desiredPlaying != m.confirmedPlaying {
					cmds = append(cmds, m.startPlaybackCommand())
				}
			}
		}
		if msg.err != nil {
			cmds = append(cmds, m.showError(fmt.Errorf("failed to %s: %w", msg.action, msg.err), 3*time.Second))
			cmds = append(cmds, m.pollPlaybackCmd())
		} else if msg.action == "play track" {
			cmds = append(cmds, m.pollPlaybackCmd())
		}

	case volumePersistMsg:
		if msg.generation == m.volumeGeneration.Load() {
			targetVol := msg.volume
			if targetVol == 0 && m.desiredVolume != 0 {
				targetVol = m.desiredVolume
			} else if targetVol == 0 && m.volume != 0 {
				targetVol = m.volume
			}
			cmds = append(cmds, m.persistVolumeCmd(targetVol, false))
		}

	case volumePersistedMsg:
		if msg.err != nil {
			m.exitPending = false
			cmds = append(cmds, m.showError(fmt.Errorf("failed to save volume: %w", msg.err), 3*time.Second))
		} else {
			if msg.generation > m.persistedVolumeGeneration {
				m.persistedVolumeGeneration = msg.generation
			}
			if msg.exiting {
				if m.playerEngine != nil {
					_ = m.playerEngine.Close()
				}
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
					if m.playerEngine != nil {
						_ = m.playerEngine.Close()
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
				// Play selected track
				m.playingIdx = m.cursor
				m.progressMs = 0
				m.isPlaying = true
				m.desiredPlaying = true
				m.playbackPending = true
				m.playbackVersion.Add(1)
				m.requestedVersion = m.playbackVersion.Load()
				cmds = append(cmds, m.playTrackIndexCmd(m.cursor))
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
				cmds = append(cmds, m.setVolumeCmd(m.volume), m.scheduleVolumePersist())
			}

		case key.Matches(msg, m.keys.VolumeDn):
			if m.volume > 0 {
				m.volume -= 5
				if m.volume < 0 {
					m.volume = 0
				}
				m.desiredVolume = m.volume
				cmds = append(cmds, m.setVolumeCmd(m.volume), m.scheduleVolumePersist())
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

func (m *Model) togglePlayback() tea.Cmd {
	m.desiredPlaying = !m.desiredPlaying
	m.isPlaying = m.desiredPlaying
	m.playbackVersion.Add(1)
	if m.playbackPending || m.playbackReconcile {
		return nil
	}
	if m.desiredPlaying == m.confirmedPlaying {
		return nil
	}
	return m.startPlaybackCommand()
}

func (m *Model) startPlaybackCommand() tea.Cmd {
	m.playbackPending = true
	m.requestedVersion = m.playbackVersion.Load()
	m.isPlaying = m.desiredPlaying
	return m.togglePlayPauseCmd(m.desiredPlaying)
}

func (m *Model) showError(err error, duration time.Duration) tea.Cmd {
	m.err = err
	m.errorGeneration++
	return clearErrorCmd(duration, m.errorGeneration)
}

// View assembles the complete TUI rendering.
func (m *Model) View() string {
	userName := ""
	if m.user != nil {
		userName = m.user.DisplayName
	}

	// 1. Header
	header := RenderHeader(userName, m.playlist, m.width)

	// Available height for track list: total height - header(1) - gap(1) - bottom bar(2)
	tableH := m.height - 4
	if tableH < 4 {
		tableH = 4
	}

	var tracks []spotify.Track
	if m.playlist != nil {
		tracks = m.playlist.Tracks
	}

	// 2. Track list
	trackList := RenderTrackTable(tracks, m.cursor, m.playingIdx, m.width, tableH)

	// 3. Current playing track for bottom bar
	var curTrack *spotify.Track
	if len(tracks) > 0 && m.playingIdx >= 0 && m.playingIdx < len(tracks) {
		curTrack = &tracks[m.playingIdx]
	}

	// 4. Bottom bar
	errStr := ""
	if m.err != nil {
		errStr = m.err.Error()
	}
	bottom := RenderBottomBar(curTrack, m.progressMs, m.volume, m.isPlaying, m.shuffle, m.repeatMode, m.width, errStr)

	baseView := lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"",
		trackList,
		strings.Repeat("\n", maxInt(0, tableH-len(strings.Split(trackList, "\n")))),
		bottom,
	)

	// If exit modal is requested, overlay it
	if m.showExitModal {
		return OverlayCenter(baseView, m.exitDialog.View(), m.width, m.height)
	}

	return baseView
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
