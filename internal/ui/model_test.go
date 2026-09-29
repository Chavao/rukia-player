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

func TestModelPlaybackChangedMsg(t *testing.T) {
	model := NewModel(nil, nil, nil, nil, "dev-1")
	model.isPlaying = false

	m, _ := model.Update(playbackChangedMsg{playing: true, err: nil})
	updated := m.(*Model)
	if !updated.isPlaying {
		t.Error("expected isPlaying to be true after successful playbackChangedMsg")
	}

	// When error occurs, isPlaying shouldn't change
	m, _ = model.Update(playbackChangedMsg{playing: false, err: assertErr("failed")})
	updated = m.(*Model)
	if !updated.isPlaying {
		t.Error("expected isPlaying to remain true after failed playbackChangedMsg")
	}
}

func TestTogglePlayPauseCmdNoModelMutation(t *testing.T) {
	model := NewModel(nil, nil, nil, nil, "dev-1")
	model.isPlaying = true

	cmd := model.togglePlayPauseCmd(false)
	if cmd == nil {
		t.Fatal("expected non-nil tea.Cmd")
	}

	// Model itself should not have changed synchronously
	if !model.isPlaying {
		t.Error("model.isPlaying should not be mutated when generating cmd")
	}

	msg := cmd()
	changedMsg, ok := msg.(playbackChangedMsg)
	if !ok {
		t.Fatalf("expected playbackChangedMsg, got %T", msg)
	}
	if changedMsg.playing != false {
		t.Errorf("expected playing=false, got %v", changedMsg.playing)
	}
}

type customErr string

func (e customErr) Error() string { return string(e) }
func assertErr(s string) error    { return customErr(s) }

