package settings

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/charmbracelet/bubbles/textinput"

	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/view"
	"github.com/adamkadaban/opensnitch-tui/internal/util"
)

// Model renders the settings view for global preferences.
type Model struct {
	store      *state.Store
	theme      theme.Theme
	controller controller.SettingsManager

	width  int
	height int

	focus           field
	themeIdx        int
	actionIdx       int
	durationIdx     int
	targetIdx       int
	timeoutIdx      int
	alertsInterrupt bool
	pauseOnInspect  bool
	yaraEnabled     bool
	yaraRuleDir     textinput.Model
	status          string
}

type field int

const (
	fieldTheme field = iota
	fieldAction
	fieldDuration
	fieldTarget
	fieldPromptTimeout
	fieldAlertsInterrupt
	fieldPauseOnInspect
	fieldYaraEnabled
	fieldYaraRuleDir
)

const settingsFieldCount = 9

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

var promptTimeouts = []option{
	{label: "15s", value: "15"},
	{label: "30s", value: "30"},
	{label: "60s", value: "60"},
	{label: "120s", value: "120"},
	{label: "300s", value: "300"},
}

var themeOptions = buildThemeOptions()

// New constructs a settings view model.
func New(store *state.Store, th theme.Theme, ctrl controller.SettingsManager) view.Model {
	m := &Model{store: store, theme: th, controller: ctrl}
	m.yaraRuleDir = textinput.New()
	m.yaraRuleDir.Placeholder = "/path/to/yara_rules"
	m.yaraRuleDir.CharLimit = 0
	m.yaraRuleDir.Width = 40
	m.syncSelection()
	return m
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Title() string { return "Settings" }

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *Model) SetTheme(th theme.Theme) {
	m.theme = th
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch key := msg.(type) {
	case tea.KeyMsg:
		// Special handling for text input field
		if m.focus == fieldYaraRuleDir {
			// Manage input focus
			m.yaraRuleDir.Focus()
			switch key.Type {
			case tea.KeyTab:
				m.yaraRuleDir.Blur()
				m.focus = (m.focus + 1) % settingsFieldCount
				return m, nil
			case tea.KeyShiftTab:
				m.yaraRuleDir.Blur()
				m.focus--
				if m.focus < 0 {
					m.focus = settingsFieldCount - 1
				}
				return m, nil
			case tea.KeyEnter:
				m.persistYaraRuleDir()
				return m, nil
			case tea.KeyUp:
				m.yaraRuleDir.Blur()
				m.focus--
				if m.focus < 0 {
					m.focus = settingsFieldCount - 1
				}
				return m, nil
			case tea.KeyDown:
				m.yaraRuleDir.Blur()
				m.focus = (m.focus + 1) % settingsFieldCount
				return m, nil
			case tea.KeyEsc:
				m.yaraRuleDir.Blur()
				return m, nil
			}
			m.yaraRuleDir, cmd = m.yaraRuleDir.Update(msg)
			return m, cmd
		}
		// General navigation (non-text fields): only arrows/tab/enter
		switch key.Type {
		case tea.KeyTab:
			m.focus = (m.focus + 1) % settingsFieldCount
		case tea.KeyShiftTab:
			m.focus--
			if m.focus < 0 {
				m.focus = settingsFieldCount - 1
			}
		case tea.KeyDown:
			m.focus = (m.focus + 1) % settingsFieldCount
		case tea.KeyUp:
			m.focus--
			if m.focus < 0 {
				m.focus = settingsFieldCount - 1
			}
		case tea.KeyLeft:
			m.shiftSelection(-1)
		case tea.KeyRight:
			m.shiftSelection(1)
		case tea.KeyEnter:
			m.persistAll()
		}

		// Blur text input when leaving it
		if m.focus != fieldYaraRuleDir {
			m.yaraRuleDir.Blur()
		}
	}

	return m, nil
}

func (m *Model) View() string {
	general := []string{
		m.renderRow("Theme", themeOptions, m.themeIdx, m.focus == fieldTheme),
		m.renderRow("Default action", promptActions, m.actionIdx, m.focus == fieldAction),
		m.renderRow("Default duration", promptDurations, m.durationIdx, m.focus == fieldDuration),
		m.renderRow("Default target", promptTargets, m.targetIdx, m.focus == fieldTarget),
		m.renderRow("Prompt timeout", promptTimeouts, m.timeoutIdx, m.focus == fieldPromptTimeout),
	}
	alerts := []string{
		m.renderToggle("Alerts interrupt", m.alertsInterrupt, m.focus == fieldAlertsInterrupt),
		m.renderToggle("Pause alert timeout on inspect", m.pauseOnInspect, m.focus == fieldPauseOnInspect),
	}
	security := []string{
		m.renderToggle("YARA scanning enabled", m.yaraEnabled, m.focus == fieldYaraEnabled),
		m.renderInput("YARA rule directory", m.yaraRuleDir, m.focus == fieldYaraRuleDir),
	}

	body := []string{
		m.renderSection("General", general),
		m.renderSection("Alerts", alerts),
		m.renderSection("Security", security),
		m.theme.Subtle.Render("↑/↓ move · ←/→ change · enter save all"),
	}
	if m.status != "" {
		body = append(body, m.status)
	}

	content := strings.Join(body, "\n")
	return lipgloss.NewStyle().Width(m.contentWidth()).Height(max(5, m.height-2)).Render(content)
}

func (m *Model) syncSelection() {
	snapshot := m.store.Snapshot()
	m.themeIdx = optionIndex(themeOptions, snapshot.Settings.ThemeName)
	m.actionIdx = optionIndex(promptActions, snapshot.Settings.DefaultPromptAction)
	m.durationIdx = optionIndex(promptDurations, snapshot.Settings.DefaultPromptDuration)
	m.targetIdx = optionIndex(promptTargets, snapshot.Settings.DefaultPromptTarget)
	timeoutSeconds := int(snapshot.Settings.PromptTimeout / time.Second)
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30
	}
	m.timeoutIdx = optionIndex(promptTimeouts, fmt.Sprintf("%d", timeoutSeconds))
	m.alertsInterrupt = snapshot.Settings.AlertsInterrupt
	m.pauseOnInspect = snapshot.Settings.PausePromptOnInspect
	m.yaraEnabled = snapshot.Settings.YaraEnabled
	m.yaraRuleDir.SetValue(snapshot.Settings.YaraRuleDir)
}

func (m *Model) persistAll() {
	if m.controller == nil {
		m.status = m.theme.Danger.Render("Settings controller unavailable")
		return
	}
	if _, err := m.saveTheme(); err != nil {
		m.status = m.theme.Danger.Render(fmt.Sprintf("Failed to save theme: %v", err))
		return
	}
	if _, err := m.saveAction(); err != nil {
		m.status = m.theme.Danger.Render(fmt.Sprintf("Failed to save action: %v", err))
		return
	}
	if _, err := m.saveDuration(); err != nil {
		m.status = m.theme.Danger.Render(fmt.Sprintf("Failed to save duration: %v", err))
		return
	}
	if _, err := m.saveTarget(); err != nil {
		m.status = m.theme.Danger.Render(fmt.Sprintf("Failed to save target: %v", err))
		return
	}
	if _, err := m.savePromptTimeout(); err != nil {
		m.status = m.theme.Danger.Render(fmt.Sprintf("Failed to save timeout: %v", err))
		return
	}
	if _, err := m.saveAlertsInterrupt(m.alertsInterrupt); err != nil {
		m.status = m.theme.Danger.Render(fmt.Sprintf("Failed to save alerts setting: %v", err))
		return
	}
	if _, err := m.savePauseOnInspect(m.pauseOnInspect); err != nil {
		m.status = m.theme.Danger.Render(fmt.Sprintf("Failed to save pause-on-inspect: %v", err))
		return
	}
	if _, err := m.saveYaraEnabled(m.yaraEnabled); err != nil {
		m.status = m.theme.Danger.Render(fmt.Sprintf("Failed to save YARA enabled: %v", err))
		return
	}
	if _, err := m.saveYaraRuleDir(m.yaraRuleDir.Value()); err != nil {
		m.status = m.theme.Danger.Render(fmt.Sprintf("Failed to save YARA rule dir: %v", err))
		return
	}
	m.status = m.theme.Success.Render("Settings saved")
}

func (m *Model) persistTheme() {
	if value, err := m.saveTheme(); err != nil {
		m.status = m.theme.Danger.Render(fmt.Sprintf("Failed to save theme: %v", err))
	} else {
		m.status = m.theme.Success.Render(fmt.Sprintf("Theme switched to %s", theme.Label(value)))
	}
}

func (m *Model) contentWidth() int {
	if m.width <= 0 {
		return 80
	}
	return m.width - 4
}

// (wrap and max replaced by util.WrapIndex and Go built-in max)

func (m *Model) shiftSelection(delta int) {
	switch m.focus {
	case fieldTheme:
		m.themeIdx = util.WrapIndex(m.themeIdx, delta, len(themeOptions))
	case fieldAction:
		m.actionIdx = util.WrapIndex(m.actionIdx, delta, len(promptActions))
	case fieldDuration:
		m.durationIdx = util.WrapIndex(m.durationIdx, delta, len(promptDurations))
	case fieldTarget:
		m.targetIdx = util.WrapIndex(m.targetIdx, delta, len(promptTargets))
	case fieldPromptTimeout:
		m.timeoutIdx = util.WrapIndex(m.timeoutIdx, delta, len(promptTimeouts))
	case fieldAlertsInterrupt:
		current := 0
		if m.alertsInterrupt {
			current = 1
		}
		current = util.WrapIndex(current, delta, 2)
		m.alertsInterrupt = current == 1
	case fieldPauseOnInspect:
		current := 0
		if m.pauseOnInspect {
			current = 1
		}
		current = util.WrapIndex(current, delta, 2)
		m.pauseOnInspect = current == 1
	case fieldYaraEnabled:
		current := 0
		if m.yaraEnabled {
			current = 1
		}
		current = util.WrapIndex(current, delta, 2)
		m.yaraEnabled = current == 1
	}
}

func (m *Model) persistAction() {
	if value, err := m.saveAction(); err != nil {
		m.status = m.theme.Danger.Render(fmt.Sprintf("Failed to save action: %v", err))
	} else {
		m.status = m.theme.Success.Render(fmt.Sprintf("Default action set to %s", value))
	}
}

func (m *Model) persistDuration() {
	if value, err := m.saveDuration(); err != nil {
		m.status = m.theme.Danger.Render(fmt.Sprintf("Failed to save duration: %v", err))
	} else {
		m.status = m.theme.Success.Render(fmt.Sprintf("Default duration set to %s", value))
	}
}

func (m *Model) persistTarget() {
	if value, err := m.saveTarget(); err != nil {
		m.status = m.theme.Danger.Render(fmt.Sprintf("Failed to save target: %v", err))
	} else {
		m.status = m.theme.Success.Render(fmt.Sprintf("Default target set to %s", value))
	}
}

func (m *Model) persistPromptTimeout() {
	if seconds, err := m.savePromptTimeout(); err != nil {
		m.status = m.theme.Danger.Render(fmt.Sprintf("Failed to save timeout: %v", err))
	} else {
		m.status = m.theme.Success.Render(fmt.Sprintf("Prompt timeout set to %ds", seconds))
	}
}

func (m *Model) persistAlertsInterrupt() {
	if enabled, err := m.saveAlertsInterrupt(m.alertsInterrupt); err != nil {
		m.status = m.theme.Danger.Render(fmt.Sprintf("Failed to save alerts setting: %v", err))
	} else if enabled {
		m.status = m.theme.Success.Render("Alerts will interrupt")
	} else {
		m.status = m.theme.Success.Render("Alerts stay in alerts tab")
	}
}

func (m *Model) persistPauseOnInspect() {
	if enabled, err := m.savePauseOnInspect(m.pauseOnInspect); err != nil {
		m.status = m.theme.Danger.Render(fmt.Sprintf("Failed to save pause-on-inspect: %v", err))
	} else if enabled {
		m.status = m.theme.Success.Render("Inspect will pause timeouts")
	} else {
		m.status = m.theme.Success.Render("Inspect won’t pause timeouts")
	}
}

func (m *Model) persistYaraEnabled() {
	if enabled, err := m.saveYaraEnabled(m.yaraEnabled); err != nil {
		m.status = m.theme.Danger.Render(fmt.Sprintf("Failed to save YARA enabled: %v", err))
	} else if enabled {
		m.status = m.theme.Success.Render("YARA scanning enabled")
	} else {
		m.status = m.theme.Success.Render("YARA scanning disabled")
	}
}

func (m *Model) persistYaraRuleDir() {
	if value, err := m.saveYaraRuleDir(m.yaraRuleDir.Value()); err != nil {
		m.status = m.theme.Danger.Render(fmt.Sprintf("Failed to save YARA rule dir: %v", err))
	} else {
		m.status = m.theme.Success.Render(fmt.Sprintf("YARA rule dir set to %s", value))
	}
}

func (m *Model) saveAction() (string, error) {
	choice := promptActions[m.actionIdx].value
	value, err := m.controller.SetDefaultPromptAction(choice)
	if err != nil {
		return "", err
	}
	m.actionIdx = optionIndex(promptActions, value)
	m.updateSettings(func(settings *state.Settings) {
		settings.DefaultPromptAction = value
	})
	return value, nil
}

func (m *Model) saveDuration() (string, error) {
	choice := promptDurations[m.durationIdx].value
	value, err := m.controller.SetDefaultPromptDuration(choice)
	if err != nil {
		return "", err
	}
	m.durationIdx = optionIndex(promptDurations, value)
	m.updateSettings(func(settings *state.Settings) {
		settings.DefaultPromptDuration = value
	})
	return value, nil
}

func (m *Model) saveTarget() (string, error) {
	choice := promptTargets[m.targetIdx].value
	value, err := m.controller.SetDefaultPromptTarget(choice)
	if err != nil {
		return "", err
	}
	m.targetIdx = optionIndex(promptTargets, value)
	m.updateSettings(func(settings *state.Settings) {
		settings.DefaultPromptTarget = value
	})
	return value, nil
}

func (m *Model) saveTheme() (string, error) {
	choice := themeOptions[m.themeIdx].value
	value, err := m.controller.SetTheme(choice)
	if err != nil {
		return "", err
	}
	m.themeIdx = optionIndex(themeOptions, value)
	m.updateSettings(func(settings *state.Settings) {
		settings.ThemeName = value
	})
	return value, nil
}

func (m *Model) savePromptTimeout() (int, error) {
	seconds := optionSeconds(promptTimeouts[m.timeoutIdx])
	value, err := m.controller.SetPromptTimeout(seconds)
	if err != nil {
		return 0, err
	}
	m.timeoutIdx = optionIndex(promptTimeouts, fmt.Sprintf("%d", value))
	m.updateSettings(func(settings *state.Settings) {
		settings.PromptTimeout = time.Duration(value) * time.Second
	})
	return value, nil
}

func (m *Model) saveAlertsInterrupt(enabled bool) (bool, error) {
	value, err := m.controller.SetAlertsInterrupt(enabled)
	if err != nil {
		return false, err
	}
	m.alertsInterrupt = value
	m.updateSettings(func(settings *state.Settings) {
		settings.AlertsInterrupt = value
	})
	return value, nil
}

func (m *Model) savePauseOnInspect(enabled bool) (bool, error) {
	value, err := m.controller.SetPausePromptOnInspect(enabled)
	if err != nil {
		return false, err
	}
	m.pauseOnInspect = value
	m.updateSettings(func(settings *state.Settings) {
		settings.PausePromptOnInspect = value
	})
	return value, nil
}

func (m *Model) saveYaraEnabled(enabled bool) (bool, error) {
	value, err := m.controller.SetYaraEnabled(enabled)
	if err != nil {
		return false, err
	}
	m.yaraEnabled = value
	m.updateSettings(func(settings *state.Settings) {
		settings.YaraEnabled = value
	})
	return value, nil
}

func (m *Model) saveYaraRuleDir(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path != "" {
		info, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("%s: %w", path, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("%s is not a directory", path)
		}
	}
	if m.yaraEnabled && path == "" {
		return "", fmt.Errorf("YARA rule directory required when YARA scanning is enabled")
	}
	value, err := m.controller.SetYaraRuleDir(path)
	if err != nil {
		return "", err
	}
	m.yaraRuleDir.SetValue(value)
	m.updateSettings(func(settings *state.Settings) {
		settings.YaraRuleDir = value
	})
	return value, nil
}

func (m *Model) updateSettings(mut func(*state.Settings)) {
	if mut == nil {
		return
	}
	settings := m.store.Snapshot().Settings
	mut(&settings)
	m.store.SetSettings(settings)
}

func (m *Model) renderSection(title string, rows []string) string {
	content := strings.Join(rows, "\n")
	head := m.theme.Title.Render(title)
	return fmt.Sprintf("%s\n%s", head, content)
}

func (m *Model) renderToggle(label string, enabled bool, focused bool) string {
	options := []option{
		{label: "Off", value: "off"},
		{label: "On", value: "on"},
	}
	idx := 0
	if enabled {
		idx = 1
	}
	return m.renderRow(label, options, idx, focused)
}

func (m *Model) renderInput(label string, input textinput.Model, focused bool) string {
	ti := input
	if focused {
		ti.Prompt = m.theme.Warning.Render("> ")
	} else {
		ti.Prompt = "  "
	}
	return fmt.Sprintf("%s: %s", label, ti.View())
}

func (m *Model) renderRow(label string, opts []option, selected int, focused bool) string {
	cells := make([]string, len(opts))
	for idx, opt := range opts {
		style := m.theme.TabInactive
		marker := " "
		if idx == selected {
			style = m.theme.TabActive
			if focused {
				style = style.Underline(true).Bold(true)
				marker = m.theme.Warning.Render(">")
			}
		} else if focused {
			style = style.Faint(true)
		}
		cells[idx] = fmt.Sprintf("%s%s", marker, style.Render(opt.label))
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

func optionSeconds(opt option) int {
	seconds, err := strconv.Atoi(opt.value)
	if err != nil {
		return 30
	}
	return seconds
}

func buildThemeOptions() []option {
	presets := theme.Presets()
	opts := make([]option, 0, len(presets))
	for _, preset := range presets {
		opts = append(opts, option{label: preset.Label, value: preset.Name})
	}
	return opts
}
