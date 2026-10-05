package ui

import (
	"github.com/Chavao/rukia-player/internal/mpris"
	"github.com/Chavao/rukia-player/internal/spotify"
	tea "github.com/charmbracelet/bubbletea"
)

// MPRISNotifier defines the state publishing interface used by the UI to notify MPRIS listeners.
type MPRISNotifier interface {
	UpdatePlaybackState(status string, track *spotify.Track, volume int, shuffle bool, repeat string, positionMs int)
	UpdateStatus(status string)
	UpdateVolume(volume int)
	UpdateTrack(track *spotify.Track)
	UpdateShuffle(shuffle bool)
	UpdateRepeat(repeat string)
}

// SetMPRIS sets the MPRIS server to receive state updates and initializes its state.
func (m *Model) SetMPRIS(notifier MPRISNotifier) {
	m.mpris = notifier
	if notifier != nil {
		status := "Stopped"
		if m.isPlaying {
			status = "Playing"
		} else if m.currentTrack != nil {
			status = "Paused"
		}
		notifier.UpdatePlaybackState(status, m.currentTrack, m.volume, m.shuffle, m.repeatMode, m.progressMs)
	}
}

// skipNext advances playback to the next track.
func (m *Model) skipNext() tea.Cmd {
	if !m.shuffle && m.playlist != nil && m.playingIdx >= 0 && m.playingIdx+1 < len(m.playlist.Tracks) {
		m.playingIdx++
		m.currentTrack = &m.playlist.Tracks[m.playingIdx]
		m.progressMs = 0
		if m.mpris != nil {
			m.mpris.UpdateTrack(m.currentTrack)
		}
	}
	return m.skipNextCmd()
}

// skipPrevious restarts or moves playback to the previous track.
func (m *Model) skipPrevious() tea.Cmd {
	if !m.shuffle && m.playlist != nil && m.playingIdx > 0 {
		m.playingIdx--
		m.currentTrack = &m.playlist.Tracks[m.playingIdx]
		m.progressMs = 0
		if m.mpris != nil {
			m.mpris.UpdateTrack(m.currentTrack)
		}
	}
	return m.skipPreviousCmd()
}

// handleMprisMessage processes incoming MPRIS events from D-Bus callers.
func (m *Model) handleMprisMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case mpris.TogglePlayPauseMsg:
		return m.togglePlayback(), true
	case mpris.PlayMsg:
		if !m.isPlaying {
			return m.togglePlayback(), true
		}
		return nil, true
	case mpris.PauseMsg:
		if m.isPlaying {
			return m.togglePlayback(), true
		}
		return nil, true
	case mpris.StopMsg:
		if m.isPlaying {
			return m.togglePlayback(), true
		}
		return nil, true
	case mpris.NextMsg:
		return m.skipNext(), true
	case mpris.PreviousMsg:
		return m.skipPrevious(), true
	case mpris.VolumeMsg:
		vol := msg.Percent
		if vol < 0 {
			vol = 0
		} else if vol > 100 {
			vol = 100
		}
		m.volume = vol
		m.desiredVolume = vol
		return m.changeVolume(), true
	case mpris.QuitMsg:
		return tea.Quit, true
	default:
		return nil, false
	}
}
