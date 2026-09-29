package util

import (
	"fmt"
	"strings"
	"time"
)

// FormatDuration formats milliseconds into "m:ss" (or "h:mm:ss" if >= 1 hour).
func FormatDuration(ms int) string {
	if ms < 0 {
		ms = 0
	}
	totalSeconds := ms / 1000
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}

// FormatPlaylistDuration formats a playlist total duration into "1h 23m 36s" or "45m 20s".
func FormatPlaylistDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalSeconds := int(d.Seconds())
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60

	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

// RenderProgressBar renders a horizontal progress bar of a given character width.
// Uses filled and empty characters matching terminal themes.
func RenderProgressBar(currentMs int, totalMs int, width int) string {
	if width <= 0 {
		return ""
	}
	if totalMs <= 0 {
		return strings.Repeat("─", width)
	}

	progressRatio := float64(currentMs) / float64(totalMs)
	if progressRatio < 0 {
		progressRatio = 0
	} else if progressRatio > 1.0 {
		progressRatio = 1.0
	}

	filledLength := int(progressRatio * float64(width))
	if filledLength > width {
		filledLength = width
	}

	var sb strings.Builder
	for i := 0; i < width; i++ {
		if i < filledLength {
			sb.WriteString("━")
		} else {
			sb.WriteString("─")
		}
	}

	return sb.String()
}
