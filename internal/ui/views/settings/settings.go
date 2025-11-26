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

	focus       field
	actionIdx   int
	durationIdx int
	targetIdx   int
	status      string
}

type field int

const (
	fieldAction field = iota
	fieldDuration
	fieldTarget
)

type option struct {
	label string
	value string
}

var promptActions = []option{
	{label: "Allow", value: "allow"},
	{label: "Deny", value: "deny"},
	{label: "Reject", value: "reject"},
}

var promptDurations = []option{
	{label: "Once", value: "once"},
	{label: "Until restart", value: "until restart"},
	{label: "Always", value: "always"},
}

var promptTargets = []option{
	{label: "Executable", value: "process.path"},
	{label: "Command", value: "process.command"},
	{label: "Process ID", value: "process.id"},
	{label: "User ID", value: "user.id"},
	{label: "Destination host", value: "dest.host"},
	{label: "Destination IP", value: "dest.ip"},
	{label: "Destination port", value: "dest.port"},
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
	switch key := msg.(type) {
	case tea.KeyMsg:
		switch key.String() {
		case "tab", "down", "j":
			m.focus = (m.focus + 1) % 3
		case "shift+tab", "up", "k":
			m.focus--
			if m.focus < 0 {
				m.focus = fieldTarget
			}
		case "left", "h":
			m.shiftSelection(-1)
		case "right", "l":
			m.shiftSelection(1)
		case "enter", "s":
			m.persistFocused()
		}
	}

	return m, nil
}

func (m *Model) View() string {
	body := []string{
		m.renderRow("Default action", promptActions, m.actionIdx, m.focus == fieldAction),
		m.renderRow("Default duration", promptDurations, m.durationIdx, m.focus == fieldDuration),
		m.renderRow("Default target", promptTargets, m.targetIdx, m.focus == fieldTarget),
		m.theme.Subtle.Render("tab move · ←/→ change · enter save"),
	}
	if m.status != "" {
		body = append(body, m.status)
	}

	content := strings.Join(body, "\n")
	return lipgloss.NewStyle().Width(m.contentWidth()).Height(max(5, m.height-2)).Render(content)
}

func (m *Model) syncSelection() {
	snapshot := m.store.Snapshot()
	m.actionIdx = optionIndex(promptActions, snapshot.Settings.DefaultPromptAction)
	m.durationIdx = optionIndex(promptDurations, snapshot.Settings.DefaultPromptDuration)
	m.targetIdx = optionIndex(promptTargets, snapshot.Settings.DefaultPromptTarget)
}

func (m *Model) persistFocused() {
	if m.controller == nil {
		m.status = m.theme.Danger.Render("Settings controller unavailable")
		return
	}
	switch m.focus {
	case fieldAction:
		m.persistAction()
	case fieldDuration:
		m.persistDuration()
	case fieldTarget:
		m.persistTarget()
	}
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

func (m *Model) shiftSelection(delta int) {
	switch m.focus {
	case fieldAction:
		m.actionIdx = wrap(m.actionIdx+delta, len(promptActions))
	case fieldDuration:
		m.durationIdx = wrap(m.durationIdx+delta, len(promptDurations))
	case fieldTarget:
		m.targetIdx = wrap(m.targetIdx+delta, len(promptTargets))
	}
}

func (m *Model) persistAction() {
	choice := promptActions[m.actionIdx].value
	value, err := m.controller.SetDefaultPromptAction(choice)
	if err != nil {
		m.status = m.theme.Danger.Render(fmt.Sprintf("Failed to save action: %v", err))
		return
	}
	m.actionIdx = optionIndex(promptActions, value)
	m.updateSettings(func(settings *state.Settings) {
		settings.DefaultPromptAction = value
	})
	m.status = m.theme.Success.Render(fmt.Sprintf("Default action set to %s", value))
}

func (m *Model) persistDuration() {
	choice := promptDurations[m.durationIdx].value
	value, err := m.controller.SetDefaultPromptDuration(choice)
	if err != nil {
		m.status = m.theme.Danger.Render(fmt.Sprintf("Failed to save duration: %v", err))
		return
	}
	m.durationIdx = optionIndex(promptDurations, value)
	m.updateSettings(func(settings *state.Settings) {
		settings.DefaultPromptDuration = value
	})
	m.status = m.theme.Success.Render(fmt.Sprintf("Default duration set to %s", value))
}

func (m *Model) persistTarget() {
	choice := promptTargets[m.targetIdx].value
	value, err := m.controller.SetDefaultPromptTarget(choice)
	if err != nil {
		m.status = m.theme.Danger.Render(fmt.Sprintf("Failed to save target: %v", err))
		return
	}
	m.targetIdx = optionIndex(promptTargets, value)
	m.updateSettings(func(settings *state.Settings) {
		settings.DefaultPromptTarget = value
	})
	m.status = m.theme.Success.Render(fmt.Sprintf("Default target set to %s", value))
}

func (m *Model) updateSettings(mut func(*state.Settings)) {
	if mut == nil {
		return
	}
	settings := m.store.Snapshot().Settings
	mut(&settings)
	m.store.SetSettings(settings)
}

func (m *Model) renderRow(label string, opts []option, selected int, focused bool) string {
	cells := make([]string, len(opts))
	for idx, opt := range opts {
		style := m.theme.TabInactive
		if idx == selected {
			style = m.theme.TabActive
			if focused {
				style = style.Underline(true)
			}
		} else if focused {
			style = style.Faint(true)
		}
		cells[idx] = style.Render(opt.label)
	}
	return fmt.Sprintf("%s %s", m.theme.Header.Render(label+":"), strings.Join(cells, " "))
}

func optionIndex(options []option, value string) int {
	for idx, opt := range options {
		if opt.value == value {
			return idx
		}
	}
	return 0
}
