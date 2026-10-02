package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/Chavao/rukia-player/internal/spotify"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
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

func TestRenderTrackTableHighlightsEntireSelectedRow(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })

	tracks := []spotify.Track{
		{Name: "First", Artist: "Artist", DurationMs: 180000},
		{Name: "Second", Artist: "Artist", DurationMs: 180000},
		{Name: "Third", Artist: "Artist", DurationMs: 180000},
	}
	for _, tc := range []struct {
		name       string
		cursor     int
		playing    int
		height     int
		selectedAt int
	}{
		{name: "selected", cursor: 0, playing: -1, height: 3, selectedAt: 0},
		{name: "selected and playing", cursor: 0, playing: 0, height: 3, selectedAt: 0},
		{name: "scrolled selection", cursor: 2, playing: 0, height: 2, selectedAt: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := strings.Split(RenderTrackTable(tracks, tc.cursor, tc.playing, 80, tc.height), "\n")
			for i, row := range rows {
				background := strings.Contains(row, "48;2;22;32;50")
				if background != (i == tc.selectedAt) {
					t.Errorf("row %d has selected background %v, want %v", i, background, i == tc.selectedAt)
				}
			}
			row := rows[tc.selectedAt]
			plain := ansi.Strip(row)
			if lipgloss.Width(plain) != 80 {
				t.Errorf("selected row width = %d, want 80", lipgloss.Width(plain))
			}
			if !strings.Contains(row, "38;2;0;229;255") || !strings.Contains(row, "\x1b[1;") {
				t.Errorf("selected row must use bold cyan text: %q", row)
			}
			// A single span keeps inter-column spaces and the duration/checkmark
			// on the same background as the artist and title.
			if strings.Count(row, "\x1b[0m") != 1 || !strings.HasSuffix(row, "\x1b[0m") {
				t.Errorf("selected row has interrupted styling: %q", row)
			}
			if strings.Contains(plain, "✓") != (tc.cursor == tc.playing) {
				t.Errorf("selected row playing marker is incorrect: %q", plain)
			}
		})
	}
}

func TestRenderBottomBarShuffleObservation(t *testing.T) {
	track := &spotify.Track{Name: "Current Song", Artist: "Artist", DurationMs: 180000}
	for _, tc := range []struct {
		name    string
		shuffle bool
		known   bool
		badge   string
	}{
		{name: "unknown off", badge: "[?]"},
		{name: "unknown on", shuffle: true, badge: "[?]"},
		{name: "known off", known: true, badge: "[ ]"},
		{name: "known on", known: true, shuffle: true, badge: "[S]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bar := ansi.Strip(RenderBottomBar(track, 60000, 75, true, tc.shuffle, tc.known, "context", 100))
			if !strings.Contains(bar, tc.badge+" [R] ▶ 1:00 / 3:00 [75%]") {
				t.Errorf("shuffle badge or playback metadata is incorrect: %q", bar)
			}
			if !strings.Contains(bar, "Artist - Current Song") {
				t.Errorf("current song metadata missing: %q", bar)
			}
		})
	}
}

func TestRenderBottomBar(t *testing.T) {
	track := &spotify.Track{
		Name:       "Alpha Waves",
		Artist:     "Brain Study",
		DurationMs: 180000,
	}

	bottom := RenderBottomBar(track, 60000, 100, true, true, true, "context", 80)
	if !strings.Contains(bottom, "Brain Study - Alpha Waves") {
		t.Errorf("bottom bar missing track name: %s", bottom)
	}
	if !strings.Contains(bottom, "[S]") {
		t.Errorf("bottom bar missing shuffle indicator: %s", bottom)
	}
	if !strings.Contains(bottom, "[R]") {
		t.Errorf("bottom bar missing repeat indicator: %s", bottom)
	}
	if !strings.Contains(bottom, "▶") {
		t.Errorf("bottom bar missing play indicator: %s", bottom)
	}
	if !strings.Contains(bottom, "[100%]") {
		t.Errorf("bottom bar missing volume percentage: %s", bottom)
	}
	if !strings.Contains(bottom, "1:00 / 3:00") {
		t.Errorf("bottom bar missing duration times: %s", bottom)
	}

	// Paused and no shuffle
	bottomPaused := RenderBottomBar(track, 60000, 100, false, false, true, "off", 80)
	if !strings.Contains(bottomPaused, "⏸") {
		t.Errorf("bottom bar missing pause indicator when stopped: %s", bottomPaused)
	}
	if strings.Contains(bottomPaused, "[S]") {
		t.Errorf("bottom bar should not display active shuffle indicator: %s", bottomPaused)
	}
}

func TestRenderBottomBarWithError(t *testing.T) {
	track := &spotify.Track{
		Name:       "Alpha Waves",
		Artist:     "Brain Study",
		DurationMs: 180000,
	}

	bottom := RenderBottomBar(track, 60000, 100, true, false, true, "context", 80, "failed to change volume")
	if !strings.Contains(bottom, "failed to change volume") {
		t.Errorf("expected bottom bar to render error message, got: %s", bottom)
	}
	if !strings.Contains(bottom, "⚠") {
		t.Errorf("expected bottom bar to contain warning icon, got: %s", bottom)
	}
}

func TestRenderHeaderWidths(t *testing.T) {
	pl := &spotify.Playlist{
		Name:          "Focus Beats",
		TotalTracks:   12,
		TotalDuration: 36 * time.Minute,
	}

	widths := []int{0, 1, 5, 10, 20, 29, 30, 40, 60, 80, 120, 200}
	for _, w := range widths {
		out := RenderHeader("Diego", pl, w)
		if w == 0 && out != "" {
			t.Errorf("expected empty string for width 0, got %q", out)
		}
	}
}
