package spotify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

func TestAPIErrorStructure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"status":429,"message":"API rate limit exceeded"}}`))
	}))
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	err = checkError(resp)
	if err == nil {
		t.Fatal("expected error from checkError")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}

	if apiErr.StatusCode != 429 {
		t.Errorf("expected status code 429, got %d", apiErr.StatusCode)
	}
	if apiErr.Message != "API rate limit exceeded" {
		t.Errorf("expected message 'API rate limit exceeded', got %s", apiErr.Message)
	}
	if apiErr.RetryAfter != 5*time.Second {
		t.Errorf("expected RetryAfter 5s, got %v", apiErr.RetryAfter)
	}
}

func TestClientGetWithRetryAfter429(t *testing.T) {
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"status":429,"message":"rate limit"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"diego","display_name":"Diego Chavão"}`))
	}))
	defer ts.Close()

	c := &Client{httpClient: ts.Client(), apiBase: ts.URL}
	user, err := c.GetCurrentUser(context.Background())
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if user.DisplayName != "Diego Chavão" {
		t.Errorf("expected 'Diego Chavão', got %s", user.DisplayName)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestClientGetWithTransient503(t *testing.T) {
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"devices":[{"id":"dev1","name":"rukia"}]}`))
	}))
	defer ts.Close()

	c := &Client{httpClient: ts.Client(), apiBase: ts.URL}
	devices, err := c.GetDevices(context.Background())
	if err != nil {
		t.Fatalf("expected 503 retry to succeed, got %v", err)
	}
	if len(devices) != 1 || devices[0].Name != "rukia" {
		t.Errorf("unexpected devices: %+v", devices)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

