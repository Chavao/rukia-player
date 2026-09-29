package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/Chavao/rukia-player/internal/auth"
	"github.com/Chavao/rukia-player/internal/player"
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

func TestModelActionResultMsgAndErrorDisplay(t *testing.T) {
	tracks := []spotify.Track{
		{ID: "t1", Name: "Track 1", DurationMs: 120000},
	}
	playlist := &spotify.Playlist{
		Name:   "My Playlist",
		Tracks: tracks,
	}
	model := NewModel(nil, nil, nil, playlist, "dev-1")

	// Initially no error rendered in View
	view := model.View()
	if strings.Contains(view, "⚠") {
		t.Error("view should not show error indicator initially")
	}

	// Receive actionResultMsg with error
	m, cmd := model.Update(actionResultMsg{action: "change volume", err: assertErr("rate limited")})
	model = m.(*Model)
	if model.err == nil {
		t.Fatal("expected model.err to be set")
	}
	if cmd == nil {
		t.Fatal("expected clearErrorCmd to be scheduled")
	}

	// View should now display error in footer
	view = model.View()
	if !strings.Contains(view, "⚠") || !strings.Contains(view, "rate limited") {
		t.Errorf("expected view to contain error message, got: %s", view)
	}

	// clearErrorMsg clears the error
	m, _ = model.Update(clearErrorMsg{})
	model = m.(*Model)
	if model.err != nil {
		t.Errorf("expected model.err to be nil after clearErrorMsg, got: %v", model.err)
	}
	view = model.View()
	if strings.Contains(view, "⚠") {
		t.Error("view should not show error indicator after clearErrorMsg")
	}
}

func TestWaitForPlayerErrorCmd(t *testing.T) {
	eng := player.NewEngine("test-engine")
	model := NewModel(nil, eng, nil, nil, "dev-1")

	cmd := model.waitForPlayerErrorCmd()
	if cmd == nil {
		t.Fatal("expected non-nil tea.Cmd for waitForPlayerErrorCmd")
	}

	// Dispatch an error into the engine's error channel via background
	go func() {
		eng.Errors()
	}()

	// Simulate handling playerErrorMsg
	m, cmd := model.Update(playerErrorMsg(assertErr("pulseaudio died")))
	model = m.(*Model)

	if model.err == nil || !strings.Contains(model.err.Error(), "pulseaudio died") {
		t.Errorf("expected model.err to contain pulseaudio died, got: %v", model.err)
	}
	if cmd == nil {
		t.Fatal("expected cmd to be scheduled")
	}
}

func TestModelPollMsg(t *testing.T) {
	model := NewModel(nil, nil, nil, nil, "dev-1")
	m, cmd := model.Update(pollMsg(time.Now()))
	if m == nil {
		t.Fatal("expected non-nil model from Update")
	}
	if cmd == nil {
		t.Fatal("expected non-nil tea.Cmd for pollMsg")
	}
}

func TestModelVolumePersistence(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	cfg := auth.DefaultConfig()
	cfg.Volume = 0 // Mute
	model := NewModel(nil, nil, nil, nil, "dev-1", cfg)

	if model.volume != 0 {
		t.Errorf("expected model initial volume 0, got %d", model.volume)
	}

	// Press VolumeUp (+)
	volUpMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}}
	m, _ := model.Update(volUpMsg)
	model = m.(*Model)

	if model.volume != 5 {
		t.Errorf("expected volume 5 after VolumeUp, got %d", model.volume)
	}
	if cfg.Volume != 5 {
		t.Errorf("expected cfg.Volume to be updated to 5, got %d", cfg.Volume)
	}
}

func TestModelTrackIndexLookup(t *testing.T) {
	tracks := []spotify.Track{
		{ID: "track-a", Name: "Alpha", DurationMs: 120000},
		{ID: "track-b", Name: "Beta", DurationMs: 180000},
		{ID: "track-c", Name: "Gamma", DurationMs: 200000},
	}
	playlist := &spotify.Playlist{
		ID:     "pl-1",
		Tracks: tracks,
	}

	model := NewModel(nil, nil, nil, playlist, "dev-1")
	if len(model.trackIndex) != 3 {
		t.Fatalf("expected trackIndex length 3, got %d", len(model.trackIndex))
	}
	if model.trackIndex["track-b"] != 1 {
		t.Errorf("expected track-b at index 1, got %d", model.trackIndex["track-b"])
	}

	// Dispatch playbackStateMsg for track-c
	state := &spotify.PlaybackState{
		IsPlaying:  true,
		ProgressMs: 50000,
		Item:       &spotify.Track{ID: "track-c"},
	}
	m, _ := model.Update(playbackStateMsg(state))
	updated := m.(*Model)

	if updated.playingIdx != 2 {
		t.Errorf("expected playingIdx to be 2 for track-c, got %d", updated.playingIdx)
	}
}
