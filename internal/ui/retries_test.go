package ui

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Chavao/rukia-player/internal/spotify"
)

type transientNetworkError struct{}

func (transientNetworkError) Error() string   { return "temporary connection failure" }
func (transientNetworkError) Timeout() bool   { return false }
func (transientNetworkError) Temporary() bool { return true }

type permanentNetworkError struct{ transientNetworkError }

func (permanentNetworkError) Temporary() bool { return false }

func TestPlaybackControlRetryClassificationAndRequestCounts(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		attempts int
	}{
		{"transient network", transientNetworkError{}, 3},
		{"permanent network", permanentNetworkError{}, 1},
		{"wrapped network", fmt.Errorf("transport: %w", transientNetworkError{}), 3},
		{"500", &spotify.APIError{StatusCode: 500}, 3},
		{"502", &spotify.APIError{StatusCode: 502}, 3},
		{"503", &spotify.APIError{StatusCode: 503}, 3},
		{"504", &spotify.APIError{StatusCode: 504}, 3},
		{"503 with retry directive", &spotify.APIError{StatusCode: 503, RetryAfter: time.Minute}, 1},
		{"501", &spotify.APIError{StatusCode: 501}, 1},
		{"401", &spotify.APIError{StatusCode: 401}, 1},
		{"403", &spotify.APIError{StatusCode: 403}, 1},
		{"404", &spotify.APIError{StatusCode: 404}, 1},
		{"429", &spotify.APIError{StatusCode: 429, RetryAfter: time.Minute}, 1},
		{"canceled", context.Canceled, 1},
		{"deadline", context.DeadlineExceeded, 1},
		{"wrapped cancellation", fmt.Errorf("transport: %w", context.Canceled), 1},
		{"unknown", errors.New("temporary sounding error"), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, playing := range []bool{false, true} {
				calls := 0
				control := func(context.Context, string) error { calls++; return tc.err }
				model := NewModel(&mockSpotifyController{pauseFunc: control, resumeFunc: control}, nil, nil, nil, "")
				result := model.togglePlayPauseCmd(playing)().(playbackChangedMsg)
				if calls != tc.attempts || result.err != tc.err {
					t.Fatalf("playing=%v calls=%d err=%v, want %d %v", playing, calls, result.err, tc.attempts, tc.err)
				}
			}
		})
	}
}

func TestPlaybackRetryBackoffStopsOnParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	model := NewModel(&mockSpotifyController{pauseFunc: func(context.Context, string) error {
		calls++
		cancel() // Deterministically cancel before the retry backoff begins.
		return transientNetworkError{}
	}}, nil, nil, nil, "")
	model.SetWarningChannel(ctx, nil)
	result := model.togglePlayPauseCmd(false)().(playbackChangedMsg)
	if calls != 1 || !errors.Is(result.err, context.Canceled) {
		t.Fatalf("calls=%d err=%v", calls, result.err)
	}
}

func TestPlaybackIntentSupersededDuringRetryBackoffDoesNotSendAnotherRequest(t *testing.T) {
	waiting, release := make(chan struct{}), make(chan struct{})
	model := NewModel(nil, nil, nil, nil, "")
	calls := 0
	failure := transientNetworkError{}
	done := make(chan error, 1)
	go func() {
		done <- retryPlaybackControl(context.Background(), func() error { calls++; return failure }, func() bool { return model.playbackVersion.Load() > 0 }, func(context.Context, time.Duration) error {
			close(waiting)
			<-release
			return nil
		})
	}()
	<-waiting // The previous request and its supersession check have finished.
	model.playbackVersion.Add(1)
	close(release)
	err := <-done
	if calls != 1 || err != failure {
		t.Fatalf("calls=%d err=%v; stale request retried after newer intent", calls, err)
	}
}
