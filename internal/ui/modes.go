package ui

import (
	"fmt"
	"time"

	"github.com/Chavao/rukia-player/internal/spotify"
	tea "github.com/charmbracelet/bubbletea"
)

const modeConfirmationObservations = 3

func (m *Model) toggleShuffle() tea.Cmd {
	m.shuffle = !m.shuffle
	m.shuffleVersion++
	m.shuffleEpoch++
	if m.shufflePending {
		return nil
	}
	return m.startShuffleCommand()
}

func (m *Model) startShuffleCommand() tea.Cmd {
	m.shufflePending = true
	m.shuffleAwaitingConfirmation = false
	m.shuffleObservationCount = 0
	m.requestedShuffle = m.shuffle
	m.requestedShuffleVersion = m.shuffleVersion
	m.shuffleEpoch++
	return m.setShuffleCmd(m.requestedShuffle)
}

func (m *Model) finishShuffleCommand(msg actionResultMsg) tea.Cmd {
	if !m.shufflePending || msg.version != m.requestedShuffleVersion {
		return nil
	}
	m.shufflePending = false
	m.shuffleEpoch++

	var errorCmd tea.Cmd
	if msg.err != nil && msg.version == m.shuffleVersion {
		errorCmd = m.showError(fmt.Errorf("failed to toggle shuffle: %w", msg.err), 3*time.Second)
	}
	if m.requestedShuffleVersion < m.shuffleVersion {
		next := m.startShuffleCommand()
		if errorCmd == nil {
			return next
		}
		return tea.Batch(errorCmd, next)
	}
	if msg.err != nil {
		m.shuffleAwaitingConfirmation = false
		return tea.Batch(errorCmd, m.pollPlaybackCmd())
	}
	m.shuffleAwaitingConfirmation = true
	m.shuffleObservationCount = 0
	return nil
}

func (m *Model) toggleRepeat() tea.Cmd {
	if m.repeatMode == "off" {
		m.repeatMode = "context"
	} else {
		m.repeatMode = "off"
	}
	m.repeatVersion++
	m.repeatEpoch++
	if m.repeatPending {
		return nil
	}
	return m.startRepeatCommand()
}

func (m *Model) startRepeatCommand() tea.Cmd {
	m.repeatPending = true
	m.repeatAwaitingConfirmation = false
	m.repeatObservationCount = 0
	m.requestedRepeat = m.repeatMode
	m.requestedRepeatVersion = m.repeatVersion
	m.repeatEpoch++
	return m.setRepeatCmd(m.requestedRepeat)
}

func (m *Model) finishRepeatCommand(msg actionResultMsg) tea.Cmd {
	if !m.repeatPending || msg.version != m.requestedRepeatVersion {
		return nil
	}
	m.repeatPending = false
	m.repeatEpoch++

	var errorCmd tea.Cmd
	if msg.err != nil && msg.version == m.repeatVersion {
		errorCmd = m.showError(fmt.Errorf("failed to toggle repeat: %w", msg.err), 3*time.Second)
	}
	if m.requestedRepeatVersion < m.repeatVersion {
		next := m.startRepeatCommand()
		if errorCmd == nil {
			return next
		}
		return tea.Batch(errorCmd, next)
	}
	if msg.err != nil {
		m.repeatAwaitingConfirmation = false
		return tea.Batch(errorCmd, m.pollPlaybackCmd())
	}
	m.repeatAwaitingConfirmation = true
	m.repeatObservationCount = 0
	return nil
}

func (m *Model) observeRemoteModes(state *spotify.PlaybackState, shuffleEpoch, repeatEpoch uint64) {
	remoteShuffle := false
	remoteRepeat := "off"
	if state != nil {
		remoteShuffle = state.ShuffleState
		remoteRepeat = state.RepeatState
	}

	if shuffleEpoch == m.shuffleEpoch && !m.shufflePending {
		if m.shuffleAwaitingConfirmation {
			if remoteShuffle != m.shuffle {
				m.shuffleObservationCount++
				if m.shuffleObservationCount < modeConfirmationObservations {
					goto repeat
				}
			}
			m.shuffleAwaitingConfirmation = false
		}
		m.shuffle = remoteShuffle
	}

repeat:
	if repeatEpoch == m.repeatEpoch && !m.repeatPending {
		if m.repeatAwaitingConfirmation {
			if remoteRepeat != m.repeatMode {
				m.repeatObservationCount++
				if m.repeatObservationCount < modeConfirmationObservations {
					return
				}
			}
			m.repeatAwaitingConfirmation = false
		}
		m.repeatMode = remoteRepeat
	}
}
