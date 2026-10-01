package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Chavao/rukia-player/internal/spotify"
	tea "github.com/charmbracelet/bubbletea"
)

func TestQueuedOAuthWarningIsDisplayedAndWaitsForNext(t *testing.T) {
	warnings := make(chan error, 1)
	warning := errors.New("failed to save refreshed Spotify token: disk full")
	warnings <- warning // Arrives before the UI exists.
	model := NewModel(nil, nil, nil, nil, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model.SetWarningChannel(ctx, warnings)
	_, next := model.Update(model.waitForWarningCmd()())
	if model.err != warning || !strings.Contains(model.View(), "failed to save refreshed Spotify token") || next == nil {
		t.Fatal("queued token warning did not reach visible UI and schedule next waiter")
	}
	if batch, ok := next().(tea.BatchMsg); !ok || len(batch) != 2 {
		t.Fatal("warning must schedule expiration and next warning")
	}
	cancel()
	if msg := model.waitForWarningCmd()(); msg != nil {
		t.Fatalf("shutdown waiter returned %T", msg)
	}
}

func TestWarningWaiterClosedAndNilChannelsTerminate(t *testing.T) {
	model := NewModel(nil, nil, nil, nil, "")
	if model.waitForWarningCmd() != nil {
		t.Fatal("nil channel scheduled waiter")
	}
	warnings := make(chan error)
	close(warnings)
	model.SetWarningChannel(context.Background(), warnings)
	if model.waitForWarningCmd()() != nil {
		t.Fatal("closed channel did not stop waiter")
	}
}

func TestInitialErrorExpiresAndCannotClearNewerError(t *testing.T) {
	model := NewModel(nil, nil, nil, nil, "")
	model.SetInitialError(errors.New("startup playback failed"))
	if model.initialErrorTimer() == nil {
		t.Fatal("initial error did not schedule normal expiration")
	}
	initial := model.initialErrorGeneration
	model.Update(clearErrorMsg{generation: initial})
	if model.err != nil {
		t.Fatal("initial timer did not expire startup error")
	}

	model.SetInitialError(errors.New("startup playback failed again"))
	initial = model.initialErrorGeneration
	newer := errors.New("new token warning")
	model.Update(warningMsg{err: newer})
	model.Update(clearErrorMsg{generation: initial})
	if model.err != newer {
		t.Fatal("old startup timer erased newer error")
	}
	model.clearInitialPlaybackError()
	if model.err != newer {
		t.Fatal("playback recovery erased unrelated newer warning")
	}
}

func TestInitialPlaybackErrorClearsAfterRecovery(t *testing.T) {
	for _, viaPoll := range []bool{false, true} {
		model := playbackFixture(&mockSpotifyController{})
		model.SetPlaybackInitialState(false)
		model.SetInitialError(errors.New("startup playback failed"))
		if viaPoll {
			model.Update(playbackObservation(true, "zero"))
		} else {
			model.Update(model.selectTrack(1)())
		}
		if model.err != nil {
			t.Fatal("successful playback recovery retained obsolete startup error")
		}
	}
}

func TestAllSpotifyCommandsHonorParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	seen := 0
	verify := func(ctx context.Context) error {
		seen++
		if _, ok := ctx.Deadline(); !ok {
			t.Error("command escaped bounded deadline")
		}
		<-ctx.Done()
		return ctx.Err()
	}
	client := &mockSpotifyController{
		getPlaybackStateFunc: func(ctx context.Context) (*spotify.PlaybackState, error) { return nil, verify(ctx) },
		pauseFunc:            func(ctx context.Context, _ string) error { return verify(ctx) },
		playPlaylistFunc:     func(ctx context.Context, _, _ string, _ int) error { return verify(ctx) },
		setVolumeFunc:        func(ctx context.Context, _ string, _ int) error { return verify(ctx) },
		setShuffleFunc:       func(ctx context.Context, _ string, _ bool) error { return verify(ctx) },
		setRepeatFunc:        func(ctx context.Context, _, _ string) error { return verify(ctx) },
	}
	model := playbackFixture(client)
	model.SetWarningChannel(ctx, nil)
	commands := []tea.Cmd{model.pollPlaybackCmd(), model.togglePlayPauseCmd(false), model.playTrackIndexCmd(1), model.setVolumeCmd(50), model.setShuffleCmd(true), model.setRepeatCmd("context")}
	cancel()
	// Playback control checks cancellation before I/O; other commands pass the canceled
	// context to their API boundary. None may block after parent cancellation.
	for _, cmd := range commands {
		done := make(chan struct{})
		go func() { cmd(); close(done) }()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("command ignored parent cancellation")
		}
	}
	if seen != 5 {
		t.Fatalf("boundary calls=%d, want5", seen)
	}
}
