package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/Chavao/rukia-player/internal/spotify"
)

func TestRenderHeader(t *testing.T) {
	pl := &spotify.Playlist{
		Name:          "Focus Beats",
		TotalTracks:   12,
		TotalDuration: 36 * time.Minute,
	}

	header := RenderHeader("Diego Chavão", pl, 80)
	if !strings.Contains(header, "Library of Diego Chavão") {
		t.Errorf("header missing user library label: %s", header)
	}
	if !strings.Contains(header, "Focus Beats") {
		t.Errorf("header missing playlist name: %s", header)
	}
	if !strings.Contains(header, "12 tracks") {
		t.Errorf("header missing track count: %s", header)
	}
}

func TestRenderTrackTable(t *testing.T) {
	tracks := []spotify.Track{
		{
			ID:         "1",
			Name:       "Alpha Waves",
			Artist:     "Brain Study",
			Album:      "Focus 40Hz",
			DurationMs: 180000,
		},
		{
			ID:         "2",
			Name:       "Beta Waves",
			Artist:     "Deep Mind",
			Album:      "Meditation",
			DurationMs: 240000,
		},
	}

	table := RenderTrackTable(tracks, 0, 0, 80, 10)
	if !strings.Contains(table, "Alpha Waves") {
		t.Error("table missing first track title")
	}
	if !strings.Contains(table, "Brain Study") {
		t.Error("table missing first track artist")
	}
	if !strings.Contains(table, "✓") {
		t.Error("playing track missing checkmark")
	}
}

func TestRenderBottomBar(t *testing.T) {
	track := &spotify.Track{
		Name:       "Alpha Waves",
		Artist:     "Brain Study",
		DurationMs: 180000,
	}

	bottom := RenderBottomBar(track, 60000, 100, true, "context", 80)
	if !strings.Contains(bottom, "Brain Study - Alpha Waves") {
		t.Errorf("bottom bar missing track name: %s", bottom)
	}
	if !strings.Contains(bottom, "[R]") {
		t.Errorf("bottom bar missing repeat indicator: %s", bottom)
	}
	if !strings.Contains(bottom, "[100%]") {
		t.Errorf("bottom bar missing volume percentage: %s", bottom)
	}
	if !strings.Contains(bottom, "1:00 / 3:00") {
		t.Errorf("bottom bar missing duration times: %s", bottom)
	}
}
