package util

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		ms       int
		expected string
	}{
		{ms: 0, expected: "0:00"},
		{ms: 89000, expected: "1:29"},
		{ms: 180000, expected: "3:00"},
		{ms: 3661000, expected: "1:01:01"},
		{ms: -500, expected: "0:00"},
	}

	for _, tt := range tests {
		got := FormatDuration(tt.ms)
		if got != tt.expected {
			t.Errorf("FormatDuration(%d) = %s; want %s", tt.ms, got, tt.expected)
		}
	}
}

func TestFormatPlaylistDuration(t *testing.T) {
	d := 1*time.Hour + 23*time.Minute + 36*time.Second
	expected := "1h 23m 36s"
	if got := FormatPlaylistDuration(d); got != expected {
		t.Errorf("FormatPlaylistDuration() = %s; want %s", got, expected)
	}

	d2 := 45*time.Minute + 20*time.Second
	expected2 := "45m 20s"
	if got := FormatPlaylistDuration(d2); got != expected2 {
		t.Errorf("FormatPlaylistDuration() = %s; want %s", got, expected2)
	}
}

func TestRenderProgressBar(t *testing.T) {
	bar := RenderProgressBar(50, 100, 10)
	if len([]rune(bar)) != 10 {
		t.Errorf("expected 10 characters, got %d", len([]rune(bar)))
	}

	emptyBar := RenderProgressBar(0, 100, 5)
	if emptyBar != "─────" {
		t.Errorf("expected '─────', got %q", emptyBar)
	}

	fullBar := RenderProgressBar(100, 100, 5)
	if fullBar != "━━━━━" {
		t.Errorf("expected '━━━━━', got %q", fullBar)
	}
}
