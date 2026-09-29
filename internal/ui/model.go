package ui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"rukia/internal/player"
	"rukia/internal/spotify"
)

type tickMsg time.Time
type playbackStateMsg *spotify.PlaybackState
type volumeEventMsg int
type errMsg error

// Model is the main Bubble Tea application model.
type Model struct {
	spotifyClient *spotify.Client
	playerEngine  *player.Engine
	user          *spotify.UserProfile
	playlist      *spotify.Playlist
	deviceID      string

	cursor     int
	playingIdx int
	isPlaying  bool
	progressMs int
	volume     int
	repeatMode string
	shuffle    bool

	showExitModal bool
	exitDialog    ExitDialog
	keys          KeyMap

	width  int
	height int
	err    error
}

// NewModel creates an initialized Bubble Tea model.
func NewModel(
	spotifyClient *spotify.Client,
	playerEngine *player.Engine,
	user *spotify.UserProfile,
	playlist *spotify.Playlist,
	deviceID string,
) *Model {
	return &Model{
		spotifyClient: spotifyClient,
		playerEngine:  playerEngine,
		user:          user,
		playlist:      playlist,
		deviceID:      deviceID,
		cursor:        0,
		playingIdx:    0,
		isPlaying:     true,
		progressMs:    0,
		volume:        100,
		repeatMode:    "off",
		exitDialog:    NewExitDialog(),
		keys:          DefaultKeyMap(),
		width:         80,
		height:        24,
	}
}

// Init sets up the progress tick and initial device/playlist sync.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		m.pollPlaybackCmd(),
		m.waitForVolumeEventCmd(),
	)
}

func (m *Model) waitForVolumeEventCmd() tea.Cmd {
	if m.playerEngine == nil {
		return nil
	}
	volEvents := m.playerEngine.VolumeEvents()
	return func() tea.Msg {
		vol, ok := <-volEvents
		if !ok {
			return nil
		}
		return volumeEventMsg(vol)
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m *Model) pollPlaybackCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		state, err := m.spotifyClient.GetPlaybackState(ctx)
		if err != nil {
			return errMsg(err)
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
		cmds = append(cmds, tickCmd(), m.pollPlaybackCmd())

	case playbackStateMsg:
		if msg != nil {
			m.isPlaying = msg.IsPlaying
			m.progressMs = msg.ProgressMs
			m.repeatMode = msg.RepeatState
			m.shuffle = msg.ShuffleState
			if msg.Device != nil {
				m.volume = msg.Device.VolumePercent
			}
			if msg.Item != nil && m.playlist != nil {
				for i, t := range m.playlist.Tracks {
					if t.ID == msg.Item.ID {
						m.playingIdx = i
						break
					}
				}
			}
		}

	case volumeEventMsg:
		m.volume = int(msg)
		cmds = append(cmds, m.waitForVolumeEventCmd(), m.syncSpotifyVolumeCmd(int(msg)))

	case errMsg:
		m.err = msg

	case tea.KeyMsg:
		// When exit modal is displayed, route keys exclusively to modal
		if m.showExitModal {
			switch {
			case key.Matches(msg, m.keys.Left), key.Matches(msg, m.keys.Right):
				m.exitDialog.Next()
			case key.Matches(msg, m.keys.Enter):
				if m.exitDialog.Selected == ExitOptionYes {
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
				cmds = append(cmds, m.togglePlayPauseCmd())
			} else {
				// Play selected track
				m.playingIdx = m.cursor
				m.progressMs = 0
				m.isPlaying = true
				cmds = append(cmds, m.playTrackIndexCmd(m.cursor))
			}

		case key.Matches(msg, m.keys.Space):
			cmds = append(cmds, m.togglePlayPauseCmd())

		case key.Matches(msg, m.keys.VolumeUp):
			if m.volume < 100 {
				m.volume += 5
				if m.volume > 100 {
					m.volume = 100
				}
				cmds = append(cmds, m.setVolumeCmd(m.volume))
			}

		case key.Matches(msg, m.keys.VolumeDn):
			if m.volume > 0 {
				m.volume -= 5
				if m.volume < 0 {
					m.volume = 0
				}
				cmds = append(cmds, m.setVolumeCmd(m.volume))
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

func (m *Model) togglePlayPauseCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		if m.isPlaying {
			m.isPlaying = false
			if m.playerEngine != nil {
				m.playerEngine.Pause()
			}
			_ = m.spotifyClient.Pause(ctx, m.deviceID)
		} else {
			m.isPlaying = true
			if m.playerEngine != nil {
				m.playerEngine.Resume()
			}
			_ = m.spotifyClient.Resume(ctx, m.deviceID)
		}
		return nil
	}
}

func (m *Model) playTrackIndexCmd(idx int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if m.playerEngine != nil {
			m.playerEngine.Resume()
		}
		if m.playlist != nil {
			_ = m.spotifyClient.PlayPlaylist(ctx, m.deviceID, m.playlist.URI, idx)
		}
		return nil
	}
}

func (m *Model) setVolumeCmd(vol int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if m.playerEngine != nil {
			m.playerEngine.SetVolume(vol)
		}
		_ = m.spotifyClient.SetVolume(ctx, m.deviceID, vol)
		return nil
	}
}

func (m *Model) syncSpotifyVolumeCmd(vol int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = m.spotifyClient.SetVolume(ctx, m.deviceID, vol)
		return nil
	}
}

func (m *Model) setShuffleCmd(shuf bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = m.spotifyClient.SetShuffle(ctx, m.deviceID, shuf)
		return nil
	}
}

func (m *Model) setRepeatCmd(mode string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = m.spotifyClient.SetRepeat(ctx, m.deviceID, mode)
		return nil
	}
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
	bottom := RenderBottomBar(curTrack, m.progressMs, m.volume, m.isPlaying, m.repeatMode, m.width)

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
