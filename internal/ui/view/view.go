package view

import (
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	tea "github.com/charmbracelet/bubbletea"
)

// Model represents a routed Bubble Tea view.
type Model interface {
	tea.Model
	SetSize(width, height int)
	SetTheme(theme theme.Theme)
	Title() string
}

// MessageHandler identifies messages owned by an inactive routed view.
type MessageHandler interface {
	HandlesMessage(tea.Msg) bool
}

// Closer releases resources owned by a routed view.
type Closer interface {
	Close()
}
