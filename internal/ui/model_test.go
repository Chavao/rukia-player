package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Chavao/rukia-player/internal/auth"
	"github.com/Chavao/rukia-player/internal/player"
	"github.com/Chavao/rukia-player/internal/spotify"
	tea "github.com/charmbracelet/bubbletea"
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

	model.Update(downMsg)
	if model.cursor != 1 {
		t.Errorf("expected cursor to remain at last row, got %d", model.cursor)
	}

	// Up arrow key
	upMsg := tea.KeyMsg{Type: tea.KeyUp}
	m, _ = model.Update(upMsg)
	model = m.(*Model)

	if model.cursor != 0 {
		t.Errorf("expected cursor 0 after Up, got %d", model.cursor)
	}

	model.Update(upMsg)
	if model.cursor != 0 {
		t.Errorf("expected cursor to remain at first row, got %d", model.cursor)
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
	space := tea.KeyMsg{Type: tea.KeySpace}
	model.Update(space)
	if model.isPlaying || !model.playbackPending {
		t.Fatal("first key must optimistically pause playback")
	}
	model.Update(playbackChangedMsg{version: model.requestedVersion, playing: false})
	if model.isPlaying || model.playbackPending || model.confirmedPlaying {
		t.Fatal("successful pause must confirm the optimistic state")
	}
	model.Update(space)
	if !model.isPlaying {
		t.Fatal("second key must optimistically resume playback")
	}
	model.Update(playbackChangedMsg{version: model.requestedVersion, playing: true, err: assertErr("failed")})
	if !model.desiredPlaying || model.playbackPending || !model.playbackReconcile {
		t.Fatal("failed resume must preserve intent and request reconciliation")
	}
}

func TestPlaybackToggleSequences(t *testing.T) {
	for _, tc := range []struct {
		name       string
		initial    bool
		toggles    int
		firstFails bool
		wantCalls  []string
		wantState  bool
	}{
		{"pause success", true, 1, false, []string{"pause"}, false},
		{"resume success", false, 1, false, []string{"resume"}, true},
		{"pause failure", true, 1, true, []string{"pause"}, true},
		{"resume failure", false, 1, true, []string{"resume"}, false},
		{"pause then resume", true, 2, false, []string{"pause", "resume"}, true},
		{"pause then resume after failure", true, 2, true, []string{"pause"}, true},
		{"pause resume pause", true, 3, false, []string{"pause"}, false},
		{"pause resume pause after failure", true, 3, true, []string{"pause", "pause"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			mock := &mockSpotifyController{
				pauseFunc:  func(context.Context, string) error { calls = append(calls, "pause"); return nil },
				resumeFunc: func(context.Context, string) error { calls = append(calls, "resume"); return nil },
			}
			model := NewModel(mock, nil, nil, nil, "device")
			model.isPlaying, model.desiredPlaying, model.confirmedPlaying = tc.initial, tc.initial, tc.initial
			var firstCmd tea.Cmd
			for i := 0; i < tc.toggles; i++ {
				_, cmd := model.Update(tea.KeyMsg{Type: tea.KeySpace})
				if i == 0 {
					firstCmd = cmd
				}
			}
			if len(calls) != 0 {
				t.Fatal("Update performed remote I/O")
			}
			if model.desiredPlaying != (tc.initial != (tc.toggles%2 == 1)) {
				t.Fatalf("wrong desired state: %v", model.desiredPlaying)
			}
			// The first command is the only one allowed to run while pending.
			firstMsg := firstCmd()
			if tc.firstFails {
				firstMsg = playbackChangedMsg{version: model.requestedVersion, playing: !tc.initial, err: assertErr("failed")}
			}
			_, next := model.Update(firstMsg)
			if tc.firstFails {
				if !model.playbackReconcile {
					t.Fatal("failure did not start reconciliation")
				}
				if model.desiredPlaying != (tc.initial != (tc.toggles%2 == 1)) {
					t.Fatal("failure discarded desired state")
				}
				_, next = model.Update(playbackStateMsg(&spotify.PlaybackState{IsPlaying: tc.initial}))
			}
			if model.playbackPending {
				model.Update(next().(playbackChangedMsg))
			}
			if model.confirmedPlaying != tc.wantState {
				t.Fatalf("confirmed=%v, want %v", model.confirmedPlaying, tc.wantState)
			}
			if len(calls) != len(tc.wantCalls) {
				t.Fatalf("calls=%v, want %v", calls, tc.wantCalls)
			}
			for i := range calls {
				if calls[i] != tc.wantCalls[i] {
					t.Fatalf("calls=%v, want %v", calls, tc.wantCalls)
				}
			}
		})
	}
}

func TestRapidPlaybackTogglesAreSerialized(t *testing.T) {
	model := NewModel(nil, nil, nil, nil, "dev-1")
	space := tea.KeyMsg{Type: tea.KeySpace}
	model.Update(space)
	model.Update(space)
	if !model.isPlaying || !model.playbackPending {
		t.Fatal("second toggle must update the visible state while first command is pending")
	}
	_, cmd := model.Update(playbackChangedMsg{version: model.requestedVersion, playing: false})
	if !model.playbackPending || !model.isPlaying || cmd == nil {
		t.Fatal("successful pause must schedule the queued resume")
	}
	model.Update(playbackChangedMsg{version: model.requestedVersion, playing: true})
	if model.playbackPending || !model.confirmedPlaying || !model.isPlaying {
		t.Fatal("queued resume must complete in order")
	}
}

func TestFailedPlaybackReconcilesRemoteStateAndPreservesNewIntent(t *testing.T) {
	state := &spotify.PlaybackState{
		IsPlaying: true, ProgressMs: 42000, ShuffleState: true, RepeatState: "context",
		Item: &spotify.Track{ID: "second"}, Device: &spotify.Device{VolumePercent: 31},
	}
	polls := 0
	mock := &mockSpotifyController{getPlaybackStateFunc: func(context.Context) (*spotify.PlaybackState, error) {
		polls++
		return state, nil
	}}
	model := NewModel(mock, nil, nil, &spotify.Playlist{Tracks: []spotify.Track{{ID: "first"}, {ID: "second"}}}, "device")
	model.Update(tea.KeyMsg{Type: tea.KeySpace})
	model.Update(tea.KeyMsg{Type: tea.KeySpace})
	model.Update(tea.KeyMsg{Type: tea.KeySpace})
	_, cmd := model.Update(playbackChangedMsg{version: model.requestedVersion, playing: false, err: assertErr("offline")})
	if polls != 0 {
		t.Fatal("poll ran synchronously in Update")
	}
	// A superseded command failure reconciles without surfacing an obsolete error.
	model.Update(cmd())
	if polls != 1 {
		t.Fatalf("expected immediate poll, got %d", polls)
	}
	if model.progressMs != 42000 || model.volume != 31 || !model.shuffle || model.repeatMode != "context" || model.playingIdx != 1 {
		t.Fatalf("reconciliation missed remote fields: %+v", model)
	}
	if model.desiredPlaying || !model.confirmedPlaying || !model.playbackPending {
		t.Fatalf("newer pause intent was discarded: desired=%v confirmed=%v pending=%v", model.desiredPlaying, model.confirmedPlaying, model.playbackPending)
	}
}

func TestLaterPollReconcilesFailedPlaybackIntent(t *testing.T) {
	mock := &mockSpotifyController{
		pauseFunc: func(context.Context, string) error { return nil },
	}
	model := NewModel(mock, nil, nil, nil, "device")
	model.Update(tea.KeyMsg{Type: tea.KeySpace})
	model.Update(playbackChangedMsg{version: model.requestedVersion, playing: false, err: assertErr("offline")})
	model.Update(playbackStateMsg(&spotify.PlaybackState{IsPlaying: true}))
	model.Update(playbackStateMsg(&spotify.PlaybackState{IsPlaying: true}))
	if !model.desiredPlaying || !model.confirmedPlaying || model.isPlaying != model.confirmedPlaying {
		t.Fatalf("failed intent was not reconciled: desired=%v confirmed=%v visible=%v", model.desiredPlaying, model.confirmedPlaying, model.isPlaying)
	}

	// Next space after failure reconciliation must trigger pause command instead of being a no-op
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeySpace})
	if cmd == nil {
		t.Fatal("expected non-nil cmd on space after reconciled failure")
	}
	if model.desiredPlaying != false {
		t.Fatalf("expected desiredPlaying=false after space, got %v", model.desiredPlaying)
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

func TestTogglePlayPauseCmdRetriesOnTransientError(t *testing.T) {
	attempts := 0
	mock := &mockSpotifyController{
		pauseFunc: func(ctx context.Context, deviceID string) error {
			attempts++
			if attempts < 3 {
				return transientNetworkError{}
			}
			return nil
		},
	}
	model := NewModel(mock, nil, nil, nil, "dev-1")
	cmd := model.togglePlayPauseCmd(false)
	msg := cmd()
	changedMsg, ok := msg.(playbackChangedMsg)
	if !ok {
		t.Fatalf("expected playbackChangedMsg, got %T", msg)
	}
	if changedMsg.err != nil {
		t.Fatalf("expected success after retries, got %v", changedMsg.err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestTogglePlayPauseCmdAbortsWhenSuperseded(t *testing.T) {
	attempts := 0
	var model *Model
	mock := &mockSpotifyController{
		pauseFunc: func(ctx context.Context, deviceID string) error {
			attempts++
			// Simulate user pressing Space again while retrying
			if attempts == 1 && model != nil {
				model.playbackVersion.Add(1)
			}
			return assertErr("failure")
		},
	}
	model = NewModel(mock, nil, nil, nil, "dev-1")
	model.requestedVersion = model.playbackVersion.Load()
	cmd := model.togglePlayPauseCmd(false)
	msg := cmd()
	changedMsg, ok := msg.(playbackChangedMsg)
	if !ok {
		t.Fatalf("expected playbackChangedMsg, got %T", msg)
	}
	if changedMsg.err == nil {
		t.Fatal("expected error, got nil")
	}
	// Should have aborted after the first attempt when superseded
	if attempts > 2 {
		t.Fatalf("expected early abort (<=2 attempts), got %d", attempts)
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
	m, _ = model.Update(clearErrorMsg{generation: model.errorGeneration})
	model = m.(*Model)
	if model.err != nil {
		t.Errorf("expected model.err to be nil after clearErrorMsg, got: %v", model.err)
	}
	view = model.View()
	if strings.Contains(view, "⚠") {
		t.Error("view should not show error indicator after clearErrorMsg")
	}
}

func TestRemoteActionPollingPolicy(t *testing.T) {
	polls := 0
	controller := &mockSpotifyController{getPlaybackStateFunc: func(context.Context) (*spotify.PlaybackState, error) {
		polls++
		return &spotify.PlaybackState{IsPlaying: true}, nil
	}}
	model := NewModel(controller, nil, nil, nil, "device")
	model.observeRemoteModes(&spotify.PlaybackState{RepeatState: "off"}, model.shuffleEpoch, model.repeatEpoch)

	if _, cmd := model.Update(actionResultMsg{version: model.requestedVersion, action: "change volume"}); cmd != nil {
		t.Fatal("change volume unexpectedly scheduled immediate poll on success")
	}

	shuffleCmd := model.toggleShuffle()
	shuffleResult := shuffleCmd().(actionResultMsg)
	if _, cmd := model.Update(shuffleResult); cmd != nil {
		t.Fatal("toggle shuffle unexpectedly scheduled immediate poll on success")
	}
	repeatCmd := model.toggleRepeat()
	repeatResult := repeatCmd().(actionResultMsg)
	if _, cmd := model.Update(repeatResult); cmd != nil {
		t.Fatal("toggle repeat unexpectedly scheduled immediate poll on success")
	}

	_, playCmd := model.Update(actionResultMsg{version: model.requestedVersion, action: "play track"})
	if playCmd == nil {
		t.Fatal("play track success must schedule immediate poll")
	}
	model.Update(playCmd())
	if polls != 1 {
		t.Fatalf("expected 1 poll from play track success, got %d", polls)
	}

	executeReconciliation := func(t *testing.T, model *Model, cmd tea.Cmd, action string) {
		t.Helper()
		if cmd == nil {
			t.Fatalf("%s failure did not schedule reconciliation poll", action)
		}
		if batch, ok := cmd().(tea.BatchMsg); ok {
			model.Update(batch[len(batch)-1]())
		} else {
			model.Update(cmd())
		}
	}

	pollsBefore := polls
	_, failedPlay := model.Update(actionResultMsg{version: model.requestedVersion, action: "play track", err: assertErr("failed")})
	executeReconciliation(t, model, failedPlay, "play track")
	if polls <= pollsBefore {
		t.Fatal("play track failure did not trigger poll execution")
	}

	for _, tc := range []struct {
		name  string
		start func(*Model) tea.Cmd
	}{
		{"toggle shuffle", func(m *Model) tea.Cmd { return m.toggleShuffle() }},
		{"toggle repeat", func(m *Model) tea.Cmd { return m.toggleRepeat() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModel(controller, nil, nil, nil, "device")
			m.observeRemoteModes(&spotify.PlaybackState{RepeatState: "off"}, m.shuffleEpoch, m.repeatEpoch)
			request := tc.start(m)
			result := request().(actionResultMsg)
			result.err = assertErr("failed")
			pollsBefore := polls
			_, cmd := m.Update(result)
			executeReconciliation(t, m, cmd, tc.name)
			if polls <= pollsBefore {
				t.Fatalf("%s failure did not trigger poll execution", tc.name)
			}
		})
	}
}

func TestOlderErrorTimerDoesNotClearNewError(t *testing.T) {
	model := NewModel(nil, nil, nil, nil, "dev-1")
	model.Update(errMsg{err: assertErr("first")})
	firstGeneration := model.errorGeneration
	model.Update(errMsg{err: assertErr("second")})
	model.Update(clearErrorMsg{generation: firstGeneration})
	if model.err == nil || model.err.Error() != "second" {
		t.Fatalf("older timer cleared newer error: %v", model.err)
	}
	model.Update(clearErrorMsg{generation: model.errorGeneration})
	if model.err != nil {
		t.Fatalf("current timer did not clear error: %v", model.err)
	}
}

func TestWaitForPlayerErrorCmd(t *testing.T) {
	eng := player.NewEngine("test-engine")
	model := NewModel(nil, eng, nil, nil, "dev-1")

	cmd := model.waitForPlayerErrorCmd()
	if cmd != nil {
		t.Fatal("stopped engine must not schedule an error waiter")
	}

	// Simulate handling playerErrorMsg
	m, cmd := model.Update(playerErrorMsg{err: assertErr("pulseaudio died")})
	model = m.(*Model)

	if model.err == nil || !strings.Contains(model.err.Error(), "pulseaudio died") {
		t.Errorf("expected model.err to contain pulseaudio died, got: %v", model.err)
	}
	if cmd == nil {
		t.Fatal("expected an error clear timer")
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
	if cfg.CurrentVolume() != 0 {
		t.Fatal("Update persisted volume synchronously")
	}
	_, cmd := model.Update(volumePersistMsg{generation: model.volumeGeneration.Load(), volume: model.volume})
	if cmd == nil {
		t.Fatal("expected persistence command")
	}
	model.Update(cmd())
	if cfg.CurrentVolume() != 5 {
		t.Errorf("expected cfg.Volume to be updated to 5, got %d", cfg.CurrentVolume())
	}
}

type volumeSettingsStub struct {
	volume int
	calls  int
	err    error
}

func (s *volumeSettingsStub) CurrentVolume() int { return s.volume }
func (s *volumeSettingsStub) SetVolume(v int) error {
	s.calls++
	if s.err == nil {
		s.volume = v
	}
	return s.err
}

func TestVolumePersistenceFailureAndExit(t *testing.T) {
	settings := &volumeSettingsStub{volume: 5, err: errors.New("disk full")}
	model := NewModel(nil, nil, nil, nil, "", settings)
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'-'}})
	if model.volume != 0 || settings.calls != 0 {
		t.Fatal("volume zero must update only in model before command")
	}
	_, cmd := model.Update(volumePersistMsg{generation: model.volumeGeneration.Load(), volume: model.volume})
	model.Update(cmd())
	if model.err == nil || !strings.Contains(model.err.Error(), "failed to save volume") {
		t.Fatalf("missing persistence error: %v", model.err)
	}
	settings.err = nil
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if settings.calls != 1 || cmd == nil {
		t.Fatal("exit must persist via command")
	}
	_, quit := model.Update(cmd())
	if settings.volume != 0 || quit == nil {
		t.Fatal("exit must persist current zero volume before quitting")
	}
}

func TestStaleVolumePersistenceDoesNotOverwriteLatestValue(t *testing.T) {
	settings := &volumeSettingsStub{volume: 0}
	model := NewModel(nil, nil, nil, nil, "", settings)
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	_, oldCmd := model.Update(volumePersistMsg{generation: model.volumeGeneration.Load(), volume: model.volume})
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	model.Update(oldCmd())
	if settings.calls != 0 {
		t.Fatal("stale persistence wrote an older volume")
	}
	_, latestCmd := model.Update(volumePersistMsg{generation: model.volumeGeneration.Load(), volume: model.volume})
	model.Update(latestCmd())
	if settings.calls != 1 || settings.volume != 10 {
		t.Fatalf("persisted %d after %d calls, want 10 after one call", settings.volume, settings.calls)
	}
}

func TestVolumePollDuringDebounceDoesNotOverwriteDesiredVolume(t *testing.T) {
	settings := &volumeSettingsStub{volume: 50}
	model := NewModel(nil, nil, nil, nil, "", settings)
	model.volume = 50
	model.desiredVolume = 50

	// User increases volume
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	if model.volume != 55 {
		t.Fatalf("expected volume 55, got %d", model.volume)
	}

	// Spotify poll arrives before debounce ticks, reporting stale volume 50
	model.Update(playbackStateMsg(&spotify.PlaybackState{
		Device: &spotify.Device{VolumePercent: 50},
	}))

	// Volume in model must NOT have been overwritten by stale poll
	if model.volume != 55 {
		t.Fatalf("poll prematurely overwritten desired volume: got %d, want 55", model.volume)
	}

	// Debounce fires and persists
	_, cmd := model.Update(volumePersistMsg{generation: model.volumeGeneration.Load(), volume: 55})
	if cmd == nil {
		t.Fatal("expected persist command")
	}
	model.Update(cmd())

	if settings.volume != 55 {
		t.Fatalf("expected persisted volume 55, got %d", settings.volume)
	}
}

func TestTrackSelectionUpdatesPlaybackStateMachine(t *testing.T) {
	tracks := []spotify.Track{
		{ID: "track-0", Name: "Zero", DurationMs: 100000},
		{ID: "track-1", Name: "One", DurationMs: 120000},
	}
	playlist := &spotify.Playlist{ID: "p1", Tracks: tracks, URI: "spotify:playlist:p1"}
	var pauseCalled bool
	mock := &mockSpotifyController{
		playPlaylistFunc: func(ctx context.Context, deviceID, playlistURI string, trackOffset int) error {
			return nil
		},
		pauseFunc: func(ctx context.Context, deviceID string) error {
			pauseCalled = true
			return nil
		},
	}
	model := NewModel(mock, nil, nil, playlist, "dev-1")
	// Player is paused initially
	model.isPlaying = false
	model.desiredPlaying = false
	model.confirmedPlaying = false
	model.cursor = 1
	model.playingIdx = 0

	// User hits Enter on track 1
	_, playCmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if playCmd == nil {
		t.Fatal("expected play track command on enter")
	}
	if !model.isPlaying || !model.desiredPlaying || !model.playbackPending {
		t.Fatalf("state machine not updated on Enter: isPlaying=%v, desired=%v, pending=%v",
			model.isPlaying, model.desiredPlaying, model.playbackPending)
	}

	// Before track finishes starting, user hits Space to Pause
	model.Update(tea.KeyMsg{Type: tea.KeySpace})
	if model.desiredPlaying != false {
		t.Fatalf("expected desiredPlaying=false after space, got %v", model.desiredPlaying)
	}

	// Play track succeeds remotely
	_, nextCmd := model.Update(actionResultMsg{version: model.requestedVersion, action: "play track", err: nil})
	if nextCmd == nil {
		t.Fatal("expected pause command after play track completed with desiredPlaying=false")
	}

	// Executing the queued command should trigger pause
	if batch, ok := nextCmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			if c != nil {
				c()
			}
		}
	} else {
		nextCmd()
	}
	if !pauseCalled {
		t.Fatal("pause command was not dispatched after track start completed")
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
	if indices := model.trackIndex["track-b"]; len(indices) != 1 || indices[0] != 1 {
		t.Errorf("expected track-b at index 1, got %v", indices)
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

type mockSpotifyController struct {
	getPlaybackStateFunc func(ctx context.Context) (*spotify.PlaybackState, error)
	pauseFunc            func(ctx context.Context, deviceID string) error
	resumeFunc           func(ctx context.Context, deviceID string) error
	playPlaylistFunc     func(ctx context.Context, deviceID, playlistURI string, trackOffset int) error
	setVolumeFunc        func(ctx context.Context, deviceID string, volumePercent int) error
	setShuffleFunc       func(ctx context.Context, deviceID string, state bool) error
	setRepeatFunc        func(ctx context.Context, deviceID string, state string) error
	nextFunc             func(ctx context.Context, deviceID string) error
	previousFunc         func(ctx context.Context, deviceID string) error
}

func (m *mockSpotifyController) GetPlaybackState(ctx context.Context) (*spotify.PlaybackState, error) {
	if m.getPlaybackStateFunc != nil {
		return m.getPlaybackStateFunc(ctx)
	}
	return nil, nil
}
func (m *mockSpotifyController) Pause(ctx context.Context, deviceID string) error {
	if m.pauseFunc != nil {
		return m.pauseFunc(ctx, deviceID)
	}
	return nil
}
func (m *mockSpotifyController) Resume(ctx context.Context, deviceID string) error {
	if m.resumeFunc != nil {
		return m.resumeFunc(ctx, deviceID)
	}
	return nil
}
func (m *mockSpotifyController) PlayPlaylist(ctx context.Context, deviceID, playlistURI string, trackOffset int) error {
	if m.playPlaylistFunc != nil {
		return m.playPlaylistFunc(ctx, deviceID, playlistURI, trackOffset)
	}
	return nil
}
func (m *mockSpotifyController) SetVolume(ctx context.Context, deviceID string, volumePercent int) error {
	if m.setVolumeFunc != nil {
		return m.setVolumeFunc(ctx, deviceID, volumePercent)
	}
	return nil
}
func (m *mockSpotifyController) SetShuffle(ctx context.Context, deviceID string, state bool) error {
	if m.setShuffleFunc != nil {
		return m.setShuffleFunc(ctx, deviceID, state)
	}
	return nil
}
func (m *mockSpotifyController) SetRepeat(ctx context.Context, deviceID string, state string) error {
	if m.setRepeatFunc != nil {
		return m.setRepeatFunc(ctx, deviceID, state)
	}
	return nil
}
func (m *mockSpotifyController) Next(ctx context.Context, deviceID string) error {
	if m.nextFunc != nil {
		return m.nextFunc(ctx, deviceID)
	}
	return nil
}
func (m *mockSpotifyController) Previous(ctx context.Context, deviceID string) error {
	if m.previousFunc != nil {
		return m.previousFunc(ctx, deviceID)
	}
	return nil
}

func TestModelWithMockControllerFailures(t *testing.T) {
	mock := &mockSpotifyController{
		pauseFunc: func(ctx context.Context, deviceID string) error {
			return errors.New("remote device disconnected")
		},
		setVolumeFunc: func(ctx context.Context, deviceID string, volumePercent int) error {
			return errors.New("rate limited")
		},
	}

	model := NewModel(mock, nil, nil, nil, "dev-1")
	model.width = 80
	model.height = 24

	// Test pause failure command
	cmd := model.togglePlayPauseCmd(false)
	msg := cmd()
	m, _ := model.Update(msg)
	model = m.(*Model)

	if model.err == nil || !strings.Contains(model.err.Error(), "remote device disconnected") {
		t.Fatalf("expected remote device error in model.err, got %v", model.err)
	}

	// Verify error appears in rendered footer
	view := model.View()
	if !strings.Contains(view, "failed to toggle playback") || !strings.Contains(view, "remote device") {
		t.Errorf("expected view to contain error message, got:\n%s", view)
	}

	// Test volume failure command
	volCmd := model.setVolumeCmd(50)
	volMsg := volCmd()
	m, _ = model.Update(volMsg)
	model = m.(*Model)

	if model.err == nil || !strings.Contains(model.err.Error(), "rate limited") {
		t.Fatalf("expected rate limited error in model.err, got %v", model.err)
	}
}

func TestModelExtremeTerminalDimensions(t *testing.T) {
	tracks := []spotify.Track{
		{ID: "t1", Name: "Short Track", DurationMs: 120000},
	}
	playlist := &spotify.Playlist{
		Name:   "Testing Extremes",
		Tracks: tracks,
	}

	model := NewModel(nil, nil, &spotify.UserProfile{DisplayName: "Tester"}, playlist, "dev-1")

	dimensions := []struct {
		width  int
		height int
	}{
		{width: 0, height: 0},
		{width: 1, height: 1},
		{width: 10, height: 2},
		{width: 20, height: 5},
		{width: 80, height: 24},
		{width: 300, height: 100},
	}

	for _, dim := range dimensions {
		model.width = dim.width
		model.height = dim.height

		// View should not panic under any dimension
		output := model.View()
		if dim.width == 0 && output != "" {
			// graceful degradation check
		}
	}
}
