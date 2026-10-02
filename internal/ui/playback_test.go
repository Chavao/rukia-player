package ui

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Chavao/rukia-player/internal/spotify"
	tea "github.com/charmbracelet/bubbletea"
)

func playbackFixture(client *mockSpotifyController) *Model {
	return NewModel(client, nil, nil, &spotify.Playlist{URI: "spotify:playlist:example", Tracks: []spotify.Track{{ID: "zero", PlaylistPosition: 0}, {ID: "one", PlaylistPosition: 1}, {ID: "two", PlaylistPosition: 2}}}, "device")
}

func playbackObservation(playing bool, id string) playbackStateMsg {
	return playbackStateMsg(&spotify.PlaybackState{IsPlaying: playing, Item: &spotify.Track{ID: id}})
}

func TestTrackSelectionRejectsStalePollUntilConfirmation(t *testing.T) {
	for _, initial := range []bool{false, true} {
		model := playbackFixture(&mockSpotifyController{})
		model.SetPlaybackInitialState(initial)
		cmd := model.selectTrack(1)
		if model.requestedVersion != 1 || !model.playbackPending || !model.desiredPlaying || !model.isPlaying {
			t.Fatal("selection did not establish local intent")
		}
		_, poll := model.Update(cmd())
		if poll == nil || !model.playbackAwaitingConfirmation || !model.confirmedPlaying || model.playbackPending {
			t.Fatal("successful play did not establish confirmation window")
		}
		model.Update(playbackObservation(false, "zero"))
		if !model.desiredPlaying || !model.isPlaying || !model.confirmedPlaying || model.playingIdx != 1 {
			t.Fatal("stale poll erased newer track/play intent")
		}
		model.Update(playbackObservation(true, "one"))
		if !model.isPlaying || !model.confirmedPlaying || model.playbackAwaitingConfirmation {
			t.Fatal("fresh matching poll did not confirm intent")
		}
		model.Update(tea.KeyMsg{Type: tea.KeySpace})
		if model.desiredPlaying {
			t.Fatal("next Space must pause")
		}
	}
}

func TestTrackConfirmationHasBoundedMismatchPolicy(t *testing.T) {
	model := playbackFixture(&mockSpotifyController{})
	model.SetPlaybackInitialState(false)
	model.Update(model.selectTrack(1)())
	for i := 1; i <= 3; i++ {
		model.Update(playbackObservation(false, "zero"))
		if i < 3 && (!model.isPlaying || !model.playbackAwaitingConfirmation) {
			t.Fatalf("intent discarded after %d observations", i)
		}
	}
	if model.isPlaying || model.desiredPlaying || model.confirmedPlaying || model.playbackAwaitingConfirmation || model.playingIdx != 0 {
		t.Fatal("third fresh mismatch must reconcile remote state")
	}
}

func TestPlaybackPollsStartedBeforeOrDuringCommandAreIgnored(t *testing.T) {
	model := playbackFixture(&mockSpotifyController{getPlaybackStateFunc: func(context.Context) (*spotify.PlaybackState, error) {
		return &spotify.PlaybackState{IsPlaying: false, Item: &spotify.Track{ID: "zero"}}, nil
	}})
	old := model.pollPlaybackCmd()
	play := model.selectTrack(1)
	during := model.pollPlaybackCmd()
	model.Update(play())
	for _, cmd := range []tea.Cmd{old, during} {
		model.Update(cmd())
		if !model.isPlaying || model.playingIdx != 1 || model.playbackObservationCount != 0 {
			t.Fatal("obsolete poll affected completed intent")
		}
	}
	fresh := model.pollPlaybackCmd()
	result := fresh()
	model.Update(result)
	model.Update(result) // A repeated result is not another fresh observation.
	if model.playbackObservationCount != 1 {
		t.Fatalf("count=%d, want 1", model.playbackObservationCount)
	}
}

func TestPlaybackPollRejectsOutOfOrderObservations(t *testing.T) {
	model := playbackFixture(&mockSpotifyController{getPlaybackStateFunc: func(context.Context) (*spotify.PlaybackState, error) {
		return &spotify.PlaybackState{IsPlaying: true}, nil
	}})
	old, newer := model.pollPlaybackCmd(), model.pollPlaybackCmd()
	model.Update(newer())
	result := old().(playbackPollResultMsg)
	result.state.IsPlaying = false
	model.Update(result)
	if !model.isPlaying {
		t.Fatal("older response overwrote newer observation")
	}
}

func TestTrackSelectionsAndPauseAreSerializedToLatestIntent(t *testing.T) {
	var calls []string
	client := &mockSpotifyController{
		playPlaylistFunc: func(_ context.Context, _, _ string, idx int) error {
			calls = append(calls, []string{"zero", "one", "two"}[idx])
			return nil
		},
		pauseFunc: func(context.Context, string) error { calls = append(calls, "pause"); return nil },
	}
	model := playbackFixture(client)
	first := model.selectTrack(1)
	if model.selectTrack(2) != nil {
		t.Fatal("second track request started while pending")
	}
	if model.togglePlayback() != nil || model.desiredPlaying {
		t.Fatal("pause must queue behind track requests")
	}
	_, second := model.Update(first())
	if !model.playbackPending || model.requestedTrack != 2 || model.requestedVersion != 3 {
		t.Fatal("latest track did not run next")
	}
	_, pause := model.Update(second())
	if pause == nil || !model.playbackPending || model.desiredPlaying {
		t.Fatal("pause did not follow latest selected track")
	}
	model.Update(pause())
	if !reflect.DeepEqual(calls, []string{"one", "two", "pause"}) || model.isPlaying || model.confirmedPlaying || model.desiredPlaying || model.playbackPending {
		t.Fatalf("calls=%v desired=%v confirmed=%v pending=%v", calls, model.desiredPlaying, model.confirmedPlaying, model.playbackPending)
	}
	model.Update(playbackObservation(true, "one"))
	if model.isPlaying || model.playingIdx != 2 {
		t.Fatal("stale track/pause observation overwrote intent")
	}
	model.Update(playbackObservation(false, "two"))
	if model.playbackAwaitingConfirmation {
		t.Fatal("remote pause did not confirm selected track")
	}
}

func TestFailedTrackReconcilesAndDoesNotShowSupersededError(t *testing.T) {
	failure := errors.New("play failed")
	client := &mockSpotifyController{playPlaylistFunc: func(context.Context, string, string, int) error { return failure }}
	model := playbackFixture(client)
	model.SetPlaybackInitialState(false)
	play := model.selectTrack(1)
	model.Update(play())
	if !model.playbackReconcile || model.err == nil || !model.desiredPlaying {
		t.Fatal("failed selection lost error/intent or reconciliation")
	}
	model.Update(playbackObservation(false, "zero"))
	if model.playbackReconcile || model.isPlaying || model.desiredPlaying || model.confirmedPlaying || model.playbackPending {
		t.Fatal("definitive failure did not reconcile remote state")
	}

	model = playbackFixture(client)
	play = model.selectTrack(1)
	model.selectTrack(2)
	model.Update(play())
	if model.err != nil || !model.playbackReconcile {
		t.Fatal("obsolete request must reconcile but not display its obsolete error")
	}
	_, next := model.Update(playbackObservation(false, "zero"))
	if next == nil || model.requestedTrack != 2 || !model.playbackPending {
		t.Fatal("failure discarded latest selection")
	}
}

func TestFreshAbsentPlaybackResolvesFailure(t *testing.T) {
	model := playbackFixture(&mockSpotifyController{})
	model.togglePlayback()
	model.Update(playbackChangedMsg{version: 1, playing: false, err: errors.New("disconnected")})
	model.Update(playbackStateMsg(nil))
	if model.playbackReconcile || model.playbackPending || model.isPlaying || model.desiredPlaying || model.confirmedPlaying {
		t.Fatal("204 observation left failed intent pending")
	}
	if model.togglePlayback() == nil {
		t.Fatal("fresh resume after absent playback must start")
	}
}

func TestRepeatedPollFailuresReleasePlaybackReconciliation(t *testing.T) {
	for _, newer := range []bool{false, true} {
		client := &mockSpotifyController{getPlaybackStateFunc: func(context.Context) (*spotify.PlaybackState, error) { return nil, errors.New("poll unavailable") }}
		model := playbackFixture(client)
		model.togglePlayback()
		model.Update(playbackChangedMsg{version: model.requestedVersion, playing: false, err: errors.New("control failed")})
		if newer {
			model.togglePlayback()
			model.togglePlayback() // Newer pause intent must survive failure of the old pause.
		}
		for i := 1; i <= 3; i++ {
			model.Update(model.pollPlaybackCmd()())
			if i < 3 && !model.playbackReconcile {
				t.Fatal("reconciliation ended before bounded fresh errors")
			}
		}
		if model.playbackReconcile {
			t.Fatal("poll failures permanently blocked playback controls")
		}
		if newer {
			if !model.playbackPending || model.desiredPlaying || model.requestedVersion != 3 {
				t.Fatal("latest intent did not proceed after failed reconciliation")
			}
		} else {
			if model.playbackPending || !model.desiredPlaying || !model.confirmedPlaying {
				t.Fatal("definitively failed command did not return to confirmed state")
			}
			if model.togglePlayback() == nil {
				t.Fatal("controls stayed blocked after bounded poll errors")
			}
		}
	}
}

func TestTrackSelectionSpaceWhileRequestRuns(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	calls := make(chan string, 2)
	client := &mockSpotifyController{
		playPlaylistFunc: func(context.Context, string, string, int) error {
			calls <- "play"
			close(started)
			<-release
			return nil
		},
		pauseFunc: func(context.Context, string) error { calls <- "pause"; return nil },
	}
	model := playbackFixture(client)
	model.SetPlaybackInitialState(false)
	command := model.selectTrack(1)
	done := make(chan tea.Msg, 1)
	go func() { done <- command() }()
	<-started
	if model.togglePlayback() != nil || model.desiredPlaying || !model.playbackPending {
		t.Fatal("Space did not queue behind in-flight Play")
	}
	close(release)
	_, pause := model.Update(<-done)
	if pause == nil {
		t.Fatal("queued pause did not start after Play completed")
	}
	model.Update(pause())
	if <-calls != "play" || <-calls != "pause" || model.isPlaying || model.confirmedPlaying || model.playbackPending {
		t.Fatal("concurrent track/pause did not converge to latest intent")
	}
}
