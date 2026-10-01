package ui

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Chavao/rukia-player/internal/spotify"
	tea "github.com/charmbracelet/bubbletea"
)

func volumeKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}} }
func volumeObservation(volume int) playbackStateMsg {
	return playbackStateMsg(&spotify.PlaybackState{Device: &spotify.Device{VolumePercent: volume}})
}

func volumeRemoteResult(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		return batch[0]()
	}
	return msg
}

func TestVolumePersistenceResolutionIsSeparateFromSuccess(t *testing.T) {
	for _, failure := range []bool{false, true} {
		settings := &volumeSettingsStub{volume: 50}
		if failure {
			settings.err = errors.New("disk full")
		}
		model := NewModel(&mockSpotifyController{}, nil, nil, nil, "", settings)
		_, cmd := model.Update(volumeKey())
		model.Update(volumeRemoteResult(t, cmd))
		gen := model.volumeGeneration.Load()
		model.Update(volumeObservation(50))
		if model.volume != 55 {
			t.Fatal("unresolved persistence accepted stale remote volume")
		}
		_, persist := model.Update(volumePersistMsg{generation: gen, volume: 55})
		model.Update(persist())
		if model.resolvedVolumeGeneration != gen {
			t.Fatal("persistence completion did not resolve suppression")
		}
		if failure {
			if model.persistedVolumeGeneration != 0 || settings.volume != 50 || model.err == nil {
				t.Fatal("failed write was recorded as durable")
			}
			model.Update(volumeObservation(31))
			if model.volume != 31 {
				t.Fatal("failed persistence permanently suppressed remote synchronization")
			}
		} else {
			if model.persistedVolumeGeneration != gen || settings.volume != 55 {
				t.Fatal("successful write not recorded")
			}
			model.Update(volumeObservation(55))
			model.Update(volumeObservation(31))
			if model.volume != 31 {
				t.Fatal("confirmed volume failed to synchronize remote changes")
			}
		}
	}
}

func TestStaleVolumePersistenceResultsCannotResolveNewGeneration(t *testing.T) {
	for _, err := range []error{nil, errors.New("old failure")} {
		settings := &volumeSettingsStub{volume: 50}
		model := NewModel(nil, nil, nil, nil, "", settings)
		model.Update(volumeKey())
		old := model.volumeGeneration.Load()
		model.Update(volumeKey())
		current := model.volumeGeneration.Load()
		model.Update(volumePersistedMsg{generation: old, err: err, exiting: true})
		if model.resolvedVolumeGeneration == current || model.persistedVolumeGeneration == current || model.err != nil {
			t.Fatal("stale result resolved/reported newer persistence")
		}
		model.volumePending = false // Isolate persistence suppression from network progress.
		model.Update(volumeObservation(20))
		if model.volume != 60 {
			t.Fatal("old completion opened synchronization for unresolved current generation")
		}
	}
}

func TestVolumeRequestsSerializeAndCoalesceWithoutActionPolls(t *testing.T) {
	var calls []int
	polls := 0
	client := &mockSpotifyController{
		setVolumeFunc: func(_ context.Context, _ string, v int) error {
			calls = append(calls, v)
			if len(calls) == 1 {
				return errors.New("old failure")
			}
			return nil
		},
		getPlaybackStateFunc: func(context.Context) (*spotify.PlaybackState, error) { polls++; return nil, nil },
	}
	model := NewModel(client, nil, nil, nil, "")
	model.volume, model.desiredVolume = 50, 50
	_, first := model.Update(volumeKey())
	if _, next := model.Update(volumeKey()); next != nil {
		t.Fatal("second volume request started concurrently")
	}
	model.Update(volumeKey())
	_, latest := model.Update(volumeRemoteResult(t, first))
	if latest == nil || model.err != nil {
		t.Fatal("older failure must dispatch latest volume without obsolete error")
	}
	model.Update(latest())
	if !reflect.DeepEqual(calls, []int{55, 65}) || polls != 0 || model.volumePending {
		t.Fatalf("calls=%v polls=%d pending=%v", calls, polls, model.volumePending)
	}
	// A delayed completion for the older generation cannot resolve the latest one.
	model.Update(actionResultMsg{action: "change volume", generation: 1, err: errors.New("old failure")})
	if model.err != nil {
		t.Fatal("obsolete volume completion surfaced")
	}
	_, cmd := model.Update(actionResultMsg{action: "change volume", generation: 3, err: errors.New("latest failure")})
	if model.err == nil || cmd == nil || polls != 0 {
		t.Fatal("latest failure must display without immediate poll")
	}
}

func TestPollStartedBeforeVolumeIntentCannotOverwriteIt(t *testing.T) {
	client := &mockSpotifyController{getPlaybackStateFunc: func(context.Context) (*spotify.PlaybackState, error) {
		return &spotify.PlaybackState{Device: &spotify.Device{VolumePercent: 50}}, nil
	}}
	model := NewModel(client, nil, nil, nil, "")
	model.volume, model.desiredVolume = 50, 50
	poll := model.pollPlaybackCmd()
	_, cmd := model.Update(volumeKey())
	model.Update(volumeRemoteResult(t, cmd))
	model.volumeAwaitingConfirmation = false
	model.Update(poll())
	if model.volume != 55 {
		t.Fatal("poll started before newer volume intent overwrote it")
	}
}

func TestVolumeNetworkConfirmationIsBounded(t *testing.T) {
	model := NewModel(&mockSpotifyController{}, nil, nil, nil, "")
	model.volume, model.desiredVolume = 50, 50
	_, cmd := model.Update(volumeKey())
	model.Update(volumeRemoteResult(t, cmd))
	for i := 1; i <= 3; i++ {
		model.Update(volumeObservation(50))
		if i < 3 && model.volume != 55 {
			t.Fatal("successful volume lost intent to stale observation")
		}
	}
	if model.volume != 50 || model.volumeAwaitingConfirmation {
		t.Fatal("volume reconciliation did not finish after bounded misses")
	}
}

func TestPollsStartedDuringVolumeRequestCannotExhaustConfirmation(t *testing.T) {
	settings := &volumeSettingsStub{volume: 50}
	client := &mockSpotifyController{getPlaybackStateFunc: func(context.Context) (*spotify.PlaybackState, error) {
		return &spotify.PlaybackState{Device: &spotify.Device{VolumePercent: 50}}, nil
	}}
	model := NewModel(client, nil, nil, nil, "", settings)
	_, put := model.Update(volumeKey())
	stale := []tea.Cmd{model.pollPlaybackCmd(), model.pollPlaybackCmd(), model.pollPlaybackCmd()}
	model.Update(volumeRemoteResult(t, put))
	_, persist := model.Update(volumePersistMsg{generation: model.volumeGeneration.Load(), volume: 55})
	model.Update(persist())
	for _, poll := range stale {
		model.Update(poll())
	}
	if model.volume != 55 || model.volumeObservationCount != 0 || !model.volumeAwaitingConfirmation {
		t.Fatalf("in-flight polls erased successful intent: volume=%d mismatches=%d", model.volume, model.volumeObservationCount)
	}
	for i := 0; i < 3; i++ {
		model.Update(model.pollPlaybackCmd()())
	}
	if model.volume != 50 || model.volumeAwaitingConfirmation {
		t.Fatal("three genuinely fresh observations did not reconcile volume")
	}
}

func TestVolumeCoalescesWhileNetworkRequestIsBlocked(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	calls := make(chan int, 2)
	client := &mockSpotifyController{setVolumeFunc: func(_ context.Context, _ string, v int) error {
		calls <- v
		if v == 55 {
			close(started)
			<-release
		}
		return nil
	}}
	model := NewModel(client, nil, nil, nil, "")
	model.volume, model.desiredVolume = 50, 50
	_, command := model.Update(volumeKey())
	done := make(chan tea.Msg, 1)
	go func() { done <- volumeRemoteResult(t, command) }()
	<-started
	for i := 0; i < 4; i++ {
		if _, cmd := model.Update(volumeKey()); cmd != nil {
			t.Fatal("repeat issued parallel volume request")
		}
	}
	close(release)
	_, latest := model.Update(<-done)
	if latest == nil {
		t.Fatal("latest volume did not dispatch after older completion")
	}
	model.Update(latest())
	if <-calls != 55 || <-calls != 75 || model.volumePending || model.volume != 75 {
		t.Fatal("volume requests did not preserve latest user intent")
	}
}
