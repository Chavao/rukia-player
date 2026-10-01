package ui

import (
	"context"
	"time"
)

// retryPlaybackControl keeps mutating playback controls within three attempts,
// honoring cancellation and supersession both before and after a short backoff.
// The wait argument makes ordering tests independent of wall-clock timing.
func retryPlaybackControl(ctx context.Context, request func() error, superseded func() bool, wait func(context.Context, time.Duration) error) error {
	backoffs := []time.Duration{50 * time.Millisecond, 100 * time.Millisecond}
	var err error
	for attempt := 0; attempt <= len(backoffs); attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if attempt > 0 && superseded() {
			return err
		}
		err = request()
		if err == nil || !shouldRetryPlaybackControl(err) || superseded() || attempt == len(backoffs) {
			return err
		}
		if waitErr := wait(ctx, backoffs[attempt]); waitErr != nil {
			return waitErr
		}
	}
	return err
}

func waitPlaybackBackoff(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
