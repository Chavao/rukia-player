package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
	c := &Client{
		httpClient: ts.Client(),
		apiBase:    ts.URL,
	}

	user, err := c.GetCurrentUser(context.Background())
	if err != nil {
		t.Fatalf("GetCurrentUser failed: %v", err)
	}
	if user.ID != "diego" {
		t.Errorf("expected user ID 'diego', got %s", user.ID)
	}
	if user.DisplayName != "Diego Chavão" {
		t.Errorf("expected display name 'Diego Chavão', got %s", user.DisplayName)
	}
	if user.Product != "premium" {
		t.Errorf("expected product 'premium', got %s", user.Product)
	}

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
	checkErr := checkError(errResp)
	if checkErr == nil || !strings.Contains(checkErr.Error(), "Premium is required") {
		t.Fatalf("expected premium required error, got %v", checkErr)
	}
}

func TestCheckErrorRetryAfterParsing(t *testing.T) {
	for _, tc := range []struct {
		name      string
		header    string
		wantRetry time.Duration
	}{
		{"valid retry after", "120", 120 * time.Second},
		{"overflow retry after", "999999999999999999999999999999", 0},
		{"negative retry after", "-5", 0},
		{"non-numeric retry after", "invalid", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", tc.header)
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":{"status":429,"message":"too many requests"}}`))
			}))
			defer ts.Close()

			resp, _ := ts.Client().Get(ts.URL)
			err := checkError(resp)
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected APIError, got %v", err)
			}
			if apiErr.RetryAfter != tc.wantRetry {
				t.Errorf("got RetryAfter %v, want %v", apiErr.RetryAfter, tc.wantRetry)
			}
		})
	}
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

func TestGetPlaylistUnplayableTrack(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/playlists/restricted" {
			w.Write([]byte(`{
				"id": "restricted",
				"name": "Restricted Playlist",
				"items": {"total": 2}
			}`))
			return
		}
		if r.URL.Path == "/playlists/restricted/items" {
			w.Write([]byte(`{
				"items": [
					{
						"item": {
							"id": "trk-unplayable",
							"uri": "spotify:track:trk-unplayable",
							"name": "Disabled Song",
							"duration_ms": 73070,
							"is_playable": false,
							"artists": [{"name": "Artist 1"}],
							"album": {"name": "Album 1"}
						}
					},
					{
						"item": {
							"id": "trk-playable",
							"uri": "spotify:track:trk-playable",
							"name": "Playable Song",
							"duration_ms": 153083,
							"is_playable": true,
							"artists": [{"name": "Artist 2"}],
							"album": {"name": "Album 2"}
						}
					}
				],
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

	pl, err := c.GetPlaylist(context.Background(), "restricted")
	if err != nil {
		t.Fatalf("GetPlaylist failed: %v", err)
	}

	if len(pl.Tracks) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(pl.Tracks))
	}
	if pl.Tracks[0].CanPlay() {
		t.Errorf("expected track 0 to be unplayable")
	}
	if pl.Tracks[0].IsPlayable == nil || *pl.Tracks[0].IsPlayable {
		t.Errorf("expected track 0 IsPlayable to be &false")
	}
	if !pl.Tracks[1].CanPlay() {
		t.Errorf("expected track 1 to be playable")
	}
	if pl.Tracks[1].IsPlayable == nil || !*pl.Tracks[1].IsPlayable {
		t.Errorf("expected track 1 IsPlayable to be &true")
	}
}

func TestGetPlaylistDoesNotFallbackOnForbiddenItems(t *testing.T) {
	legacyCalls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/playlists/test":
			w.Write([]byte(`{"id":"test","items":{"total":1}}`))
		case "/playlists/test/items":
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":{"status":403,"message":"Forbidden"}}`))
		case "/playlists/test/tracks":
			legacyCalls++
			w.Write([]byte(`{"items":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	c := &Client{httpClient: ts.Client(), apiBase: ts.URL}
	_, err := c.GetPlaylist(context.Background(), "test")
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != http.StatusForbidden {
		t.Fatalf("expected forbidden API error, got %v", err)
	}
	if legacyCalls != 0 {
		t.Fatalf("expected no legacy fallback, got %d requests", legacyCalls)
	}
}

func TestGetPlaylistPreservesPositionsAcrossSkippedItemsAndPages(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "items"
		if legacy {
			name = "tracks"
		}
		t.Run(name, func(t *testing.T) {
			var offsets []string
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/playlists/test" {
					w.Write([]byte(`{"id":"test","items":{"total":8}}`))
					return
				}
				if legacy && r.URL.Path == "/playlists/test/items" {
					http.NotFound(w, r)
					return
				}
				if r.URL.Path != "/playlists/test/"+name {
					http.NotFound(w, r)
					return
				}
				offset := r.URL.Query().Get("offset")
				offsets = append(offsets, offset)
				switch offset {
				case "0":
					page := `{"items":[{"item":null},{"item":{"id":"duplicate"}},{"item":{"id":""}},{"item":{"id":"second"}}],"next":"next-page"}`
					if legacy {
						page = strings.ReplaceAll(page, `"item":`, `"track":`)
					}
					w.Write([]byte(page))
				case "4":
					page := `{"items":[{}, {"item":{"id":"duplicate"}},{"item":null},{"item":{"id":"fourth"}}],"next":null}`
					if legacy {
						page = strings.ReplaceAll(page, `"item":`, `"track":`)
					}
					w.Write([]byte(page))
				default:
					t.Errorf("unexpected page offset %q", offset)
					w.Write([]byte(`{"items":[],"next":null}`))
				}
			}))
			defer ts.Close()
			client := &Client{httpClient: ts.Client(), apiBase: ts.URL}
			playlist, err := client.GetPlaylist(context.Background(), "test")
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(offsets, ",") != "0,4" {
				t.Fatalf("page offsets=%v, want [0 4]", offsets)
			}
			if len(playlist.Tracks) != 4 {
				t.Fatalf("loaded tracks=%d, want 4", len(playlist.Tracks))
			}
			for index, want := range []int{1, 3, 5, 7} {
				if got := playlist.Tracks[index].PlaylistPosition; got != want {
					t.Errorf("track %d position=%d, want %d", index, got, want)
				}
			}
		})
	}
}

func TestTrackPlaylistPositionIsNotSerialized(t *testing.T) {
	data, err := json.Marshal(Track{ID: "track", PlaylistPosition: 7})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "PlaylistPosition") || strings.Contains(string(data), "playlist_position") {
		t.Fatalf("playlist position leaked into JSON: %s", data)
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

func TestClientGetRespectsValidRetryAfter(t *testing.T) {
	var attempts atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{"id":"user"}`))
	}))
	defer ts.Close()

	c := &Client{httpClient: ts.Client(), apiBase: ts.URL}
	start := time.Now()
	if _, err := c.GetCurrentUser(context.Background()); err != nil {
		t.Fatalf("GetCurrentUser failed: %v", err)
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Errorf("retried before Retry-After elapsed: %v", elapsed)
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("expected 2 requests, got %d", got)
	}
}

func TestClientGetRetryAfterExceedsContextDeadline(t *testing.T) {
	var attempts atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer ts.Close()

	c := &Client{httpClient: ts.Client(), apiBase: ts.URL}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	_, err := c.GetCurrentUser(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("expected 1 request, got %d", got)
	}
}

func TestClientGetCancellationDuringRetryAfter(t *testing.T) {
	var attempts atomic.Int32
	responded := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)
			close(responded)
			return
		}
		w.Write([]byte(`{"id":"user"}`))
	}))
	defer ts.Close()

	c := &Client{httpClient: ts.Client(), apiBase: ts.URL}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := c.GetCurrentUser(ctx)
		result <- err
	}()
	<-responded
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("expected 1 request, got %d", got)
	}
}

func TestClientGetInvalidRetryAfterUsesFallback(t *testing.T) {
	var attempts atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "invalid")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{"id":"user"}`))
	}))
	defer ts.Close()

	c := &Client{httpClient: ts.Client(), apiBase: ts.URL}
	start := time.Now()
	if _, err := c.GetCurrentUser(context.Background()); err != nil {
		t.Fatalf("GetCurrentUser failed: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Errorf("retried before fallback backoff elapsed: %v", elapsed)
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("expected 2 requests, got %d", got)
	}
}

func TestClientGetFinal429IsAPIError(t *testing.T) {
	var attempts atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"status":429,"message":"rate limit"}}`))
	}))
	defer ts.Close()

	c := &Client{httpClient: ts.Client(), apiBase: ts.URL}
	_, err := c.GetCurrentUser(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 APIError, got %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("expected 3 requests, got %d", got)
	}
}

func TestClientDoesNotRetryMutatingRequest(t *testing.T) {
	var attempts atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer ts.Close()

	c := &Client{httpClient: ts.Client(), apiBase: ts.URL}
	var apiErr *APIError
	if err := c.Pause(context.Background(), "device"); !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 APIError, got %v", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("expected 1 request, got %d", got)
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
