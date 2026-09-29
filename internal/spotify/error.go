package spotify

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// APIError represents a structured error returned by the Spotify Web API.
type APIError struct {
	StatusCode int
	Message    string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("spotify API error (%d): %s (retry after %v)", e.StatusCode, e.Message, e.RetryAfter)
	}
	return fmt.Sprintf("spotify API error (%d): %s", e.StatusCode, e.Message)
}

func checkError(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	var retryAfter time.Duration
	if retryHeader := resp.Header.Get("Retry-After"); retryHeader != "" {
		if sec, err := strconv.Atoi(retryHeader); err == nil && sec > 0 {
			retryAfter = time.Duration(sec) * time.Second
		}
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	var apiErr struct {
		Error struct {
			Status  int    `json:"status"`
			Message string `json:"message"`
		} `json:"error"`
	}

	message := strings.TrimSpace(string(bodyBytes))
	statusCode := resp.StatusCode
	if err := json.Unmarshal(bodyBytes, &apiErr); err == nil && apiErr.Error.Message != "" {
		message = apiErr.Error.Message
		if apiErr.Error.Status != 0 {
			statusCode = errPayloadStatus(apiErr.Error.Status, resp.StatusCode)
		}
	}

	if statusCode == http.StatusForbidden && strings.Contains(strings.ToLower(message), "premium") {
		return &APIError{
			StatusCode: statusCode,
			Message:    "spotify Premium is required for playback control",
			RetryAfter: retryAfter,
		}
	}

	return &APIError{
		StatusCode: statusCode,
		Message:    message,
		RetryAfter: retryAfter,
	}
}

func errPayloadStatus(status, fallback int) int {
	if status != 0 {
		return status
	}
	return fallback
}
