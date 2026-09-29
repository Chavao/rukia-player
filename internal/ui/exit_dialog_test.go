package ui

import (
	"strings"
	"testing"
)

func TestExitDialogToggle(t *testing.T) {
	d := NewExitDialog()
	if d.Selected != ExitOptionYes {
		t.Errorf("expected initial selection ExitOptionYes, got %v", d.Selected)
	}

	d.Next()
	if d.Selected != ExitOptionNo {
		t.Errorf("expected selection ExitOptionNo after Next(), got %v", d.Selected)
	}

	d.Prev()
	if d.Selected != ExitOptionYes {
		t.Errorf("expected selection ExitOptionYes after Prev(), got %v", d.Selected)
	}
}

func TestExitDialogView(t *testing.T) {
	d := NewExitDialog()
	view := d.View()

	if !strings.Contains(view, "Do you want to exit?") {
		t.Error("view missing title")
	}
	if !strings.Contains(view, "<No>") || !strings.Contains(view, "<Yes>") {
		t.Error("view missing buttons")
	}
	if !strings.Contains(view, "Use left/right and Enter") {
		t.Error("view missing subtitle")
	}
}

func TestOverlayCenter(t *testing.T) {
	bg := "line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7"
	modal := "MODAL"
	result := OverlayCenter(bg, modal, 20, 7)

	if !strings.Contains(result, "MODAL") {
		t.Error("overlay does not contain modal content")
	}
}
