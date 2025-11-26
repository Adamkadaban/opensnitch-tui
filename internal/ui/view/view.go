package view

import tea "github.com/charmbracelet/bubbletea"

// Model represents a routed Bubble Tea view.
type Model interface {
	tea.Model
	SetSize(width, height int)
	Title() string
}
