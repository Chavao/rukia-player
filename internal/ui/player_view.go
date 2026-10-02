package ui

import (
	"fmt"
	"strings"

	"github.com/Chavao/rukia-player/internal/spotify"
	"github.com/Chavao/rukia-player/internal/util"
	"github.com/charmbracelet/lipgloss"
)

// RenderHeader renders the top status bar matching Image 2.
func RenderHeader(userName string, playlist *spotify.Playlist, width int) string {
	if playlist == nil || width <= 0 {
		return ""
	}

	centerText := playlist.Name
	center := HeaderAccentStyle.Render(centerText)
	centerW := lipgloss.Width(center)

	// If width is very narrow, render only the playlist name truncated to width
	if width < 30 {
		return HeaderAccentStyle.Render(truncateString(centerText, width))
	}

	leftText := fmt.Sprintf("< Library of %s", userName)
	if userName == "" {
		leftText = "< Library"
	}
	left := HeaderLibraryStyle.Render(leftText)
	leftW := lipgloss.Width(left)

	durationStr := util.FormatPlaylistDuration(playlist.TotalDuration)
	rightText := fmt.Sprintf("%d tracks, %s", playlist.TotalTracks, durationStr)
	right := HeaderInfoStyle.Render(rightText)
	rightW := lipgloss.Width(right)

	// If terminal cannot fit all three sections, drop right metadata
	if width < leftW+centerW+rightW+2 {
		if width < leftW+centerW+1 {
			// Compact mode: show only playlist name truncated to available width
			return HeaderAccentStyle.Render(truncateString(centerText, width))
		}
		// Medium width: show left and center
		gap := max(1, width-leftW-centerW)
		return left + strings.Repeat(" ", gap) + center
	}

	// Normal width: three areas (left, center, right)
	rem := width - leftW - rightW - centerW
	gapLeft := max(1, rem/2)
	gapRight := max(1, rem-gapLeft)

	return left + strings.Repeat(" ", gapLeft) + center + strings.Repeat(" ", gapRight) + right
}

// RenderTrackTable renders the scrollable list of tracks with three columns.
func RenderTrackTable(tracks []spotify.Track, cursor int, playingIdx int, width int, height int) string {
	if len(tracks) == 0 {
		return lipgloss.NewStyle().Foreground(ColorMuted).Render("No tracks in playlist.")
	}

	if height < 1 {
		height = 10
	}

	// Calculate window scroll bounds so cursor is always visible
	startIdx := 0
	if cursor >= height {
		startIdx = cursor - height + 1
	}
	endIdx := startIdx + height
	if endIdx > len(tracks) {
		endIdx = len(tracks)
	}

	// Column widths:
	// Col 3 (Duration): fixed 8 chars
	// Col 1 & 2 share remaining width equally
	durationColW := 8
	remW := width - durationColW - 4
	if remW < 20 {
		remW = 20
	}
	col1W := remW * 45 / 100
	col2W := remW - col1W

	var rows []string
	for i := startIdx; i < endIdx; i++ {
		t := tracks[i]
		isSelected := (i == cursor)
		isPlaying := (i == playingIdx)

		// Column 1: Artist - Album
		col1Text := t.Artist
		if t.Album != "" {
			col1Text += " - " + t.Album
		}
		col1Text = truncateString(col1Text, col1W)
		col1Padded := padRight(col1Text, col1W)

		// Column 2: Track Title
		col2Text := truncateString(t.Name, col2W)
		col2Padded := padRight(col2Text, col2W)

		// Column 3: Duration
		durStr := util.FormatDuration(t.DurationMs)
		if isPlaying {
			durStr = "✓ " + durStr
		}
		durPadded := padLeft(durStr, durationColW)

		var rowStr string
		if isSelected {
			fullRow := fmt.Sprintf("%s  %s  %s", col1Padded, col2Padded, durPadded)
			rowStr = TrackRowSelected.Render(fullRow)
		} else if !t.CanPlay() {
			styledCol1 := TrackUnplayableNormal.Render(col1Padded)
			styledCol2 := TrackUnplayableNormal.Render(col2Padded)
			styledCol3 := TrackUnplayableNormal.Render(durPadded)
			rowStr = fmt.Sprintf("%s  %s  %s", styledCol1, styledCol2, styledCol3)
		} else {
			styledCol1 := TrackArtistNormal.Render(col1Padded)
			styledCol2 := TrackTitleNormal.Render(col2Padded)
			styledCol3 := TrackDurationNormal.Render(durPadded)
			rowStr = fmt.Sprintf("%s  %s  %s", styledCol1, styledCol2, styledCol3)
		}

		rows = append(rows, rowStr)
	}

	return strings.Join(rows, "\n")
}

// RenderBottomBar renders the player progress bar and metadata matching Image 2.
func RenderBottomBar(currentTrack *spotify.Track, progressMs int, volume int, isPlaying bool, shuffle bool, shuffleKnown bool, repeatMode string, width int, errStr ...string) string {
	if width <= 0 {
		return ""
	}

	totalMs := 180000
	trackTitle := "Ready"
	if currentTrack != nil {
		totalMs = currentTrack.DurationMs
		if currentTrack.Artist != "" {
			trackTitle = fmt.Sprintf("■ %s - %s", currentTrack.Artist, currentTrack.Name)
		} else {
			trackTitle = "■ " + currentTrack.Name
		}
	} else {
		trackTitle = "■ Stopped"
	}

	// Line 1: Progress bar
	bar := util.RenderProgressBar(progressMs, totalMs, width)
	barStyled := ProgressBarFilled.Render(bar)

	// Line 2: Details
	// Left: Error message if present, otherwise Track name
	// Right: [S][R] ▶ 0:00 / 3:00 [100%]
	repIcon := " "
	if repeatMode != "off" && repeatMode != "" {
		repIcon = "R"
	}
	shufIcon := " "
	if !shuffleKnown {
		shufIcon = "?"
	} else if shuffle {
		shufIcon = "S"
	}
	playIcon := "⏸"
	if isPlaying {
		playIcon = "▶"
	}

	statusRight := fmt.Sprintf("[%s] [%s] %s %s / %s [%d%%]",
		shufIcon,
		repIcon,
		playIcon,
		util.FormatDuration(progressMs),
		util.FormatDuration(totalMs),
		volume,
	)

	rightStyled := BottomStatusStyle.Render(statusRight)
	rightW := lipgloss.Width(rightStyled)

	leftMaxW := width - rightW - 2
	if leftMaxW < 10 {
		leftMaxW = 10
	}

	var leftStyled string
	if len(errStr) > 0 && errStr[0] != "" {
		errDisplay := truncateString("⚠ "+errStr[0], leftMaxW)
		leftStyled = BottomErrorStyle.Render(errDisplay)
	} else {
		trackTitle = truncateString(trackTitle, leftMaxW)
		leftStyled = BottomTrackStyle.Render(trackTitle)
	}

	leftW := lipgloss.Width(leftStyled)
	gapW := width - leftW - rightW
	if gapW < 1 {
		gapW = 1
	}

	infoLine := leftStyled + strings.Repeat(" ", gapW) + rightStyled

	return barStyled + "\n" + infoLine
}

func truncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-1]) + "…"
}

func padRight(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

func padLeft(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return strings.Repeat(" ", width-w) + s
}
