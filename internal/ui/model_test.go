package ui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/Chavao/rukia-player/internal/spotify"
)

func TestModelUpdateNavigationAndModal(t *testing.T) {
	tracks := []spotify.Track{
		{ID: "t1", Name: "Track 1", DurationMs: 120000},
		{ID: "t2", Name: "Track 2", DurationMs: 180000},
	}
	playlist := &spotify.Playlist{
		Name:   "My Playlist",
		Tracks: tracks,
	}

	model := NewModel(nil, nil, nil, playlist, "dev-1")

	if model.cursor != 0 {
		t.Errorf("expected initial cursor 0, got %d", model.cursor)
	}

	// Down arrow key
	downMsg := tea.KeyMsg{Type: tea.KeyDown}
	m, _ := model.Update(downMsg)
	model = m.(*Model)

	if model.cursor != 1 {
		t.Errorf("expected cursor 1 after Down, got %d", model.cursor)
	}

	// Up arrow key
	upMsg := tea.KeyMsg{Type: tea.KeyUp}
	m, _ = model.Update(upMsg)
	model = m.(*Model)

	if model.cursor != 0 {
		t.Errorf("expected cursor 0 after Up, got %d", model.cursor)
	}

	// Exit key ('q')
	qMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	m, _ = model.Update(qMsg)
	model = m.(*Model)

	if !model.showExitModal {
		t.Error("expected showExitModal to be true after pressing 'q'")
	}

	// Left/Right toggles modal button
	rightMsg := tea.KeyMsg{Type: tea.KeyRight}
	m, _ = model.Update(rightMsg)
	model = m.(*Model)

	if model.exitDialog.Selected != ExitOptionNo {
		t.Errorf("expected selection No after Right key in modal, got %v", model.exitDialog.Selected)
	}

	// Cancel (Esc) closes modal
	escMsg := tea.KeyMsg{Type: tea.KeyEsc}
	m, _ = model.Update(escMsg)
	model = m.(*Model)

	if model.showExitModal {
		t.Error("expected showExitModal to be false after pressing Esc")
	}
}

func TestModelTick(t *testing.T) {
	model := NewModel(nil, nil, nil, nil, "dev-1")
	model.progressMs = 5000
	model.isPlaying = true

	m, _ := model.Update(tickMsg(time.Now()))
	updated := m.(*Model)

	if updated.progressMs != 6000 {
		t.Errorf("expected progressMs 6000, got %d", updated.progressMs)
	}
}
