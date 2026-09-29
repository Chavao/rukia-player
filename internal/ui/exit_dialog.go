package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type ExitOption int

const (
	ExitOptionNo ExitOption = iota
	ExitOptionYes
)

// ExitDialog represents the exit confirmation modal dialog.
type ExitDialog struct {
	Selected ExitOption
}

// NewExitDialog initializes the dialog with default selection (Yes as in Image 1).
func NewExitDialog() ExitDialog {
	return ExitDialog{
		Selected: ExitOptionYes,
	}
}

// Next selects the other button.
func (d *ExitDialog) Next() {
	if d.Selected == ExitOptionNo {
		d.Selected = ExitOptionYes
	} else {
		d.Selected = ExitOptionNo
	}
}

// Prev selects the other button.
func (d *ExitDialog) Prev() {
	d.Next()
}

// View renders the modal dialog matching Image 1.
func (d *ExitDialog) View() string {
	title := ModalTitleStyle.Render("Do you want to exit?")

	var noBtn, yesBtn string
	if d.Selected == ExitOptionNo {
		noBtn = ModalButtonActive.Render("<No>")
		yesBtn = ModalButtonInactive.Render("<Yes>")
	} else {
		noBtn = ModalButtonInactive.Render("<No>")
		yesBtn = ModalButtonActive.Render("<Yes>")
	}

	buttons := lipgloss.JoinHorizontal(lipgloss.Center, noBtn, "    ", yesBtn)
	subtitle := ModalSubtitleStyle.Render("Use left/right and Enter")

	content := lipgloss.JoinVertical(
		lipgloss.Center,
		title,
		"",
		buttons,
		"",
		subtitle,
	)

	return ModalBoxStyle.Render(content)
}

// OverlayCenter centers the modal dialog over the background content.
func OverlayCenter(bg string, modal string, width int, height int) string {
	if width <= 0 || height <= 0 {
		return modal
	}

	modalLines := strings.Split(modal, "\n")
	bgLines := strings.Split(bg, "\n")

	modalH := len(modalLines)
	startRow := (height - modalH) / 2
	if startRow < 0 {
		startRow = 0
	}

	for i, mLine := range modalLines {
		targetRow := startRow + i
		if targetRow < len(bgLines) {
			mLen := lipgloss.Width(mLine)
			startCol := (width - mLen) / 2
			if startCol < 0 {
				startCol = 0
			}

			// Render modal line centered
			leftPad := strings.Repeat(" ", startCol)
			bgLines[targetRow] = leftPad + mLine
		}
	}

	return strings.Join(bgLines, "\n")
}
