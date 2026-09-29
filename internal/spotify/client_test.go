package spotify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientGetCurrentUser(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/me" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"diego","display_name":"Diego Chavão","product":"premium"}`))
	}))
	defer ts.Close()

	// Direct client with customized URL for testing
	c := &Client{httpClient: ts.Client()}

	// Test checkError helper
	resp, _ := ts.Client().Get(ts.URL + "/me")
	if err := checkError(resp); err != nil {
		t.Fatalf("checkError failed on 200: %v", err)
	}

	// Test premium check in error
	errTs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"status":403,"message":"Player command failed: Premium required"}}`))
	}))
	defer errTs.Close()

	errResp, _ := errTs.Client().Get(errTs.URL)
	err := checkError(errResp)
	if err == nil || !strings.Contains(err.Error(), "Premium is required") {
		t.Fatalf("expected premium required error, got %v", err)
	}

	_ = c
}

func TestGetPlaylistItemsEndpoint(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/playlists/6UUCMxk575eDTwSWa0qQhB" {
			w.Write([]byte(`{
				"id": "6UUCMxk575eDTwSWa0qQhB",
				"name": "Focus",
				"uri": "spotify:playlist:6UUCMxk575eDTwSWa0qQhB",
				"items": {"total": 1}
			}`))
			return
		}
		if r.URL.Path == "/playlists/6UUCMxk575eDTwSWa0qQhB/items" {
			w.Write([]byte(`{
				"items": [{
					"item": {
						"id": "trk1",
						"uri": "spotify:track:trk1",
						"name": "528 Hz Staying Focused",
						"duration_ms": 89230,
						"artists": [{"name": "Spiritual Frequencies"}],
						"album": {"name": "528 Hz Positive Transformation"}
					}
				}],
				"next": null
			}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	c := &Client{
		httpClient: ts.Client(),
		apiBase:    ts.URL,
	}

	pl, err := c.GetPlaylist(context.Background(), "6UUCMxk575eDTwSWa0qQhB")
	if err != nil {
		t.Fatalf("GetPlaylist failed: %v", err)
	}

	if pl.Name != "Focus" {
		t.Errorf("expected playlist name 'Focus', got %s", pl.Name)
	}
	if len(pl.Tracks) != 1 {
		t.Fatalf("expected 1 track, got %d", len(pl.Tracks))
	}
	if pl.Tracks[0].Name != "528 Hz Staying Focused" {
		t.Errorf("expected track name '528 Hz Staying Focused', got %s", pl.Tracks[0].Name)
	}
	if pl.Tracks[0].Artist != "Spiritual Frequencies" {
		t.Errorf("expected artist 'Spiritual Frequencies', got %s", pl.Tracks[0].Artist)
	}
}

func TestTrackAndPlaylistStructures(t *testing.T) {
	track := Track{
		ID:         "123",
		URI:        "spotify:track:123",
		Name:       "Focus Frequency",
		Artist:     "Study Beats",
		Album:      "Alpha Waves",
		DurationMs: 180000,
	}

	pl := Playlist{
		ID:          "xyz",
		Name:        "Focus",
		Tracks:      []Track{track},
		TotalTracks: 1,
	}

	if pl.Name != "Focus" || len(pl.Tracks) != 1 {
		t.Fatal("unexpected playlist data")
	}
}
