package spotify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const spotifyAPIBase = "https://api.spotify.com/v1"

// Client is the Spotify Web API HTTP client wrapper.
type Client struct {
	httpClient *http.Client
	apiBase    string
}

// NewClient returns a new Client with the provided authenticated HTTP client.
func NewClient(httpClient *http.Client) *Client {
	return &Client{
		httpClient: httpClient,
		apiBase:    spotifyAPIBase,
	}
}

func (c *Client) endpointBase() string {
	if c.apiBase != "" {
		return c.apiBase
	}
	return spotifyAPIBase
}

// UserProfile represents a Spotify user profile.
type UserProfile struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Product     string `json:"product"` // "premium", "free", etc.
}

// Track represents an individual track in a playlist.
type Track struct {
	ID               string `json:"id"`
	URI              string `json:"uri"`
	Name             string `json:"name"`
	Artist           string `json:"artist"`
	Album            string `json:"album"`
	DurationMs       int    `json:"duration_ms"`
	IsPlayable       *bool  `json:"is_playable,omitempty"`
	PlaylistPosition int    `json:"-"`
}

// Playlist represents a Spotify playlist with all its loaded tracks.
type Playlist struct {
	ID            string `json:"id"`
	URI           string `json:"uri"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	TotalTracks   int    `json:"total_tracks"`
	TotalDuration time.Duration
	Tracks        []Track `json:"tracks"`
}

// Device represents an available Spotify Connect playback device.
type Device struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	IsActive      bool   `json:"is_active"`
	VolumePercent int    `json:"volume_percent"`
}

// PlaybackState represents current player state.
type PlaybackState struct {
	IsPlaying    bool    `json:"is_playing"`
	ProgressMs   int     `json:"progress_ms"`
	ShuffleState bool    `json:"shuffle_state"`
	RepeatState  string  `json:"repeat_state"` // "off", "context", "track"
	Item         *Track  `json:"item"`
	Device       *Device `json:"device"`
}

// GetCurrentUser fetches the logged-in user profile.
func (c *Client) GetCurrentUser(ctx context.Context) (*UserProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpointBase()+"/me", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get user profile: %w", err)
	}
	defer resp.Body.Close()

	if err := checkError(resp); err != nil {
		return nil, err
	}

	var user UserProfile
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode user profile: %w", err)
	}

	return &user, nil
}

// GetPlaylist fetches the complete playlist, including paginated tracks.
func (c *Client) GetPlaylist(ctx context.Context, playlistID string) (*Playlist, error) {
	endpoint := fmt.Sprintf("%s/playlists/%s", c.endpointBase(), playlistID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get playlist: %w", err)
	}
	defer resp.Body.Close()

	if err := checkError(resp); err != nil {
		return nil, err
	}

	var rawMeta struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		URI         string `json:"uri"`
		Items       struct {
			Total int `json:"total"`
		} `json:"items"`
		Tracks struct {
			Total int `json:"total"`
		} `json:"tracks"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&rawMeta); err != nil {
		return nil, fmt.Errorf("failed to decode playlist metadata: %w", err)
	}

	totalTracks := rawMeta.Items.Total
	if totalTracks == 0 {
		totalTracks = rawMeta.Tracks.Total
	}

	playlist := &Playlist{
		ID:          rawMeta.ID,
		Name:        rawMeta.Name,
		Description: rawMeta.Description,
		URI:         rawMeta.URI,
		TotalTracks: totalTracks,
		Tracks:      make([]Track, 0, totalTracks),
	}

	type rawTrack struct {
		ID         string `json:"id"`
		URI        string `json:"uri"`
		Name       string `json:"name"`
		DurationMs int    `json:"duration_ms"`
		IsPlayable *bool  `json:"is_playable"`
		Artists    []struct {
			Name string `json:"name"`
		} `json:"artists"`
		Album struct {
			Name string `json:"name"`
		} `json:"album"`
	}

	offset := 0
	limit := 100
	var totalDurationMs int

	for {
		// Use modern /items endpoint, with fallback to legacy /tracks if needed
		itemsEndpoint := fmt.Sprintf("%s/playlists/%s/items?limit=%d&offset=%d", c.endpointBase(), playlistID, limit, offset)
		tReq, err := http.NewRequestWithContext(ctx, http.MethodGet, itemsEndpoint, nil)
		if err != nil {
			return nil, err
		}

		tResp, err := c.do(tReq)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch tracks page: %w", err)
		}

		// A 403 from /items is a permission denial, not a signal to try /tracks.
		if tResp.StatusCode == http.StatusNotFound {
			tResp.Body.Close()
			// Fallback to legacy /tracks endpoint
			legacyEndpoint := fmt.Sprintf("%s/playlists/%s/tracks?limit=%d&offset=%d", c.endpointBase(), playlistID, limit, offset)
			tReqLegacy, lErr := http.NewRequestWithContext(ctx, http.MethodGet, legacyEndpoint, nil)
			if lErr != nil {
				return nil, lErr
			}
			tResp, err = c.do(tReqLegacy)
			if err != nil {
				return nil, fmt.Errorf("failed to fetch legacy tracks page: %w", err)
			}
		}

		if err := checkError(tResp); err != nil {
			tResp.Body.Close()
			return nil, err
		}

		var page struct {
			Items []struct {
				Item  *rawTrack `json:"item"`
				Track *rawTrack `json:"track"`
			} `json:"items"`
			Next *string `json:"next"`
		}

		if err := json.NewDecoder(tResp.Body).Decode(&page); err != nil {
			tResp.Body.Close()
			return nil, fmt.Errorf("failed to decode tracks page: %w", err)
		}
		tResp.Body.Close()

		for itemIndex, item := range page.Items {
			trackObj := item.Item
			if trackObj == nil {
				trackObj = item.Track
			}
			if trackObj == nil || trackObj.ID == "" || (trackObj.IsPlayable != nil && !*trackObj.IsPlayable) {
				continue
			}

			artistNames := make([]string, len(trackObj.Artists))
			for i, a := range trackObj.Artists {
				artistNames[i] = a.Name
			}

			t := Track{
				ID:               trackObj.ID,
				URI:              trackObj.URI,
				Name:             trackObj.Name,
				Artist:           strings.Join(artistNames, ", "),
				Album:            trackObj.Album.Name,
				DurationMs:       trackObj.DurationMs,
				IsPlayable:       trackObj.IsPlayable,
				PlaylistPosition: offset + itemIndex,
			}
			totalDurationMs += t.DurationMs
			playlist.Tracks = append(playlist.Tracks, t)
		}

		if page.Next == nil || len(page.Items) == 0 {
			break
		}
		offset += len(page.Items)
	}

	playlist.TotalTracks = len(playlist.Tracks)
	playlist.TotalDuration = time.Duration(totalDurationMs) * time.Millisecond
	return playlist, nil
}

// GetDevices returns all currently connected Spotify playback devices.
func (c *Client) GetDevices(ctx context.Context) ([]Device, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpointBase()+"/me/player/devices", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get devices: %w", err)
	}
	defer resp.Body.Close()

	if err := checkError(resp); err != nil {
		return nil, err
	}

	var data struct {
		Devices []Device `json:"devices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode devices: %w", err)
	}

	return data.Devices, nil
}

// TransferPlayback transfers playback to the specified device.
func (c *Client) TransferPlayback(ctx context.Context, deviceID string, play bool) error {
	payload := map[string]any{
		"device_ids": []string{deviceID},
		"play":       play,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.endpointBase()+"/me/player", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("failed to transfer playback: %w", err)
	}
	defer resp.Body.Close()

	return checkError(resp)
}

// PlayPlaylist starts playing a playlist context, optionally at a specific track offset.
func (c *Client) PlayPlaylist(ctx context.Context, deviceID string, playlistURI string, trackOffset int) error {
	endpoint := c.endpointBase() + "/me/player/play"
	if deviceID != "" {
		endpoint += "?device_id=" + url.QueryEscape(deviceID)
	}

	payload := map[string]any{
		"context_uri": playlistURI,
		"offset": map[string]int{
			"position": trackOffset,
		},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("failed to start playlist playback: %w", err)
	}
	defer resp.Body.Close()

	return checkError(resp)
}

// Resume resumes playback on the specified or active device.
func (c *Client) Resume(ctx context.Context, deviceID string) error {
	endpoint := c.endpointBase() + "/me/player/play"
	if deviceID != "" {
		endpoint += "?device_id=" + url.QueryEscape(deviceID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, nil)
	if err != nil {
		return err
	}

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("failed to resume playback: %w", err)
	}
	defer resp.Body.Close()

	return checkError(resp)
}

// Pause pauses playback.
func (c *Client) Pause(ctx context.Context, deviceID string) error {
	endpoint := c.endpointBase() + "/me/player/pause"
	if deviceID != "" {
		endpoint += "?device_id=" + url.QueryEscape(deviceID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, nil)
	if err != nil {
		return err
	}

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("failed to pause playback: %w", err)
	}
	defer resp.Body.Close()

	return checkError(resp)
}

// SetVolume sets the playback volume percentage (0-100).
func (c *Client) SetVolume(ctx context.Context, deviceID string, volumePercent int) error {
	if volumePercent < 0 {
		volumePercent = 0
	} else if volumePercent > 100 {
		volumePercent = 100
	}

	endpoint := fmt.Sprintf("%s/me/player/volume?volume_percent=%d", c.endpointBase(), volumePercent)
	if deviceID != "" {
		endpoint += "&device_id=" + url.QueryEscape(deviceID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, nil)
	if err != nil {
		return err
	}

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("failed to set volume: %w", err)
	}
	defer resp.Body.Close()

	return checkError(resp)
}

// SetShuffle toggles shuffle state.
func (c *Client) SetShuffle(ctx context.Context, deviceID string, state bool) error {
	endpoint := fmt.Sprintf("%s/me/player/shuffle?state=%t", c.endpointBase(), state)
	if deviceID != "" {
		endpoint += "&device_id=" + url.QueryEscape(deviceID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, nil)
	if err != nil {
		return err
	}

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("failed to set shuffle: %w", err)
	}
	defer resp.Body.Close()

	return checkError(resp)
}

// SetRepeat sets repeat mode: "off", "context", or "track".
func (c *Client) SetRepeat(ctx context.Context, deviceID string, state string) error {
	endpoint := fmt.Sprintf("%s/me/player/repeat?state=%s", c.endpointBase(), url.QueryEscape(state))
	if deviceID != "" {
		endpoint += "&device_id=" + url.QueryEscape(deviceID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, nil)
	if err != nil {
		return err
	}

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("failed to set repeat: %w", err)
	}
	defer resp.Body.Close()

	return checkError(resp)
}

// GetPlaybackState returns the active playback state.
func (c *Client) GetPlaybackState(ctx context.Context) (*PlaybackState, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpointBase()+"/me/player", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get playback state: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		// Nothing is currently playing
		return nil, nil
	}

	if err := checkError(resp); err != nil {
		return nil, err
	}

	var raw struct {
		IsPlaying    bool    `json:"is_playing"`
		ProgressMs   int     `json:"progress_ms"`
		ShuffleState bool    `json:"shuffle_state"`
		RepeatState  string  `json:"repeat_state"`
		Device       *Device `json:"device"`
		Item         *struct {
			ID         string `json:"id"`
			URI        string `json:"uri"`
			Name       string `json:"name"`
			DurationMs int    `json:"duration_ms"`
			IsPlayable *bool  `json:"is_playable"`
			Artists    []struct {
				Name string `json:"name"`
			} `json:"artists"`
			Album struct {
				Name string `json:"name"`
			} `json:"album"`
		} `json:"item"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to decode playback state: %w", err)
	}

	state := &PlaybackState{
		IsPlaying:    raw.IsPlaying,
		ProgressMs:   raw.ProgressMs,
		ShuffleState: raw.ShuffleState,
		RepeatState:  raw.RepeatState,
		Device:       raw.Device,
	}

	if raw.Item != nil {
		artistNames := make([]string, len(raw.Item.Artists))
		for i, a := range raw.Item.Artists {
			artistNames[i] = a.Name
		}
		state.Item = &Track{
			ID:         raw.Item.ID,
			URI:        raw.Item.URI,
			Name:       raw.Item.Name,
			Artist:     strings.Join(artistNames, ", "),
			Album:      raw.Item.Album.Name,
			DurationMs: raw.Item.DurationMs,
			IsPlayable: raw.Item.IsPlayable,
		}
	}

	return state, nil
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet {
		return c.httpClient.Do(req)
	}

	maxAttempts := 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		resp, err := c.httpClient.Do(req)
		if err != nil {
			if req.Context().Err() != nil || attempt == maxAttempts-1 {
				return nil, err
			}
			backoff := time.Duration(100*(1<<attempt)) * time.Millisecond
			select {
			case <-time.After(backoff):
				continue
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			var retryAfter time.Duration
			if h := resp.Header.Get("Retry-After"); h != "" {
				if sec, parseErr := strconv.ParseInt(h, 10, 64); parseErr == nil && sec > 0 && sec <= (1<<63-1)/int64(time.Second) {
					retryAfter = time.Duration(sec) * time.Second
				}
			}
			if retryAfter == 0 {
				retryAfter = time.Duration(200*(1<<attempt)) * time.Millisecond
			}

			if attempt == maxAttempts-1 {
				return resp, nil
			}

			resp.Body.Close()
			timer := time.NewTimer(retryAfter)
			select {
			case <-timer.C:
				continue
			case <-req.Context().Done():
				timer.Stop()
				return nil, req.Context().Err()
			}
		}

		if resp.StatusCode == http.StatusBadGateway ||
			resp.StatusCode == http.StatusServiceUnavailable ||
			resp.StatusCode == http.StatusGatewayTimeout {
			if attempt == maxAttempts-1 {
				return resp, nil
			}
			resp.Body.Close()
			backoff := time.Duration(150*(1<<attempt)) * time.Millisecond
			select {
			case <-time.After(backoff):
				continue
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		}

		return resp, nil
	}

	return c.httpClient.Do(req)
}
