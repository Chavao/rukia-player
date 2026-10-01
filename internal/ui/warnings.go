package ui

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"time"
)

type warningMsg struct{ err error }

// SetWarningChannel connects run cancellation and queued OAuth persistence warnings.
// Configure it before starting Bubble Tea.
func (m *Model) SetWarningChannel(ctx context.Context, warnings <-chan error) {
	m.ctx, m.warnings = ctx, warnings
}

func (m *Model) waitForWarningCmd() tea.Cmd {
	warnings, ctx := m.warnings, m.ctx
	if warnings == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return nil
		case err, ok := <-warnings:
			if !ok {
				return nil
			}
			return warningMsg{err: err}
		}
	}
}

func (m *Model) initialErrorTimer() tea.Cmd {
	if m.err == nil || m.initialErrorGeneration == 0 {
		return nil
	}
	return clearErrorCmd(3*time.Second, m.initialErrorGeneration)
}

func (m *Model) clearInitialPlaybackError() {
	if m.initialErrorGeneration != 0 && m.errorGeneration == m.initialErrorGeneration {
		m.err = nil
		m.initialErrorGeneration = 0
	}
}
