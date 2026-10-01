package app

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Chavao/rukia-player/internal/spotify"
)

type deviceListerFunc func(context.Context) ([]spotify.Device, error)

type playbackStarterStub struct {
	transferErr error
	playErr     error
	calls       []string
}

func (s *playbackStarterStub) TransferPlayback(_ context.Context, device string, play bool) error {
	s.calls = append(s.calls, "transfer:"+device)
	return s.transferErr
}

func (s *playbackStarterStub) PlayPlaylist(_ context.Context, device, _ string, _ int) error {
	s.calls = append(s.calls, "play:"+device)
	return s.playErr
}

func TestStartInitialPlayback(t *testing.T) {
	origDelay := initialPlaybackDelay
	initialPlaybackDelay = 0
	defer func() { initialPlaybackDelay = origDelay }()

	transferErr := errors.New("transfer failed")
	playErr := errors.New("play failed")
	for _, tc := range []struct {
		name                 string
		device               string
		transferErr, playErr error
		wantCalls            string
		wantErr              error
	}{
		{"transfer and play", "device", nil, nil, "transfer:device,play:device", nil},
		{"transfer fails but play succeeds", "device", transferErr, nil, "transfer:device,play:device", nil},
		{"play fails", "device", nil, playErr, "transfer:device,play:device", playErr},
		{"both fail", "device", transferErr, playErr, "transfer:device,play:device", playErr},
		{"no device succeeds", "", nil, nil, "play:", nil},
		{"no device fails", "", nil, playErr, "play:", playErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &playbackStarterStub{transferErr: tc.transferErr, playErr: tc.playErr}
			err := startInitialPlayback(context.Background(), stub, tc.device, "spotify:playlist:test")
			if strings.Join(stub.calls, ",") != tc.wantCalls {
				t.Fatalf("calls=%v, want %s", stub.calls, tc.wantCalls)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error=%v, want %v", err, tc.wantErr)
			}
			if tc.transferErr != nil && tc.playErr != nil && !errors.Is(err, tc.transferErr) {
				t.Fatalf("combined error omits transfer failure: %v", err)
			}
		})
	}
}

func TestStartInitialPlaybackRespectsDelay(t *testing.T) {
	origDelay := initialPlaybackDelay
	initialPlaybackDelay = 50 * time.Millisecond
	defer func() { initialPlaybackDelay = origDelay }()

	stub := &playbackStarterStub{}
	start := time.Now()
	err := startInitialPlayback(context.Background(), stub, "dev", "spotify:playlist:test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Fatalf("expected at least 50ms delay, got %v", elapsed)
	}
}

func (f deviceListerFunc) GetDevices(ctx context.Context) ([]spotify.Device, error) {
	return f(ctx)
}

func TestPrintUsage(t *testing.T) {
	var buf bytes.Buffer
	PrintUsage(&buf)
	output := buf.String()

	if !strings.Contains(output, "rukia v") {
		t.Errorf("expected usage to contain version, got: %s", output)
	}
	if !strings.Contains(output, "Usage:") {
		t.Errorf("expected usage to contain Usage, got: %s", output)
	}
}

func TestRunHelpAndVersion(t *testing.T) {
	ctx := context.Background()

	// -help flag should exit without error
	err := Run(ctx, []string{"-help"})
	if err != nil {
		t.Errorf("expected nil error for -help, got: %v", err)
	}

	// -version flag should exit without error
	err = Run(ctx, []string{"-version"})
	if err != nil {
		t.Errorf("expected nil error for -version, got: %v", err)
	}

	// invalid flag should return error
	err = Run(ctx, []string{"--invalid-flag-xyz"})
	if err == nil {
		t.Error("expected error for unknown flag")
	}
}

func TestDiscoverDeviceRespectsBudget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	client := deviceListerFunc(func(ctx context.Context) ([]spotify.Device, error) {
		calls++
		cancel()
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if got := discoverDevice(ctx, client, "rukia", 8, 500*time.Millisecond); got != "" {
		t.Fatalf("expected no device after deadline, got %q", got)
	}
	if calls != 1 {
		t.Fatalf("expected one bounded request, got %d", calls)
	}
}

func TestDiscoverDeviceKeepsActiveFallback(t *testing.T) {
	client := deviceListerFunc(func(context.Context) ([]spotify.Device, error) {
		return []spotify.Device{{ID: "first"}, {ID: "active", IsActive: true}}, nil
	})
	if got := discoverDevice(context.Background(), client, "rukia", 2, 0); got != "active" {
		t.Fatalf("expected active fallback, got %q", got)
	}
}

func TestDiscoverDeviceUsesLatestSuccessfulFallback(t *testing.T) {
	for _, tc := range []struct {
		name      string
		second    []spotify.Device
		secondErr error
		want      string
	}{
		{"active device changed", []spotify.Device{{ID: "tablet", IsActive: true}}, nil, "tablet"},
		{"devices disappeared", nil, nil, ""},
		{"discovery failed", nil, errors.New("unavailable"), "phone"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := deviceListerFunc(func(context.Context) ([]spotify.Device, error) {
				calls++
				if calls == 1 {
					return []spotify.Device{{ID: "phone", IsActive: true}}, nil
				}
				return tc.second, tc.secondErr
			})
			if got := discoverDevice(context.Background(), client, "rukia", 2, 0); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			if calls != 2 {
				t.Fatalf("got %d calls, want 2", calls)
			}
		})
	}
}

func TestFormatStartupError(t *testing.T) {
	// Rate limit error
	apiErr := &spotify.APIError{
		StatusCode: 429,
		Message:    "rate limit exceeded",
		RetryAfter: 30 * time.Second,
	}
	err429 := formatStartupError("profile", apiErr)
	var structured *spotify.APIError
	if !errors.As(err429, &structured) || structured != apiErr {
		t.Fatal("formatted rate limit error lost the original structured error")
	}
	if !strings.Contains(err429.Error(), "Spotify rate limit reached") || !strings.Contains(err429.Error(), "30s") {
		t.Errorf("unexpected 429 error format: %v", err429)
	}

	// Timeout error
	timeoutErr := formatStartupError("playlist", context.DeadlineExceeded)
	if !strings.Contains(timeoutErr.Error(), "request timed out while connecting to Spotify") {
		t.Errorf("unexpected timeout error format: %v", timeoutErr)
	}

	// Generic error
	genErr := formatStartupError("initialization", errors.New("network failure"))
	if !strings.Contains(genErr.Error(), "initialization: network failure") {
		t.Errorf("unexpected generic error format: %v", genErr)
	}
}

func TestPrintStartupWarning(t *testing.T) {
	warning := errors.New("token was refreshed but could not be saved")
	var output bytes.Buffer
	if err := printWarning(&output, warning); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "token was refreshed but could not be saved") {
		t.Fatalf("queued warning disappeared: %q", output.String())
	}
	output.Reset()
	if err := printWarning(&output, nil); err != nil || output.Len() != 0 {
		t.Fatal(err)
	}
}
