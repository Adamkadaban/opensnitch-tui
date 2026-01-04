// Package view defines the interface for Bubble Tea view models
// used in the application's routing system.
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
