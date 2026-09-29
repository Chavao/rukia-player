package ui

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"time"
)

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
