package ui

import (
	"context"
	"errors"
	"github.com/Chavao/rukia-player/internal/spotify"
	tea "github.com/charmbracelet/bubbletea"
	"net"
	"time"
)

func (m *Model) togglePlayPauseCmd(shouldPlay bool) tea.Cmd {
	client := m.spotifyClient
	deviceID := m.deviceID
	version := m.requestedVersion
	parent := m.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, 3*time.Second)
		defer cancel()

		err := retryPlaybackControl(ctx, func() error {
			if client == nil {
				return nil
			}
			if shouldPlay {
				return client.Resume(ctx, deviceID)
			}
			return client.Pause(ctx, deviceID)
		}, func() bool { return version < m.playbackVersion.Load() }, waitPlaybackBackoff)
		return playbackChangedMsg{version: version, playing: shouldPlay, err: err}
	}
}

func (m *Model) playTrackIndexCmd(idx int) tea.Cmd {
	client := m.spotifyClient
	deviceID := m.deviceID
	version, parent := m.requestedVersion, m.ctx
	uri := ""
	offset := 0
	if m.playlist != nil {
		uri = m.playlist.URI
		offset = m.playlist.Tracks[idx].PlaylistPosition
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, 5*time.Second)
		defer cancel()

		var err error
		if client != nil && uri != "" {
			err = client.PlayPlaylist(ctx, deviceID, uri, offset)
		}
		return actionResultMsg{version: version, action: "play track", err: err}
	}
}

func (m *Model) skipNextCmd() tea.Cmd {
	client := m.spotifyClient
	deviceID := m.deviceID
	parent := m.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, 5*time.Second)
		defer cancel()

		var err error
		if client != nil {
			err = client.Next(ctx, deviceID)
		}
		return actionResultMsg{action: "skip next", err: err}
	}
}

func (m *Model) skipPreviousCmd() tea.Cmd {
	client := m.spotifyClient
	deviceID := m.deviceID
	parent := m.ctx
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, 5*time.Second)
		defer cancel()

		var err error
		if client != nil {
			err = client.Previous(ctx, deviceID)
		}
		return actionResultMsg{action: "skip previous", err: err}
	}
}

func (m *Model) setVolumeCmd(vol int) tea.Cmd {
	client := m.spotifyClient
	deviceID := m.deviceID
	parent := m.ctx
	generation := m.volumeGeneration.Load()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, 2*time.Second)
		defer cancel()
		var err error
		if client != nil {
			err = client.SetVolume(ctx, deviceID, vol)
		}
		return actionResultMsg{generation: generation, action: "change volume", err: err}
	}
}

func (m *Model) setShuffleCmd(shuf bool) tea.Cmd {
	client := m.spotifyClient
	deviceID := m.deviceID
	parent := m.ctx
	version := m.requestedShuffleVersion
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, 2*time.Second)
		defer cancel()
		var err error
		if client != nil {
			err = client.SetShuffle(ctx, deviceID, shuf)
		}
		return actionResultMsg{version: version, action: "toggle shuffle", err: err}
	}
}

func (m *Model) setRepeatCmd(mode string) tea.Cmd {
	client := m.spotifyClient
	deviceID := m.deviceID
	parent := m.ctx
	version := m.requestedRepeatVersion
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, 2*time.Second)
		defer cancel()
		var err error
		if client != nil {
			err = client.SetRepeat(ctx, deviceID, mode)
		}
		return actionResultMsg{version: version, action: "toggle repeat", err: err}
	}
}

// shouldRetryPlaybackControl permits only selected service failures and transient
// network failures. Rate limits and all unknown errors remain caller-visible.
func shouldRetryPlaybackControl(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var apiErr *spotify.APIError
	if errors.As(err, &apiErr) {
		if apiErr.RetryAfter > 0 {
			return false
		}
		switch apiErr.StatusCode {
		case 500, 502, 503, 504:
			return true
		}
		return false
	}
	var networkErr net.Error
	return errors.As(err, &networkErr) && (networkErr.Timeout() || networkErr.Temporary())
}
