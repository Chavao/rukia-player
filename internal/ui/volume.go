package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"time"
)

func (m *Model) scheduleVolumePersist() tea.Cmd {
	if m.volumeSettings == nil {
		return nil
	}
	generation := m.volumeGeneration.Add(1)
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
