package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/Chavao/rukia-player/internal/player"
	"github.com/Chavao/rukia-player/internal/spotify"
)

type tickMsg time.Time
type playbackStateMsg *spotify.PlaybackState
type playerErrorMsg error
type errMsg error
type playbackChangedMsg struct {
	playing bool
	err     error
}
type actionResultMsg struct {
	action string
	err    error
}
type clearErrorMsg struct{}

func clearErrorCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		return clearErrorMsg{}
	})
}

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
		m.waitForPlayerErrorCmd(),
	)
}

func (m *Model) waitForPlayerErrorCmd() tea.Cmd {
	if m.playerEngine == nil {
		return nil
	}
	errCh := m.playerEngine.Errors()
	return func() tea.Msg {
		if errCh == nil {
			return nil
		}
		err, ok := <-errCh
		if !ok || err == nil {
			return nil
		}
		return playerErrorMsg(err)
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

	case playerErrorMsg:
		m.err = fmt.Errorf("audio player error: %w", error(msg))
		cmds = append(cmds, m.waitForPlayerErrorCmd(), clearErrorCmd(5*time.Second))

	case playbackChangedMsg:
		if msg.err == nil {
			m.isPlaying = msg.playing
		} else {
			m.err = fmt.Errorf("failed to toggle playback: %w", msg.err)
			cmds = append(cmds, clearErrorCmd(3*time.Second))
		}

	case actionResultMsg:
		if msg.err != nil {
			m.err = fmt.Errorf("failed to %s: %w", msg.action, msg.err)
			cmds = append(cmds, clearErrorCmd(3*time.Second))
		}

	case errMsg:
		m.err = msg
		cmds = append(cmds, clearErrorCmd(3*time.Second))

	case clearErrorMsg:
		m.err = nil

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
				cmds = append(cmds, m.togglePlayPauseCmd(!m.isPlaying))
			} else {
				// Play selected track
				m.playingIdx = m.cursor
				m.progressMs = 0
				m.isPlaying = true
				cmds = append(cmds, m.playTrackIndexCmd(m.cursor))
			}

		case key.Matches(msg, m.keys.Space):
			cmds = append(cmds, m.togglePlayPauseCmd(!m.isPlaying))

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

func (m *Model) togglePlayPauseCmd(shouldPlay bool) tea.Cmd {
	client := m.spotifyClient
	deviceID := m.deviceID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		var err error
		if shouldPlay {
			if client != nil {
				err = client.Resume(ctx, deviceID)
			}
		} else {
			if client != nil {
				err = client.Pause(ctx, deviceID)
			}
		}
		return playbackChangedMsg{playing: shouldPlay, err: err}
	}
}

func (m *Model) playTrackIndexCmd(idx int) tea.Cmd {
	client := m.spotifyClient
	deviceID := m.deviceID
	uri := ""
	if m.playlist != nil {
		uri = m.playlist.URI
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var err error
		if client != nil && uri != "" {
			err = client.PlayPlaylist(ctx, deviceID, uri, idx)
		}
		return actionResultMsg{action: "play track", err: err}
	}
}

func (m *Model) setVolumeCmd(vol int) tea.Cmd {
	client := m.spotifyClient
	deviceID := m.deviceID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var err error
		if client != nil {
			err = client.SetVolume(ctx, deviceID, vol)
		}
		return actionResultMsg{action: "change volume", err: err}
	}
}

func (m *Model) setShuffleCmd(shuf bool) tea.Cmd {
	client := m.spotifyClient
	deviceID := m.deviceID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var err error
		if client != nil {
			err = client.SetShuffle(ctx, deviceID, shuf)
		}
		return actionResultMsg{action: "toggle shuffle", err: err}
	}
}

func (m *Model) setRepeatCmd(mode string) tea.Cmd {
	client := m.spotifyClient
	deviceID := m.deviceID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var err error
		if client != nil {
			err = client.SetRepeat(ctx, deviceID, mode)
		}
		return actionResultMsg{action: "toggle repeat", err: err}
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
	errStr := ""
	if m.err != nil {
		errStr = m.err.Error()
	}
	bottom := RenderBottomBar(curTrack, m.progressMs, m.volume, m.isPlaying, m.repeatMode, m.width, errStr)

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
