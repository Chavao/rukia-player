package mpris

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/Chavao/rukia-player/internal/spotify"
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
)

func TestFormatStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"playing", "Playing"},
		{"Playing", "Playing"},
		{"paused", "Paused"},
		{"Paused", "Paused"},
		{"stopped", "Stopped"},
		{"unknown", "Stopped"},
		{"", "Stopped"},
	}
	for _, tc := range tests {
		if got := formatStatus(tc.input); got != tc.expected {
			t.Errorf("formatStatus(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestFormatLoopStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"off", "None"},
		{"context", "Playlist"},
		{"track", "Track"},
		{"", "None"},
		{"random", "None"},
	}
	for _, tc := range tests {
		if got := formatLoopStatus(tc.input); got != tc.expected {
			t.Errorf("formatLoopStatus(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestMakeMetadata(t *testing.T) {
	// Nil track
	nilMeta := makeMetadata(nil)
	if got := nilMeta["mpris:trackid"]; got != dbus.ObjectPath("/org/mpris/MediaPlayer2/TrackList/NoTrack") {
		t.Errorf("unexpected trackid for nil track: %v", got)
	}
	if got := nilMeta["xesam:title"]; got != "" {
		t.Errorf("unexpected title for nil track: %v", got)
	}

	// Populated track
	track := &spotify.Track{
		ID:               "track123_abc",
		Name:             "Song Title",
		Artist:           "Artist Name",
		Album:            "Album Name",
		DurationMs:       180000,
		PlaylistPosition: 2,
	}
	meta := makeMetadata(track)
	if got := meta["mpris:trackid"]; got != dbus.ObjectPath("/org/mpris/MediaPlayer2/Track/track123_abc") {
		t.Errorf("unexpected trackid: %v", got)
	}
	if got := meta["xesam:title"]; got != "Song Title" {
		t.Errorf("unexpected title: %v", got)
	}
	if got, ok := meta["xesam:artist"].([]string); !ok || len(got) != 1 || got[0] != "Artist Name" {
		t.Errorf("unexpected artist: %v", meta["xesam:artist"])
	}
	if got := meta["xesam:album"]; got != "Album Name" {
		t.Errorf("unexpected album: %v", got)
	}
	if got := meta["mpris:length"]; got != int64(180000000) {
		t.Errorf("unexpected length: %v", got)
	}
	if got := meta["xesam:trackNumber"]; got != int32(3) {
		t.Errorf("unexpected trackNumber: %v", got)
	}
	if got := meta["xesam:url"]; got != "https://open.spotify.com/track/track123_abc" {
		t.Errorf("unexpected url: %v", got)
	}
}

func TestSanitizeDbusPath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"abc123_XYZ", "abc123_XYZ"},
		{"abc-123.xyz:456", "abc123xyz456"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := sanitizeDbusPath(tc.input); got != tc.expected {
			t.Errorf("sanitizeDbusPath(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestServerDispatchesEvents(t *testing.T) {
	s := &Server{}
	var received []any
	s.SetSender(func(msg any) {
		received = append(received, msg)
	})

	root := &rootInterface{server: s}
	play := &playerInterface{server: s}

	_ = play.PlayPause()
	_ = play.Next()
	_ = play.Previous()
	_ = play.Pause()
	_ = play.Play()
	_ = play.Stop()
	_ = root.Quit()

	expectedTypes := []any{
		TogglePlayPauseMsg{},
		NextMsg{},
		PreviousMsg{},
		PauseMsg{},
		PlayMsg{},
		StopMsg{},
		QuitMsg{},
	}

	if len(received) != len(expectedTypes) {
		t.Fatalf("expected %d messages, got %d", len(expectedTypes), len(received))
	}

	for i, exp := range expectedTypes {
		if reflect.TypeOf(received[i]) != reflect.TypeOf(exp) {
			t.Errorf("msg %d: expected type %T, got %T", i, exp, received[i])
		}
	}
}

func TestServerVolumeChange(t *testing.T) {
	s := &Server{}
	var gotVol int
	s.SetSender(func(msg any) {
		if vm, ok := msg.(VolumeMsg); ok {
			gotVol = vm.Percent
		}
	})

	err := s.onVolumeChange(&prop.Change{Value: 0.75})
	if err != nil {
		t.Fatalf("onVolumeChange returned error: %v", err)
	}
	if gotVol != 75 {
		t.Errorf("expected volume 75, got %d", gotVol)
	}

	// Invalid type
	err = s.onVolumeChange(&prop.Change{Value: "invalid"})
	if err != prop.ErrInvalidArg {
		t.Errorf("expected ErrInvalidArg, got %v", err)
	}
}

func TestNewServerDBusIntegration(t *testing.T) {
	conn, err := dbus.SessionBus()
	if err != nil {
		t.Skip("D-Bus session bus not available, skipping integration test")
	}
	_ = conn

	server, err := NewServer()
	if err != nil {
		t.Skipf("cannot acquire MPRIS name on D-Bus: %v", err)
	}
	defer server.Close()

	track := &spotify.Track{
		ID:         "test123",
		Name:       "Test Track",
		Artist:     "Test Artist",
		Album:      "Test Album",
		DurationMs: 120000,
	}

	// Verify state updates don't panic or fail
	server.UpdatePlaybackState("playing", track, 80, true, "context", 10000)
	server.UpdateStatus("paused")
	server.UpdateVolume(50)
	server.UpdateTrack(track)
	server.UpdateShuffle(false)
	server.UpdateRepeat("off")
}

func TestPlayerctlCommandIntegration(t *testing.T) {
	conn, err := dbus.SessionBus()
	if err != nil {
		t.Skip("D-Bus session bus not available, skipping integration test")
	}
	_ = conn

	server, err := NewServer()
	if err != nil {
		t.Skipf("cannot acquire MPRIS name on D-Bus: %v", err)
	}
	defer server.Close()

	ch := make(chan any, 10)
	server.SetSender(func(msg any) {
		ch <- msg
	})

	track := &spotify.Track{
		ID:         "track999",
		Name:       "Hello World",
		Artist:     "Go Artist",
		Album:      "Go Album",
		DurationMs: 180000,
	}
	server.UpdatePlaybackState("playing", track, 100, false, "off", 0)

	execPath, err := exec.LookPath("playerctl")
	if err != nil {
		t.Skip("playerctl not found in PATH")
	}
	_ = execPath

	// 1. playerctl -p rukia-player status
	out, err := exec.Command("playerctl", "-p", "rukia-player", "status").CombinedOutput()
	if err != nil {
		t.Fatalf("playerctl status failed: %v, out: %s", err, string(out))
	}
	if strings.TrimSpace(string(out)) != "Playing" {
		t.Errorf("expected Playing, got %s", string(out))
	}

	// 2. playerctl -p rukia-player metadata title
	out, err = exec.Command("playerctl", "-p", "rukia-player", "metadata", "title").CombinedOutput()
	if err != nil {
		t.Fatalf("playerctl metadata title failed: %v, out: %s", err, string(out))
	}
	if strings.TrimSpace(string(out)) != "Hello World" {
		t.Errorf("expected Hello World, got %s", string(out))
	}

	// 3. playerctl -p rukia-player play-pause
	if err := exec.Command("playerctl", "-p", "rukia-player", "play-pause").Run(); err != nil {
		t.Fatalf("playerctl play-pause failed: %v", err)
	}
	select {
	case msg := <-ch:
		if _, ok := msg.(TogglePlayPauseMsg); !ok {
			t.Errorf("expected TogglePlayPauseMsg, got %T", msg)
		}
	default:
		t.Error("expected message from play-pause")
	}

	// 4. playerctl -p rukia-player next
	if err := exec.Command("playerctl", "-p", "rukia-player", "next").Run(); err != nil {
		t.Fatalf("playerctl next failed: %v", err)
	}
	select {
	case msg := <-ch:
		if _, ok := msg.(NextMsg); !ok {
			t.Errorf("expected NextMsg, got %T", msg)
		}
	default:
		t.Error("expected message from next")
	}

	// 5. playerctl -p rukia-player previous
	if err := exec.Command("playerctl", "-p", "rukia-player", "previous").Run(); err != nil {
		t.Fatalf("playerctl previous failed: %v", err)
	}
	select {
	case msg := <-ch:
		if _, ok := msg.(PreviousMsg); !ok {
			t.Errorf("expected PreviousMsg, got %T", msg)
		}
	default:
		t.Error("expected message from previous")
	}

	// 6. playerctl -p rukia play-pause (using alias)
	if err := exec.Command("playerctl", "-p", "rukia", "play-pause").Run(); err != nil {
		t.Fatalf("playerctl -p rukia play-pause failed: %v", err)
	}
	select {
	case msg := <-ch:
		if _, ok := msg.(TogglePlayPauseMsg); !ok {
			t.Errorf("expected TogglePlayPauseMsg, got %T", msg)
		}
	default:
		t.Error("expected message from play-pause alias")
	}
}
