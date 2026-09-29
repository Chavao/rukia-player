package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

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
