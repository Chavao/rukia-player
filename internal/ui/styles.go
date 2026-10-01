package ui

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	// Theme Colors
	ColorCyan        = lipgloss.Color("#00e5ff")
	ColorBrightBlue  = lipgloss.Color("#0077ff")
	ColorDimCyan     = lipgloss.Color("#0096c7")
	ColorMuted       = lipgloss.Color("#6c757d")
	ColorDarkMuted   = lipgloss.Color("#495057")
	ColorText        = lipgloss.Color("#e0e6ed")
	ColorTextDim     = lipgloss.Color("#8892b0")
	ColorBackground  = lipgloss.Color("#0d1117")
	ColorModalBg     = lipgloss.Color("#0a0e14")
	ColorSelectionBg = lipgloss.Color("#162032")
	ColorError       = lipgloss.Color("#ff6b6b")

	// Header Styles
	HeaderLibraryStyle = lipgloss.NewStyle().
				Foreground(ColorTextDim).
				Bold(false)

	HeaderAccentStyle = lipgloss.NewStyle().
				Foreground(ColorCyan).
				Bold(true)

	HeaderInfoStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	// Track Table Styles
	TrackArtistNormal = lipgloss.NewStyle().
				Foreground(ColorTextDim)

	TrackTitleNormal = lipgloss.NewStyle().
				Foreground(ColorText)

	TrackDurationNormal = lipgloss.NewStyle().
				Foreground(ColorTextDim).
				Align(lipgloss.Right)

	TrackRowSelected = lipgloss.NewStyle().
				Foreground(ColorCyan).
				Bold(true)

	TrackRowPlaying = lipgloss.NewStyle().
			Foreground(ColorCyan).
			Bold(true)

	// Bottom Bar Styles
	BottomTrackStyle = lipgloss.NewStyle().
				Foreground(ColorCyan).
				Bold(true)

	BottomErrorStyle = lipgloss.NewStyle().
				Foreground(ColorError).
				Bold(true)

	BottomStatusStyle = lipgloss.NewStyle().
				Foreground(ColorDimCyan)

	ProgressBarFilled = lipgloss.NewStyle().
				Foreground(ColorCyan)

	ProgressBarEmpty = lipgloss.NewStyle().
				Foreground(ColorDarkMuted)

	// Exit Modal Dialog Styles (Matching Image 1)
	ModalBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorCyan).
			Background(ColorModalBg).
			Padding(1, 3).
			Width(48).
			Align(lipgloss.Center)

	ModalTitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ffffff")).
			Bold(true).
			MarginBottom(1)

	ModalButtonActive = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#ffffff")).
				Background(ColorBrightBlue).
				Bold(true).
				Padding(0, 1)

	ModalButtonInactive = lipgloss.NewStyle().
				Foreground(ColorTextDim).
				Background(lipgloss.Color("#1a2230")).
				Padding(0, 1)

	ModalSubtitleStyle = lipgloss.NewStyle().
				Foreground(ColorTextDim).
				MarginTop(1)
)
