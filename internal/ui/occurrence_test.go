package ui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Chavao/rukia-player/internal/spotify"
	tea "github.com/charmbracelet/bubbletea"
)

func occurrenceFixture(client *mockSpotifyController) *Model {
	return NewModel(client, nil, nil, &spotify.Playlist{
		URI: "spotify:playlist:duplicates",
		Tracks: []spotify.Track{
			{ID: "duplicate", Name: "Repeated Song", PlaylistPosition: 1, DurationMs: 120000},
			{ID: "unique", Name: "Other Song", PlaylistPosition: 2, DurationMs: 90000},
			{ID: "duplicate", Name: "Repeated Song", PlaylistPosition: 4, DurationMs: 120000},
			{ID: "duplicate", Name: "Repeated Song", PlaylistPosition: 101, DurationMs: 120000},
		},
	}, "device")
}

func TestDuplicateSelectionKeepsExactOccurrence(t *testing.T) {
	for _, idx := range []int{0, 2, 3} {
		t.Run(fmt.Sprintf("row=%d", idx), func(t *testing.T) {
			var offsets []int
			model := occurrenceFixture(&mockSpotifyController{
				playPlaylistFunc: func(_ context.Context, _, _ string, offset int) error {
					offsets = append(offsets, offset)
					return nil
				},
			})
			model.cursor = idx
			_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if cmd == nil {
				t.Fatal("unconfirmed row must start its exact occurrence")
			}
			model.Update(cmd())
			wantOffset := model.playlist.Tracks[idx].PlaylistPosition
			if !reflect.DeepEqual(offsets, []int{wantOffset}) {
				t.Fatalf("offsets=%v, want [%d]", offsets, wantOffset)
			}
			for i := 0; i < 4; i++ {
				model.Update(playbackObservation(true, "duplicate"))
				if model.playingIdx != idx || model.confirmedTrackIdx != idx {
					t.Fatalf("poll %d selected row=%d confirmed=%d, want %d", i, model.playingIdx, model.confirmedTrackIdx, idx)
				}
			}
			if strings.Count(model.View(), "✓") != 1 {
				t.Fatal("exactly one occurrence must receive the playing checkmark")
			}
			_, pause := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if pause == nil || model.desiredPlaying {
				t.Fatal("Enter on the playing occurrence must pause")
			}
			model.Update(pause())
			model.Update(playbackObservation(false, "duplicate"))
			_, resume := model.Update(tea.KeyMsg{Type: tea.KeySpace})
			model.Update(resume())
			model.Update(playbackObservation(true, "duplicate"))
			if model.playingIdx != idx || len(offsets) != 1 {
				t.Fatal("pause/resume restarted or relabeled the duplicate occurrence")
			}
		})
	}
}

func TestDuplicateEnterOnDifferentOccurrenceStartsSelectedRow(t *testing.T) {
	var offsets []int
	model := occurrenceFixture(&mockSpotifyController{
		playPlaylistFunc: func(_ context.Context, _, _ string, offset int) error {
			offsets = append(offsets, offset)
			return nil
		},
	})
	model.SetInitialPlaybackTrack(0)
	model.Update(playbackObservation(true, "duplicate"))
	model.cursor = 2
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model.Update(cmd())
	model.Update(playbackObservation(true, "duplicate"))
	if !reflect.DeepEqual(offsets, []int{4}) || model.playingIdx != 2 || !model.isPlaying {
		t.Fatalf("offsets=%v row=%d playing=%v", offsets, model.playingIdx, model.isPlaying)
	}
}

func TestAmbiguousDuplicateKeepsMetadataWithoutRowCheckmark(t *testing.T) {
	model := occurrenceFixture(&mockSpotifyController{})
	if model.playingIdx != -1 || model.confirmedTrackIdx != -1 {
		t.Fatal("startup must not assume a playlist occurrence")
	}
	model.Update(playbackStateMsg(&spotify.PlaybackState{
		IsPlaying: true, ProgressMs: 12000,
		Item: &spotify.Track{ID: "duplicate", Name: "Repeated Song", DurationMs: 120000},
	}))
	view := model.View()
	if model.playingIdx != -1 || strings.Contains(view, "✓") || !strings.Contains(view, "■ Repeated Song") || !strings.Contains(view, "0:12 / 2:00") {
		t.Fatalf("ambiguous occurrence lost metadata or guessed a row:\n%s", view)
	}
	model.Update(playbackObservation(true, "unique"))
	if model.playingIdx != 1 {
		t.Fatal("a unique song must establish its row")
	}
	model.Update(playbackObservation(true, "duplicate"))
	if model.playingIdx != -1 {
		t.Fatal("returning to an ambiguous song must not reuse another song's occurrence")
	}
}

func TestDuplicateFailedSelectionDoesNotConfirmOptimisticRow(t *testing.T) {
	model := occurrenceFixture(&mockSpotifyController{
		playPlaylistFunc: func(context.Context, string, string, int) error {
			return errors.New("play rejected")
		},
	})
	model.SetInitialPlaybackTrack(0)
	model.Update(model.selectTrack(2)())
	if model.playingIdx != -1 || model.confirmedTrackIdx != -1 {
		t.Fatal("failed selection left an optimistic or assumed confirmed occurrence")
	}
	model.Update(playbackObservation(true, "duplicate"))
	if model.playingIdx != -1 || model.playbackReconcile || model.err == nil {
		t.Fatal("ambiguous reconciliation confirmed a failed selection or hid its error")
	}
	model.Update(playbackObservation(true, "unique"))
	if model.playingIdx != 1 {
		t.Fatal("unique remote state did not recover after failed selection")
	}
}

func TestDuplicateQueuedSelectionsPreserveLatestOccurrence(t *testing.T) {
	var offsets []int
	model := occurrenceFixture(&mockSpotifyController{
		playPlaylistFunc: func(_ context.Context, _, _ string, offset int) error {
			offsets = append(offsets, offset)
			return nil
		},
	})
	first := model.selectTrack(0)
	if model.selectTrack(2) != nil || model.selectTrack(3) != nil {
		t.Fatal("queued selections must not dispatch concurrent playback requests")
	}
	_, latest := model.Update(first())
	model.Update(playbackObservation(true, "duplicate"))
	if model.playingIdx != 3 || model.confirmedTrackIdx != 0 {
		t.Fatal("in-flight latest selection was overwritten by an earlier acknowledgement or poll")
	}
	model.Update(latest())
	model.Update(playbackObservation(true, "duplicate"))
	if !reflect.DeepEqual(offsets, []int{1, 101}) || model.playingIdx != 3 || model.confirmedTrackIdx != 3 {
		t.Fatalf("offsets=%v row=%d confirmed=%d", offsets, model.playingIdx, model.confirmedTrackIdx)
	}
}

func TestDuplicateQueuedSelectionSurvivesFailedRequest(t *testing.T) {
	var offsets []int
	model := occurrenceFixture(&mockSpotifyController{
		playPlaylistFunc: func(_ context.Context, _, _ string, offset int) error {
			offsets = append(offsets, offset)
			if len(offsets) == 1 {
				return errors.New("old selection failed")
			}
			return nil
		},
	})
	first := model.selectTrack(0)
	model.selectTrack(3)
	model.Update(first())
	_, latest := model.Update(playbackObservation(true, "duplicate"))
	if latest == nil || model.playingIdx != 3 || model.err != nil {
		t.Fatal("failure reconciliation discarded or relabeled the latest selection")
	}
	model.Update(latest())
	model.Update(playbackObservation(true, "duplicate"))
	if model.playingIdx != 3 || model.confirmedTrackIdx != 3 || !reflect.DeepEqual(offsets, []int{1, 101}) {
		t.Fatal("latest duplicate did not establish its acknowledged occurrence")
	}
}

func TestDuplicateQueuedSelectionThenPauseKeepsLatestRow(t *testing.T) {
	var calls []string
	model := occurrenceFixture(&mockSpotifyController{
		playPlaylistFunc: func(_ context.Context, _, _ string, offset int) error {
			calls = append(calls, fmt.Sprintf("play:%d", offset))
			return nil
		},
		pauseFunc: func(context.Context, string) error {
			calls = append(calls, "pause")
			return nil
		},
	})
	first := model.selectTrack(0)
	model.selectTrack(3)
	model.Update(tea.KeyMsg{Type: tea.KeySpace})
	_, latest := model.Update(first())
	_, pause := model.Update(latest())
	model.Update(pause())
	model.Update(playbackObservation(false, "duplicate"))
	if !reflect.DeepEqual(calls, []string{"play:1", "play:101", "pause"}) || model.playingIdx != 3 || model.confirmedTrackIdx != 3 || model.isPlaying {
		t.Fatalf("calls=%v row=%d confirmed=%d playing=%v", calls, model.playingIdx, model.confirmedTrackIdx, model.isPlaying)
	}
}

func TestOccurrenceClearsForAbsentOrUnrelatedPlayback(t *testing.T) {
	for _, item := range []*spotify.Track{nil, {ID: "outside", Name: "External Song"}} {
		model := occurrenceFixture(&mockSpotifyController{})
		model.SetInitialPlaybackTrack(0)
		model.Update(playbackObservation(true, "duplicate"))
		model.Update(playbackStateMsg(&spotify.PlaybackState{Item: item}))
		if model.playingIdx != -1 || model.confirmedTrackIdx != -1 || model.currentTrack != item {
			t.Fatal("absent or unrelated playback retained a playlist row")
		}
	}
}

func TestInitialDuplicateOccurrenceSurvivesStaleStartupPoll(t *testing.T) {
	model := occurrenceFixture(&mockSpotifyController{})
	model.SetInitialPlaybackTrack(0)
	model.Update(playbackObservation(false, "unique"))
	if model.playingIdx != 0 || !model.playbackAwaitingConfirmation {
		t.Fatal("stale startup playback erased the acknowledged initial occurrence")
	}
	model.Update(playbackObservation(true, "duplicate"))
	if model.playingIdx != 0 || model.confirmedTrackIdx != 0 || model.playbackAwaitingConfirmation {
		t.Fatal("matching startup poll did not retain the exact initial occurrence")
	}
}

func TestEnterOnEmptyPlaylistDoesNotStartPlayback(t *testing.T) {
	model := NewModel(&mockSpotifyController{}, nil, nil, &spotify.Playlist{}, "device")
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || model.playbackPending {
		t.Fatal("empty playlist must not issue a row playback request")
	}
}
