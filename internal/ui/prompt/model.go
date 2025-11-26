package prompt

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/adamkadaban/opensnitch-tui/internal/config"
	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
)

// Model renders and handles interactive connection prompts.
type Model struct {
	store      *state.Store
	theme      theme.Theme
	controller controller.PromptManager

	width  int
	height int

	focus     field
	promptIdx int
	forms     map[string]*formState
	status    string
	activeID  string
}

type field int

const (
	fieldAction field = iota
	fieldDuration
	fieldTarget
)

type formState struct {
	action   int
	duration int
	target   int
}

type actionOption struct {
	label string
	value controller.PromptAction
}

type durationOption struct {
	label string
	value controller.PromptDuration
}

type targetOption struct {
	label string
	value controller.PromptTarget
}

var actionOptions = []actionOption{
	{label: "Allow", value: controller.PromptActionAllow},
	{label: "Deny", value: controller.PromptActionDeny},
	{label: "Reject", value: controller.PromptActionReject},
}

var durationOptions = []durationOption{
	{label: "Once", value: controller.PromptDurationOnce},
	{label: "Until restart", value: controller.PromptDurationUntilRestart},
	{label: "Always", value: controller.PromptDurationAlways},
}

var fallbackPromptTimeout = time.Duration(config.DefaultPromptTimeoutSeconds) * time.Second

func New(store *state.Store, th theme.Theme, ctrl controller.PromptManager) *Model {
	return &Model{
		store:      store,
		theme:      th,
		controller: ctrl,
		forms:      make(map[string]*formState),
	}
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *Model) SetTheme(th theme.Theme) {
	m.theme = th
}

func (m *Model) Active() bool {
	snapshot := m.store.Snapshot()
	return m.shouldDisplayPrompts(snapshot)
}

func (m *Model) Update(msg tea.Msg) (tea.Cmd, bool) {
	snapshot := m.store.Snapshot()
	if !m.shouldDisplayPrompts(snapshot) {
		m.syncForms(snapshot.Prompts)
		return nil, false
	}
	prompt, targets, form, ok := m.promptStateFromSnapshot(snapshot)
	if !ok {
		return nil, false
	}
	allowTabPassthrough := !snapshot.Settings.AlertsInterrupt && snapshot.ActiveView == state.ViewAlerts

	switch key := msg.(type) {
	case tea.KeyMsg:
		switch key.String() {
		case "tab":
			if allowTabPassthrough {
				return nil, false
			}
			m.focus = (m.focus + 1) % 3
			return nil, true
		case "shift+tab":
			if allowTabPassthrough {
				return nil, false
			}
			m.focus--
			if m.focus < 0 {
				m.focus = fieldTarget
			}
			return nil, true
		case "down", "j":
			m.focus = (m.focus + 1) % 3
			return nil, true
		case "up", "k":
			m.focus--
			if m.focus < 0 {
				m.focus = fieldTarget
			}
			return nil, true
		case "left", "h":
			m.stepSelection(-1, form, len(targets))
			return nil, true
		case "right", "l":
			m.stepSelection(1, form, len(targets))
			return nil, true
		case "a":
			form.action = 0
			return nil, true
		case "d":
			form.action = 1
			return nil, true
		case "r":
			form.action = 2
			return nil, true
		case "[":
			m.shiftPrompt(-1)
			return nil, true
		case "]":
			m.shiftPrompt(1)
			return nil, true
		case "enter", "esc":
			m.submit(prompt, targets, form)
			return nil, true
		}
	}

	return nil, false
}

func (m *Model) View() string {
	snapshot := m.store.Snapshot()
	if !m.shouldDisplayPrompts(snapshot) {
		return ""
	}
	prompt, targets, form, ok := m.promptStateFromSnapshot(snapshot)
	if !ok {
		return ""
	}

	headline := fmt.Sprintf("Connection prompt · %s · node %s", prompt.ID, prompt.NodeName)
	dest := prompt.Connection.DstHost
	if dest == "" {
		dest = prompt.Connection.DstIP
	}
	command := strings.Join(prompt.Connection.ProcessArgs, " ")
	info := []string{
		fmt.Sprintf("Process: %s", fallback(prompt.Connection.ProcessPath, "unknown")),
		fmt.Sprintf("Command: %s", fallback(command, "-")),
		fmt.Sprintf("Destination: %s:%d (%s)", fallback(dest, "unknown"), prompt.Connection.DstPort, prompt.Connection.Protocol),
		fmt.Sprintf("User %d · PID %d", prompt.Connection.UserID, prompt.Connection.ProcessID),
	}

	actionRow := m.renderChoices("Action", mapActionLabels(actionOptions), form.action, m.focus == fieldAction)
	durationRow := m.renderChoices("Duration", mapDurationLabels(durationOptions), form.duration, m.focus == fieldDuration)
	targetRow := m.renderChoices("Target", mapTargetLabels(targets), form.target, m.focus == fieldTarget)

	controls := m.theme.Subtle.Render("↑/↓ move · ←/→ change · enter confirm · [/] cycle prompts")
	expiresAt := prompt.ExpiresAt
	if expiresAt.IsZero() && !prompt.RequestedAt.IsZero() {
		timeout := snapshot.Settings.PromptTimeout
		if timeout <= 0 {
			timeout = fallbackPromptTimeout
		}
		expiresAt = prompt.RequestedAt.Add(timeout)
	}
	status := m.status
	if status == "" && !expiresAt.IsZero() {
		remaining := time.Until(expiresAt)
		if remaining < 0 {
			remaining = 0
		}
		status = fmt.Sprintf("Timeout in %s", remaining.Round(time.Second))
	}

	body := lipgloss.JoinVertical(lipgloss.Left,
		m.theme.Header.Render(headline),
		strings.Join(info, "\n"),
		actionRow,
		durationRow,
		targetRow,
		controls,
		status,
	)

	return lipgloss.Place(m.width, max(10, m.height-2), lipgloss.Center, lipgloss.Center, m.theme.Card.Width(min(m.width-4, 96)).Render(body))
}

func (m *Model) promptState() (state.Prompt, []targetOption, *formState, bool) {
	snapshot := m.store.Snapshot()
	return m.promptStateFromSnapshot(snapshot)
}

func (m *Model) promptStateFromSnapshot(snapshot state.Snapshot) (state.Prompt, []targetOption, *formState, bool) {
	m.syncForms(snapshot.Prompts)
	if len(snapshot.Prompts) == 0 {
		return state.Prompt{}, nil, nil, false
	}
	if m.promptIdx >= len(snapshot.Prompts) {
		m.promptIdx = len(snapshot.Prompts) - 1
	}
	if m.promptIdx < 0 {
		m.promptIdx = 0
	}
	prompt := snapshot.Prompts[m.promptIdx]
	if prompt.ID != m.activeID {
		m.activeID = prompt.ID
		m.status = ""
	}
	targets := targetOptionsFor(prompt.Connection)
	form := m.ensureForm(prompt.ID, targets)
	return prompt, targets, form, true
}

func (m *Model) ensureForm(id string, targets []targetOption) *formState {
	form, ok := m.forms[id]
	if !ok {
		form = &formState{
			action:   m.defaultActionIndex(),
			duration: m.defaultDurationIndex(),
			target:   m.defaultTargetIndex(targets),
		}
		m.forms[id] = form
	}
	if form.action >= len(actionOptions) {
		form.action = len(actionOptions) - 1
	}
	if form.duration >= len(durationOptions) {
		form.duration = len(durationOptions) - 1
	}
	if len(targets) == 0 {
		form.target = 0
	} else if form.target >= len(targets) {
		form.target = len(targets) - 1
	}
	return form
}

func (m *Model) syncForms(prompts []state.Prompt) {
	if len(m.forms) == 0 {
		return
	}
	keep := make(map[string]struct{}, len(prompts))
	for _, prompt := range prompts {
		keep[prompt.ID] = struct{}{}
	}
	for id := range m.forms {
		if _, ok := keep[id]; !ok {
			delete(m.forms, id)
		}
	}
	if len(prompts) == 0 {
		m.promptIdx = 0
	}
}

func (m *Model) stepSelection(delta int, form *formState, targets int) {
	switch m.focus {
	case fieldAction:
		form.action = wrapIndex(form.action, delta, len(actionOptions))
	case fieldDuration:
		form.duration = wrapIndex(form.duration, delta, len(durationOptions))
	case fieldTarget:
		form.target = wrapIndex(form.target, delta, max(1, targets))
	}
}

func (m *Model) submit(prompt state.Prompt, targets []targetOption, form *formState) {
	if m.controller == nil {
		m.status = m.theme.Danger.Render("Prompt controller unavailable")
		return
	}
	decision := controller.PromptDecision{
		PromptID: prompt.ID,
		Action:   actionOptions[min(form.action, len(actionOptions)-1)].value,
		Duration: durationOptions[min(form.duration, len(durationOptions)-1)].value,
	}
	if len(targets) > 0 {
		decision.Target = targets[min(form.target, len(targets)-1)].value
	}
	if err := m.controller.ResolvePrompt(decision); err != nil {
		m.status = m.theme.Danger.Render(fmt.Sprintf("Failed to send decision: %v", err))
		return
	}
	m.status = m.theme.Success.Render(fmt.Sprintf("Action %s for %s", decision.Action, prompt.NodeName))
}

func (m *Model) shiftPrompt(delta int) {
	snapshot := m.store.Snapshot()
	m.syncForms(snapshot.Prompts)
	if len(snapshot.Prompts) == 0 {
		return
	}
	m.promptIdx = wrapIndex(m.promptIdx, delta, len(snapshot.Prompts))
}

func (m *Model) renderChoices(label string, options []string, selected int, focused bool) string {
	cells := make([]string, len(options))
	for idx, option := range options {
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
		cells[idx] = fmt.Sprintf("%s%s", marker, style.Render(option))
	}
	return fmt.Sprintf("%s %s", m.theme.Header.Render(label+":"), strings.Join(cells, " "))
}

func targetOptionsFor(conn state.Connection) []targetOption {
	options := make([]targetOption, 0, 6)
	if conn.ProcessPath != "" {
		options = append(options, targetOption{label: "Executable", value: controller.PromptTargetProcessPath})
	}
	if len(conn.ProcessArgs) > 0 {
		options = append(options, targetOption{label: "Command", value: controller.PromptTargetProcessCmd})
	}
	if conn.DstHost != "" {
		options = append(options, targetOption{label: "Destination host", value: controller.PromptTargetDestinationHost})
	}
	if conn.DstIP != "" {
		options = append(options, targetOption{label: "Destination IP", value: controller.PromptTargetDestinationIP})
	}
	if conn.DstPort != 0 {
		options = append(options, targetOption{label: "Destination port", value: controller.PromptTargetDestinationPort})
	}
	options = append(options, targetOption{label: "Process ID", value: controller.PromptTargetProcessID})
	options = append(options, targetOption{label: "User ID", value: controller.PromptTargetUserID})
	return options
}

func mapActionLabels(opts []actionOption) []string {
	labels := make([]string, len(opts))
	for i, opt := range opts {
		labels[i] = opt.label
	}
	return labels
}

func mapDurationLabels(opts []durationOption) []string {
	labels := make([]string, len(opts))
	for i, opt := range opts {
		labels[i] = opt.label
	}
	return labels
}

func mapTargetLabels(opts []targetOption) []string {
	labels := make([]string, len(opts))
	for i, opt := range opts {
		labels[i] = opt.label
	}
	return labels
}

func wrapIndex(current, delta, length int) int {
	if length <= 0 {
		return 0
	}
	next := (current + delta) % length
	if next < 0 {
		next += length
	}
	return next
}

func (m *Model) defaultActionIndex() int {
	snapshot := m.store.Snapshot()
	current := snapshot.Settings.DefaultPromptAction
	for idx, opt := range actionOptions {
		if string(opt.value) == current {
			return idx
		}
	}
	return 0
}

func (m *Model) defaultDurationIndex() int {
	snapshot := m.store.Snapshot()
	current := snapshot.Settings.DefaultPromptDuration
	for idx, opt := range durationOptions {
		if string(opt.value) == current {
			return idx
		}
	}
	return 0
}

func (m *Model) defaultTargetIndex(targets []targetOption) int {
	if len(targets) == 0 {
		return 0
	}
	snapshot := m.store.Snapshot()
	current := snapshot.Settings.DefaultPromptTarget
	for idx, opt := range targets {
		if string(opt.value) == current {
			return idx
		}
	}
	return 0
}

func (m *Model) shouldDisplayPrompts(snapshot state.Snapshot) bool {
	if len(snapshot.Prompts) == 0 {
		return false
	}
	if snapshot.Settings.AlertsInterrupt {
		return true
	}
	return snapshot.ActiveView == state.ViewAlerts
}

func fallback(value, def string) string {
	if strings.TrimSpace(value) == "" {
		return def
	}
	return value
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
