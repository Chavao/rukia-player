package ui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines the keybindings for the rukia player.
type KeyMap struct {
	Up       key.Binding
	Down     key.Binding
	Left     key.Binding
	Right    key.Binding
	Enter    key.Binding
	Space    key.Binding
	Next     key.Binding
	Prev     key.Binding
	VolumeUp key.Binding
	VolumeDn key.Binding
	Shuffle  key.Binding
	Repeat   key.Binding
	Exit     key.Binding
	Cancel   key.Binding
}

// DefaultKeyMap returns the configured keyboard shortcuts.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		Left: key.NewBinding(
			key.WithKeys("left", "h"),
			key.WithHelp("←/h", "left"),
		),
		Right: key.NewBinding(
			key.WithKeys("right", "l"),
			key.WithHelp("→/l", "right"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "play/pause"),
		),
		Space: key.NewBinding(
			key.WithKeys(" "),
			key.WithHelp("space", "pause/resume"),
		),
		Next: key.NewBinding(
			key.WithKeys("n", ">"),
			key.WithHelp("n/>", "next track"),
		),
		Prev: key.NewBinding(
			key.WithKeys("p", "<"),
			key.WithHelp("p/<", "prev track"),
		),
		VolumeUp: key.NewBinding(
			key.WithKeys("+", "="),
			key.WithHelp("+", "volume up"),
		),
		VolumeDn: key.NewBinding(
			key.WithKeys("-", "_"),
			key.WithHelp("-", "volume down"),
		),
		Shuffle: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "toggle shuffle"),
		),
		Repeat: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "toggle repeat"),
		),
		Exit: key.NewBinding(
			key.WithKeys("ctrl+q", "q", "ctrl+c"),
			key.WithHelp("ctrl+q/q", "exit"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel"),
		),
	}
}
