package ui

import (
	"context"
	"testing"

	"github.com/Chavao/rukia-player/internal/mpris"
	"github.com/Chavao/rukia-player/internal/spotify"
	tea "github.com/charmbracelet/bubbletea"
)

type mockMPRISNotifier struct {
	status     string
	track      *spotify.Track
	volume     int
	shuffle    bool
	repeat     string
	positionMs int
}

func (m *mockMPRISNotifier) UpdatePlaybackState(status string, track *spotify.Track, volume int, shuffle bool, repeat string, positionMs int) {
	m.status = status
	m.track = track
	m.volume = volume
	m.shuffle = shuffle
	m.repeat = repeat
	m.positionMs = positionMs
}

func (m *mockMPRISNotifier) UpdateStatus(status string) {
	m.status = status
}

func (m *mockMPRISNotifier) UpdateVolume(volume int) {
	m.volume = volume
}

func (m *mockMPRISNotifier) UpdateTrack(track *spotify.Track) {
	m.track = track
}

func (m *mockMPRISNotifier) UpdateShuffle(shuffle bool) {
	m.shuffle = shuffle
}

func (m *mockMPRISNotifier) UpdateRepeat(repeat string) {
	m.repeat = repeat
}

func sampleTestPlaylist() *spotify.Playlist {
	return &spotify.Playlist{
		ID:  "pl1",
		URI: "spotify:playlist:pl1",
		Tracks: []spotify.Track{
			{ID: "t1", Name: "Track 1", Artist: "Artist 1", DurationMs: 180000, PlaylistPosition: 0},
			{ID: "t2", Name: "Track 2", Artist: "Artist 2", DurationMs: 200000, PlaylistPosition: 1},
			{ID: "t3", Name: "Track 3", Artist: "Artist 3", DurationMs: 220000, PlaylistPosition: 2},
		},
	}
}

func TestModelMPRISIntegration(t *testing.T) {
	playlist := sampleTestPlaylist()
	var nextCalls, prevCalls int
	ctrl := &mockSpotifyController{
		nextFunc: func(ctx context.Context, deviceID string) error {
			nextCalls++
			return nil
		},
		previousFunc: func(ctx context.Context, deviceID string) error {
			prevCalls++
			return nil
		},
		getPlaybackStateFunc: func(ctx context.Context) (*spotify.PlaybackState, error) {
			return &spotify.PlaybackState{
				IsPlaying:  true,
				ProgressMs: 5000,
				Item:       &playlist.Tracks[1],
			}, nil
		},
	}

	model := NewModel(ctrl, nil, nil, playlist, "dev1")
	notifier := &mockMPRISNotifier{}
	model.SetMPRIS(notifier)

	// Verify SetMPRIS initializes state
	if notifier.status != "Playing" {
		t.Errorf("expected initial status Playing, got %s", notifier.status)
	}

	// Test Next message
	model.playingIdx = 0
	model.currentTrack = &playlist.Tracks[0]
	_, nextCmd := model.Update(mpris.NextMsg{})
	if nextCmd == nil {
		t.Fatal("expected command from NextMsg")
	}
	if model.playingIdx != 1 || model.currentTrack.ID != "t2" {
		t.Errorf("expected track index 1 and ID t2, got idx=%d track=%+v", model.playingIdx, model.currentTrack)
	}
	if notifier.track == nil || notifier.track.ID != "t2" {
		t.Errorf("expected notifier track to be updated to t2, got %+v", notifier.track)
	}

	// Execute command and verify ctrl.Next was called
	res := nextCmd().(actionResultMsg)
	if res.action != "skip next" || res.err != nil {
		t.Errorf("unexpected skip next result: %+v", res)
	}
	if nextCalls != 1 {
		t.Errorf("expected 1 next call, got %d", nextCalls)
	}

	// Test Previous message
	_, prevCmd := model.Update(mpris.PreviousMsg{})
	if prevCmd == nil {
		t.Fatal("expected command from PreviousMsg")
	}
	if model.playingIdx != 0 || model.currentTrack.ID != "t1" {
		t.Errorf("expected track index 0 and ID t1, got idx=%d track=%+v", model.playingIdx, model.currentTrack)
	}
	if notifier.track == nil || notifier.track.ID != "t1" {
		t.Errorf("expected notifier track to be updated to t1, got %+v", notifier.track)
	}

	res = prevCmd().(actionResultMsg)
	if res.action != "skip previous" || res.err != nil {
		t.Errorf("unexpected skip previous result: %+v", res)
	}
	if prevCalls != 1 {
		t.Errorf("expected 1 prev call, got %d", prevCalls)
	}

	// Test PlayPause toggle message
	_, toggleCmd := model.Update(mpris.TogglePlayPauseMsg{})
	if toggleCmd == nil {
		t.Fatal("expected command from TogglePlayPauseMsg")
	}
	if model.isPlaying {
		t.Error("expected isPlaying to be false after toggle")
	}
	if notifier.status != "Paused" {
		t.Errorf("expected notifier status Paused, got %s", notifier.status)
	}
	// Finish the pending playback command
	model.Update(playbackChangedMsg{version: model.requestedVersion, playing: false})

	// Test PauseMsg when already paused should be a no-op
	_, pauseCmd := model.Update(mpris.PauseMsg{})
	if pauseCmd != nil {
		t.Error("expected PauseMsg when already paused to return nil command")
	}

	// Test PlayMsg resumes
	_, playCmd := model.Update(mpris.PlayMsg{})
	if playCmd == nil {
		t.Fatal("expected PlayMsg to trigger playback toggle command")
	}
	if !model.isPlaying {
		t.Error("expected isPlaying to be true after PlayMsg")
	}

	// Test VolumeMsg
	_, volCmd := model.Update(mpris.VolumeMsg{Percent: 75})
	if volCmd == nil {
		t.Fatal("expected command from VolumeMsg")
	}
	if model.volume != 75 {
		t.Errorf("expected volume 75, got %d", model.volume)
	}
	if notifier.volume != 75 {
		t.Errorf("expected notifier volume 75, got %d", notifier.volume)
	}

	// Test QuitMsg returns tea.Quit
	_, quitCmd := model.Update(mpris.QuitMsg{})
	if quitCmd == nil {
		t.Fatal("expected command from QuitMsg")
	}
	if quitCmd() != tea.Quit() {
		t.Error("expected QuitMsg to return tea.Quit")
	}
}

func TestModelKeyBindingsNextPrev(t *testing.T) {
	playlist := sampleTestPlaylist()
	var nextCalled, prevCalled bool
	ctrl := &mockSpotifyController{
		nextFunc: func(ctx context.Context, deviceID string) error {
			nextCalled = true
			return nil
		},
		previousFunc: func(ctx context.Context, deviceID string) error {
			prevCalled = true
			return nil
		},
	}

	model := NewModel(ctrl, nil, nil, playlist, "dev1")
	model.playingIdx = 0
	model.currentTrack = &playlist.Tracks[0]

	// Press 'n'
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if cmd == nil {
		t.Fatal("expected command on 'n' keypress")
	}
	cmd()
	if !nextCalled {
		t.Error("expected next to be called on 'n'")
	}

	// Press 'p'
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if cmd == nil {
		t.Fatal("expected command on 'p' keypress")
	}
	cmd()
	if !prevCalled {
		t.Error("expected previous to be called on 'p'")
	}
}
