package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/Chavao/rukia-player/internal/spotify"
	tea "github.com/charmbracelet/bubbletea"
)

type playbackPollResultMsg struct {
	state                                                                              *spotify.PlaybackState
	err                                                                                error
	version, epoch, sequence, volumeGeneration, volumeEpoch, shuffleEpoch, repeatEpoch uint64
}

func (m *Model) pollPlaybackCmd() tea.Cmd {
	if m.spotifyClient == nil {
		return nil
	}
	client, parent := m.spotifyClient, m.ctx
	m.pollSequence++
	result := playbackPollResultMsg{version: m.playbackVersion.Load(), epoch: m.playbackEpoch, sequence: m.pollSequence, volumeGeneration: m.volumeGeneration.Load(), volumeEpoch: m.volumeEpoch, shuffleEpoch: m.shuffleEpoch, repeatEpoch: m.repeatEpoch}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, 3*time.Second)
		defer cancel()
		result.state, result.err = client.GetPlaybackState(ctx)
		return result
	}
}

func (m *Model) togglePlayback() tea.Cmd {
	m.desiredPlaying = !m.desiredPlaying
	m.isPlaying = m.desiredPlaying
	m.playbackVersion.Add(1)
	if m.playbackPending || m.playbackReconcile {
		return nil
	}
	if m.desiredPlaying == m.confirmedPlaying && !m.playbackAwaitingConfirmation {
		return nil
	}
	return m.startPlaybackCommand()
}

func (m *Model) selectTrack(idx int) tea.Cmd {
	m.queuedTrack = idx
	m.playingIdx, m.progressMs = idx, 0
	m.desiredPlaying, m.isPlaying = true, true
	m.playbackVersion.Add(1)
	if m.playbackPending || m.playbackReconcile {
		return nil
	}
	return m.startPlaybackCommand()
}

func (m *Model) startPlaybackCommand() tea.Cmd {
	m.playbackPending = true
	m.playbackAwaitingConfirmation = false
	m.playbackObservationCount = 0
	m.requestedVersion = m.playbackVersion.Load()
	m.playbackEpoch++
	m.requestedTrack, m.queuedTrack = m.queuedTrack, -1
	m.isPlaying = m.desiredPlaying
	if m.requestedTrack >= 0 {
		m.playingIdx, m.progressMs = m.requestedTrack, 0
		return m.playTrackIndexCmd(m.requestedTrack)
	}
	return m.togglePlayPauseCmd(m.desiredPlaying)
}

func (m *Model) finishPlaybackCommand(version uint64, playing bool, err error, action string) tea.Cmd {
	if version != m.requestedVersion {
		return nil
	}
	m.playbackPending = false
	m.playbackEpoch++ // Polls started while the request was running cannot confirm its result.
	if err != nil {
		m.failedVersion = version
		m.playbackReconcile = true
		m.playbackObservationCount = 0
		m.playbackAwaitingConfirmation = false
		m.confirmationTrack = -1
		var errorCmd tea.Cmd
		if version == m.playbackVersion.Load() {
			errorCmd = m.showError(fmt.Errorf("failed to %s: %w", action, err), 3*time.Second)
		}
		return tea.Batch(errorCmd, m.pollPlaybackCmd())
	}
	m.confirmedPlaying = playing
	if m.requestedTrack >= 0 {
		m.confirmationTrack = m.requestedTrack
	}
	m.clearInitialPlaybackError()
	if m.queuedTrack >= 0 || m.desiredPlaying != playing {
		return m.startPlaybackCommand()
	}
	m.isPlaying = m.desiredPlaying
	m.playbackAwaitingConfirmation = true
	m.playbackObservationCount = 0
	if action == "play track" {
		return m.pollPlaybackCmd()
	}
	return nil
}

func (m *Model) observePlayback(state *spotify.PlaybackState) tea.Cmd {
	m.observeRemoteModes(state, m.shuffleEpoch, m.repeatEpoch)
	return m.observePlaybackPoll(state, m.volumeGeneration.Load(), m.volumeEpoch)
}

func (m *Model) observePlaybackPoll(state *spotify.PlaybackState, volumeGeneration, volumeEpoch uint64) tea.Cmd {
	if state == nil {
		// Spotify returns 204 when no playback/device is active. That is a fresh
		// stopped observation, and must resolve failed intents just like a state.
		state = &spotify.PlaybackState{RepeatState: "off"}
	}
	if volumeEpoch == m.volumeEpoch {
		m.observeVolume(state.Device, volumeGeneration)
	}
	if m.playbackPending {
		return nil
	}
	var next tea.Cmd
	if m.playbackReconcile {
		m.confirmedPlaying = state.IsPlaying
		m.playbackReconcile = false
		if m.queuedTrack >= 0 || (m.playbackVersion.Load() > m.failedVersion && m.desiredPlaying != m.confirmedPlaying) {
			next = m.startPlaybackCommand()
		} else {
			m.desiredPlaying, m.isPlaying = state.IsPlaying, state.IsPlaying
		}
	} else if m.playbackAwaitingConfirmation {
		trackMatches := m.confirmationTrack < 0 || (m.playlist != nil && state.Item != nil && state.Item.ID == m.playlist.Tracks[m.confirmationTrack].ID)
		if state.IsPlaying == m.desiredPlaying && trackMatches {
			m.playbackAwaitingConfirmation = false
			m.confirmationTrack = -1
		} else {
			m.playbackObservationCount++
			if m.playbackObservationCount < 3 {
				return nil
			}
			m.playbackAwaitingConfirmation = false
			m.confirmationTrack = -1
		}
		m.confirmedPlaying, m.desiredPlaying, m.isPlaying = state.IsPlaying, state.IsPlaying, state.IsPlaying
	} else {
		m.confirmedPlaying, m.desiredPlaying, m.isPlaying = state.IsPlaying, state.IsPlaying, state.IsPlaying
	}
	if m.playbackPending && m.requestedTrack >= 0 {
		return next
	}
	m.progressMs = state.ProgressMs
	if state.Item != nil {
		if idx, ok := m.trackIndex[state.Item.ID]; ok {
			m.playingIdx = idx
		}
	}
	if state.IsPlaying {
		m.clearInitialPlaybackError()
	}
	return next
}

// A failed command cannot hold the controls hostage when Spotify observations
// also fail. After three fresh poll errors, use the last confirmed state and
// allow any newer user intent to proceed. The normal poll remains the safety net.
func (m *Model) failedPlaybackPoll() tea.Cmd {
	if !m.playbackReconcile {
		return nil
	}
	m.playbackObservationCount++
	if m.playbackObservationCount < 3 {
		return nil
	}
	m.playbackReconcile = false
	if m.queuedTrack >= 0 || (m.playbackVersion.Load() > m.failedVersion && m.desiredPlaying != m.confirmedPlaying) {
		return m.startPlaybackCommand()
	}
	m.desiredPlaying, m.isPlaying = m.confirmedPlaying, m.confirmedPlaying
	return nil
}
