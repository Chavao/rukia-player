package ui

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Chavao/rukia-player/internal/spotify"
)

func TestShuffleSerializesAndCoalescesLatestIntent(t *testing.T) {
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	var active atomic.Int32
	var maxActive atomic.Int32
	var mu sync.Mutex
	var calls []bool
	client := &mockSpotifyController{
		setShuffleFunc: func(ctx context.Context, _ string, state bool) error {
			current := active.Add(1)
			defer active.Add(-1)
			for {
				seen := maxActive.Load()
				if current <= seen || maxActive.CompareAndSwap(seen, current) {
					break
				}
			}
			mu.Lock()
			calls = append(calls, state)
			call := len(calls)
			mu.Unlock()
			if call == 1 {
				close(firstEntered)
				select {
				case <-releaseFirst:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		},
	}
	model := NewModel(client, nil, nil, nil, "device")
	model.observeRemoteModes(&spotify.PlaybackState{RepeatState: "off"}, model.shuffleEpoch, model.repeatEpoch)

	first := model.toggleShuffle()
	if first == nil || !model.shuffle || !model.shufflePending {
		t.Fatal("first shuffle intent did not start")
	}
	results := make(chan actionResultMsg, 1)
	go func() { results <- first().(actionResultMsg) }()
	select {
	case <-firstEntered:
	case <-time.After(time.Second):
		t.Fatal("first shuffle request did not start")
	}

	if next := model.toggleShuffle(); next != nil {
		t.Fatal("newer shuffle intent started concurrently instead of coalescing")
	}
	if model.shuffle {
		t.Fatal("latest local shuffle intent should be false")
	}

	close(releaseFirst)
	var firstResult actionResultMsg
	select {
	case firstResult = <-results:
	case <-time.After(time.Second):
		t.Fatal("first shuffle request did not finish")
	}
	next := model.finishShuffleCommand(firstResult)
	if next == nil {
		t.Fatal("coalesced shuffle intent was not dispatched")
	}
	secondResult := next().(actionResultMsg)
	model.finishShuffleCommand(secondResult)

	mu.Lock()
	got := append([]bool(nil), calls...)
	mu.Unlock()
	if len(got) != 2 || !got[0] || got[1] {
		t.Fatalf("shuffle requests=%v, want [true false]", got)
	}
	if maxActive.Load() != 1 {
		t.Fatalf("shuffle requests overlapped; max concurrency=%d", maxActive.Load())
	}
	if model.shuffle || model.shufflePending {
		t.Fatalf("final shuffle state=%v pending=%v", model.shuffle, model.shufflePending)
	}
}

func TestRepeatSerializesAndCoalescesLatestIntent(t *testing.T) {
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	var mu sync.Mutex
	var calls []string
	client := &mockSpotifyController{
		setRepeatFunc: func(ctx context.Context, _ string, mode string) error {
			mu.Lock()
			calls = append(calls, mode)
			call := len(calls)
			mu.Unlock()
			if call == 1 {
				close(firstEntered)
				select {
				case <-releaseFirst:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		},
	}
	model := NewModel(client, nil, nil, nil, "device")

	first := model.toggleRepeat()
	if first == nil || model.repeatMode != "context" || !model.repeatPending {
		t.Fatal("first repeat intent did not start")
	}
	results := make(chan actionResultMsg, 1)
	go func() { results <- first().(actionResultMsg) }()
	select {
	case <-firstEntered:
	case <-time.After(time.Second):
		t.Fatal("first repeat request did not start")
	}

	if next := model.toggleRepeat(); next != nil {
		t.Fatal("newer repeat intent started concurrently instead of coalescing")
	}
	if model.repeatMode != "off" {
		t.Fatalf("latest local repeat intent=%q, want off", model.repeatMode)
	}

	close(releaseFirst)
	firstResult := <-results
	next := model.finishRepeatCommand(firstResult)
	if next == nil {
		t.Fatal("coalesced repeat intent was not dispatched")
	}
	secondResult := next().(actionResultMsg)
	model.finishRepeatCommand(secondResult)

	mu.Lock()
	got := append([]string(nil), calls...)
	mu.Unlock()
	if len(got) != 2 || got[0] != "context" || got[1] != "off" {
		t.Fatalf("repeat requests=%v, want [context off]", got)
	}
	if model.repeatMode != "off" || model.repeatPending {
		t.Fatalf("final repeat state=%q pending=%v", model.repeatMode, model.repeatPending)
	}
}

func TestModePollsCannotOverwriteNewerIntent(t *testing.T) {
	model := NewModel(&mockSpotifyController{}, nil, nil, nil, "device")
	model.observeRemoteModes(&spotify.PlaybackState{RepeatState: "off"}, model.shuffleEpoch, model.repeatEpoch)

	shuffleCmd := model.toggleShuffle()
	shufflePollEpoch := model.shuffleEpoch
	shuffleResult := shuffleCmd().(actionResultMsg)
	model.finishShuffleCommand(shuffleResult)
	if !model.shuffleAwaitingConfirmation {
		t.Fatal("shuffle success did not await remote confirmation")
	}
	model.observeRemoteModes(&spotify.PlaybackState{ShuffleState: false, RepeatState: "off"}, shufflePollEpoch, model.repeatEpoch)
	if !model.shuffle {
		t.Fatal("poll started during shuffle request overwrote newer intent")
	}
	for i := 0; i < modeConfirmationObservations-1; i++ {
		model.observeRemoteModes(&spotify.PlaybackState{ShuffleState: false, RepeatState: "off"}, model.shuffleEpoch, model.repeatEpoch)
		if !model.shuffle {
			t.Fatal("fresh stale shuffle observation reconciled too early")
		}
	}
	model.observeRemoteModes(&spotify.PlaybackState{ShuffleState: false, RepeatState: "off"}, model.shuffleEpoch, model.repeatEpoch)
	if model.shuffle || model.shuffleAwaitingConfirmation {
		t.Fatal("bounded shuffle reconciliation did not accept authoritative remote state")
	}

	repeatCmd := model.toggleRepeat()
	repeatPollEpoch := model.repeatEpoch
	repeatResult := repeatCmd().(actionResultMsg)
	model.finishRepeatCommand(repeatResult)
	model.observeRemoteModes(&spotify.PlaybackState{ShuffleState: false, RepeatState: "off"}, model.shuffleEpoch, repeatPollEpoch)
	if model.repeatMode != "context" {
		t.Fatal("poll started during repeat request overwrote newer intent")
	}
}
