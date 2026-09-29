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
	return tea.Tick(300*time.Millisecond, func(time.Time) tea.Msg { return volumePersistMsg{generation: generation} })
}

func (m *Model) persistVolumeCmd(exiting bool) tea.Cmd {
	settings, volume := m.volumeSettings, m.volume
	generation := m.volumeGeneration.Load()
	return func() tea.Msg {
		m.volumeWriteMu.Lock()
		defer m.volumeWriteMu.Unlock()
		if generation != m.volumeGeneration.Load() {
			return volumePersistedMsg{}
		}
		return volumePersistedMsg{err: settings.SetVolume(volume), exiting: exiting}
	}
}
