package ui

import (
	"fmt"
	"github.com/Chavao/rukia-player/internal/spotify"
	tea "github.com/charmbracelet/bubbletea"
	"time"
)

func (m *Model) scheduleVolumePersist() tea.Cmd {
	if m.volumeSettings == nil {
		return nil
	}
	generation := m.volumeGeneration.Load()
	vol := m.volume
	return tea.Tick(300*time.Millisecond, func(time.Time) tea.Msg {
		return volumePersistMsg{generation: generation, volume: vol}
	})
}

func (m *Model) persistVolumeCmd(volume int, exiting bool) tea.Cmd {
	settings := m.volumeSettings
	generation := m.volumeGeneration.Load()
	return func() tea.Msg {
		m.volumeWriteMu.Lock()
		defer m.volumeWriteMu.Unlock()
		if generation != m.volumeGeneration.Load() {
			return volumePersistedMsg{}
		}
		return volumePersistedMsg{generation: generation, err: settings.SetVolume(volume), exiting: exiting}
	}
}

// changeVolume advances both remote and persistence intent before dispatching I/O.
func (m *Model) changeVolume() tea.Cmd {
	m.volumeGeneration.Add(1)
	persist := m.scheduleVolumePersist()
	if m.volumeSettings == nil {
		m.resolvedVolumeGeneration = m.volumeGeneration.Load()
	}
	if m.volumePending {
		return persist
	}
	return tea.Batch(m.startVolumeCommand(), persist)
}

func (m *Model) startVolumeCommand() tea.Cmd {
	m.volumePending = true
	m.volumeEpoch++
	m.volumeAwaitingConfirmation = false
	m.requestedVolume = m.desiredVolume
	m.requestedVolumeGeneration = m.volumeGeneration.Load()
	return m.setVolumeCmd(m.requestedVolume)
}

func (m *Model) finishVolumeCommand(msg actionResultMsg) tea.Cmd {
	if msg.generation != m.requestedVolumeGeneration {
		return nil
	}
	m.volumePending = false
	m.volumeEpoch++
	if m.requestedVolumeGeneration < m.volumeGeneration.Load() {
		return m.startVolumeCommand()
	}
	if msg.err != nil {
		m.volumeAwaitingConfirmation = false
		return m.showError(fmt.Errorf("failed to change volume: %w", msg.err), 3*time.Second)
	}
	m.volumeAwaitingConfirmation = true
	m.volumeObservationCount = 0
	return nil
}

func (m *Model) observeVolume(device *spotify.Device, generation uint64) {
	if device == nil || generation != m.volumeGeneration.Load() || m.volumePending || m.volumeGeneration.Load() != m.resolvedVolumeGeneration {
		return
	}
	if m.volumeAwaitingConfirmation {
		if device.VolumePercent != m.desiredVolume {
			m.volumeObservationCount++
			if m.volumeObservationCount < 3 {
				return
			}
		}
		m.volumeAwaitingConfirmation = false
	}
	m.volume, m.desiredVolume = device.VolumePercent, device.VolumePercent
}
