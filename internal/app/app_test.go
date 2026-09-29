package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Chavao/rukia-player/internal/spotify"
)

type deviceListerFunc func(context.Context) ([]spotify.Device, error)

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
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	calls := 0
	client := deviceListerFunc(func(ctx context.Context) ([]spotify.Device, error) {
		calls++
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
