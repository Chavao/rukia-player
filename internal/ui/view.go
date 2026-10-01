package ui

import (
	"github.com/Chavao/rukia-player/internal/spotify"
	"github.com/charmbracelet/lipgloss"
	"strings"
)

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
