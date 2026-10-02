package ui

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Chavao/rukia-player/internal/spotify"
	tea "github.com/charmbracelet/bubbletea"
)

func TestShuffleStartupQueuesToggleParity(t *testing.T) {
	for _, remote := range []bool{false, true} {
		for presses := 0; presses <= 3; presses++ {
			t.Run(fmt.Sprintf("remote=%v/presses=%d", remote, presses), func(t *testing.T) {
				var requests []bool
				model := NewModel(&mockSpotifyController{
					setShuffleFunc: func(_ context.Context, _ string, state bool) error {
						requests = append(requests, state)
						return nil
					},
				}, nil, nil, nil, "device")
				for i := 0; i < presses; i++ {
					if cmd := model.toggleShuffle(); cmd != nil {
						t.Fatal("unknown shuffle dispatched a request before synchronization")
					}
				}
				if model.shuffleKnown || model.shuffleEpoch != 0 || model.shuffleVersion != 0 {
					t.Fatal("queued startup intent changed shuffle synchronization state")
				}
				cmd := model.observeRemoteModes(&spotify.PlaybackState{ShuffleState: remote, RepeatState: "off"}, 0, 0)
				want := remote
				if presses%2 == 1 {
					want = !remote
					if cmd == nil {
						t.Fatal("odd queued intent did not dispatch after synchronization")
					}
					model.finishShuffleCommand(cmd().(actionResultMsg))
					if len(requests) != 1 || requests[0] != want {
						t.Fatalf("requests=%v, want [%v]", requests, want)
					}
				} else if cmd != nil || len(requests) != 0 {
					t.Fatal("even queued intent dispatched a shuffle request")
				}
				if !model.shuffleKnown || model.shuffle != want || model.queuedShuffleToggle {
					t.Fatalf("known=%v shuffle=%v queued=%v, want known shuffle=%v without queued intent", model.shuffleKnown, model.shuffle, model.queuedShuffleToggle, want)
				}
			})
		}
	}
}

func TestShuffleStartupRetainsIntentThroughMissingAndFailedPolls(t *testing.T) {
	model := NewModel(&mockSpotifyController{}, nil, nil, nil, "device")
	model.toggleShuffle()
	model.Update(playbackPollResultMsg{sequence: 1, err: errors.New("Spotify unavailable")})
	model.Update(playbackPollResultMsg{sequence: 2})
	if model.shuffleKnown || !model.queuedShuffleToggle || model.shufflePending {
		t.Fatal("failed or absent playback discarded unknown shuffle intent")
	}
	if model.err == nil {
		t.Fatal("startup polling error was not reported")
	}
	_, cmd := model.Update(playbackPollResultMsg{sequence: 3, state: &spotify.PlaybackState{ShuffleState: true, RepeatState: "off"}})
	if cmd == nil || !model.shuffleKnown || model.shuffle || !model.shufflePending {
		t.Fatal("successful recovery did not toggle the observed enabled shuffle")
	}
	result := cmd().(actionResultMsg)
	model.Update(result)
	if model.shufflePending || !model.shuffleAwaitingConfirmation {
		t.Fatal("recovered shuffle request did not finish normally")
	}
}

func TestShuffleMissingPlaybackDoesNotResetOrConfirm(t *testing.T) {
	model := NewModel(&mockSpotifyController{}, nil, nil, nil, "device")
	model.observeRemoteModes(&spotify.PlaybackState{ShuffleState: true, RepeatState: "off"}, 0, 0)
	model.observeRemoteModes(nil, 0, 0)
	if !model.shuffleKnown || !model.shuffle {
		t.Fatal("absent playback reset known enabled shuffle")
	}
	cmd := model.toggleShuffle()
	model.finishShuffleCommand(cmd().(actionResultMsg))
	for i := 0; i < modeConfirmationObservations; i++ {
		model.observeRemoteModes(nil, model.shuffleEpoch, model.repeatEpoch)
	}
	if !model.shuffleAwaitingConfirmation || model.shuffleObservationCount != 0 {
		t.Fatal("absent playback counted as authoritative shuffle confirmation")
	}
	model.observeRemoteModes(&spotify.PlaybackState{ShuffleState: false, RepeatState: "off"}, model.shuffleEpoch, model.repeatEpoch)
	if model.shuffleAwaitingConfirmation || model.shuffle {
		t.Fatal("valid playback did not confirm disabled shuffle")
	}
}

func TestShuffleObservationsSurvivePlaybackIntentChanges(t *testing.T) {
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeySpace},
		{Type: tea.KeyEnter},
	} {
		t.Run(key.String(), func(t *testing.T) {
			model := NewModel(&mockSpotifyController{}, nil, nil, &spotify.Playlist{Tracks: []spotify.Track{{ID: "track"}}}, "device")
			model.toggleShuffle()
			poll := playbackPollResultMsg{sequence: 1, state: &spotify.PlaybackState{ShuffleState: true, RepeatState: "off"}}
			model.Update(key)
			if model.playbackVersion.Load() == poll.version {
				t.Fatal("playback key did not establish a newer intent")
			}
			_, cmd := model.Update(poll)
			if cmd == nil || !model.shuffleKnown || model.shuffle || !model.shufflePending {
				t.Fatal("playback intent invalidated independent shuffle synchronization")
			}
			if model.appliedPollSequence != 0 {
				t.Fatal("stale playback state was accepted alongside mode observation")
			}
		})
	}
}

func TestShuffleExternalObservationsRejectOutOfOrderPolls(t *testing.T) {
	model := NewModel(&mockSpotifyController{}, nil, nil, nil, "device")
	model.Update(playbackPollResultMsg{sequence: 2, state: &spotify.PlaybackState{ShuffleState: true, RepeatState: "off"}})
	model.Update(playbackPollResultMsg{sequence: 1, state: &spotify.PlaybackState{ShuffleState: false, RepeatState: "off"}})
	if !model.shuffle {
		t.Fatal("older mode observation overwrote newer shuffle state")
	}
	model.Update(playbackPollResultMsg{sequence: 3, state: &spotify.PlaybackState{ShuffleState: false, RepeatState: "off"}})
	if model.shuffle || !model.shuffleKnown {
		t.Fatal("fresh external shuffle change was not synchronized")
	}
}

func TestShuffleRejectsModeEpochWhileAcceptingIndependentRepeat(t *testing.T) {
	model := NewModel(&mockSpotifyController{}, nil, nil, nil, "device")
	model.observeRemoteModes(&spotify.PlaybackState{RepeatState: "off"}, 0, 0)
	cmd := model.toggleShuffle()
	model.finishShuffleCommand(cmd().(actionResultMsg))
	model.observeRemoteModes(&spotify.PlaybackState{ShuffleState: false, RepeatState: "context"}, 0, model.repeatEpoch)
	if !model.shuffle || !model.shuffleAwaitingConfirmation || model.shuffleObservationCount != 0 {
		t.Fatal("stale shuffle epoch overwrote or confirmed current shuffle intent")
	}
	if model.repeatMode != "context" {
		t.Fatal("stale shuffle epoch discarded independent repeat observation")
	}
}

func TestShuffleCommandFailureReconcilesWithRemoteState(t *testing.T) {
	model := NewModel(&mockSpotifyController{}, nil, nil, nil, "device")
	model.observeRemoteModes(&spotify.PlaybackState{ShuffleState: true, RepeatState: "off"}, 0, 0)
	cmd := model.toggleShuffle()
	result := cmd().(actionResultMsg)
	result.err = errors.New("shuffle request rejected")
	if model.finishShuffleCommand(result) == nil {
		t.Fatal("shuffle failure did not schedule reconciliation")
	}
	if model.err == nil || model.shufflePending || model.shuffleAwaitingConfirmation {
		t.Fatal("shuffle command failure was not reported and settled")
	}
	model.observeRemoteModes(&spotify.PlaybackState{ShuffleState: true, RepeatState: "off"}, model.shuffleEpoch, model.repeatEpoch)
	if !model.shuffleKnown || !model.shuffle {
		t.Fatal("failed shuffle request did not reconcile with authoritative remote state")
	}
}
