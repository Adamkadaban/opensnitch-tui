package settings

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/view"
)

// Model renders the settings view for global preferences.
type Model struct {
	store      *state.Store
	theme      theme.Theme
	controller controller.SettingsManager

	width  int
	height int

	selected int
	status   string
}

var promptActions = []struct {
	label string
	value string
}{
	{label: "Allow", value: "allow"},
	{label: "Deny", value: "deny"},
	{label: "Reject", value: "reject"},
}

// New constructs a settings view model.
func New(store *state.Store, th theme.Theme, ctrl controller.SettingsManager) view.Model {
	m := &Model{store: store, theme: th, controller: ctrl}
	m.syncSelection()
	return m
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Title() string { return "Settings" }

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.syncSelection()

	switch key := msg.(type) {
	case tea.KeyMsg:
		switch key.String() {
		case "left", "h":
			m.selected = wrap(m.selected-1, len(promptActions))
		case "right", "l":
			m.selected = wrap(m.selected+1, len(promptActions))
		case "enter", "s":
			m.persistSelection()
		}
	}

	return m, nil
}

func (m *Model) View() string {
	m.syncSelection()

	cells := make([]string, len(promptActions))
	for idx, action := range promptActions {
		style := m.theme.TabInactive
		if idx == m.selected {
			style = m.theme.TabActive
		}
		cells[idx] = style.Render(action.label)
	}

	body := []string{
		m.theme.Header.Render("Default prompt action"),
		strings.Join(cells, " "),
		m.theme.Subtle.Render("←/→ select · enter save"),
	}
	if m.status != "" {
		body = append(body, m.status)
	}

	content := strings.Join(body, "\n")
	return lipgloss.NewStyle().Width(m.contentWidth()).Height(max(5, m.height-2)).Render(content)
}

func (m *Model) syncSelection() {
	snapshot := m.store.Snapshot()
	current := snapshot.Settings.DefaultPromptAction
	for idx, option := range promptActions {
		if option.value == current {
			m.selected = idx
			return
		}
	}
	m.selected = 0
}

func (m *Model) persistSelection() {
	if m.controller == nil {
		m.status = m.theme.Danger.Render("Settings controller unavailable")
		return
	}
	choice := promptActions[m.selected].value
	value, err := m.controller.SetDefaultPromptAction(choice)
	if err != nil {
		m.status = m.theme.Danger.Render(fmt.Sprintf("Failed to save: %v", err))
		return
	}
	m.store.SetSettings(state.Settings{DefaultPromptAction: value})
	m.status = m.theme.Success.Render(fmt.Sprintf("Default action set to %s", value))
}

func (m *Model) contentWidth() int {
	if m.width <= 0 {
		return 80
	}
	return m.width - 4
}

func wrap(value, size int) int {
	if size == 0 {
		return 0
	}
	value %= size
	if value < 0 {
		value += size
	}
	return value
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
