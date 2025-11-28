package prompt

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/adamkadaban/opensnitch-tui/internal/config"
	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	"github.com/adamkadaban/opensnitch-tui/internal/util"
	"github.com/adamkadaban/opensnitch-tui/internal/yara"
)

// Model renders and handles interactive connection prompts.
type Model struct {
	store      *state.Store
	theme      theme.Theme
	controller controller.PromptManager

	width  int
	height int

	focus          field
	promptIdx      int
	forms          map[string]*formState
	status         string
	activeID       string
	inspect        bool
	inspectInfo    processInspect
	inspectVP      viewport.Model
	inspectXOffset int
	paused         bool
	yaraPending    bool
	yaraStatus     string
	yaraKind       yaraStatusKind
	inspectRoot    bool
}

var (
	friendlyPaths = []string{"/bin", "/sbin", "/usr/bin", "/usr/sbin", "/usr/local/bin", "/usr/local/sbin"}
	dangerPaths   = []string{"/tmp", "/var/tmp", "/dev/shm", "/run", "/home", "/root"}
)

func (m *Model) highlightPath(path string) string {
	style := m.theme.Warning
	if hasPrefix(path, friendlyPaths) {
		style = m.theme.Success
	} else if hasPrefix(path, dangerPaths) {
		style = m.theme.Danger
	}
	if prefix := matchedPrefix(path, append(friendlyPaths, dangerPaths...)); prefix != "" {
		return style.Render(prefix) + path[len(prefix):]
	}
	return style.Render(path)
}

type yaraStatusKind int

const (
	yaraStatusUnknown yaraStatusKind = iota
	yaraStatusScanning
	yaraStatusNoMatches
	yaraStatusMatches
	yaraStatusError
	yaraStatusDisabled
	yaraStatusNotAvailable
	yaraStatusRuleDirMissing
	yaraStatusPathUnknown
	yaraStatusTimeout
)

func (m *Model) toggleInspect(prompt state.Prompt, settings state.Settings, local bool) tea.Cmd {
	if m.inspect {
		// resume
		if m.controller != nil && m.paused {
			_ = m.controller.ResumePrompt(prompt.ID)
		}
		m.inspect = false
		m.paused = false
		m.status = ""
		m.yaraPending = false
		m.yaraStatus = ""
		return nil
	}
	// enter inspect
	pauseOnInspect := settings.PausePromptOnInspect
	if pauseOnInspect && m.controller != nil {
		if err := m.controller.PausePrompt(prompt.ID); err == nil {
			m.paused = true
		} else {
			m.status = m.theme.Danger.Render(fmt.Sprintf("Failed to pause prompt: %v", err))
		}
	}

	// detect root (real or effective)
	root := prompt.Connection.UserID == 0
	if !root && prompt.Connection.ProcessID != 0 {
		uids, _ := readProcIDs(int(prompt.Connection.ProcessID))
		if uids[1] == "0" { // effective UID
			root = true
		}
	}
	m.inspectRoot = root
	if !local {
		msg := "Process details available only for local nodes"
		m.inspectInfo = processInspect{Lines: []string{msg}, MaxWidth: len(msg)}
		m.resetInspectViewport()
		m.setYaraStatus("YARA: unavailable for remote nodes", yaraStatusNotAvailable)
		m.inspect = true
		return nil
	}

	m.inspectInfo = buildProcessInspect(prompt.Connection, m.highlightPath)
	m.resetInspectViewport()
	m.setYaraStatus("", yaraStatusUnknown)
	m.inspect = true
	// trigger optional YARA scan
	if !settings.YaraEnabled {
		m.setYaraStatus("YARA: disabled", yaraStatusDisabled)
		return nil
	}
	if settings.YaraRuleDir == "" {
		m.setYaraStatus("YARA: rule dir not set", yaraStatusRuleDirMissing)
		return nil
	}
	if prompt.Connection.ProcessPath == "" {
		m.setYaraStatus("YARA: process path unknown", yaraStatusPathUnknown)
		return nil
	}
	if !yara.IsAvailable() {
		m.setYaraStatus("YARA: not available (build without -tags yara)", yaraStatusNotAvailable)
		return nil
	}
	m.yaraPending = true
	status := fmt.Sprintf("YARA: scanning %s", prompt.Connection.ProcessPath)
	m.setYaraStatus(status, yaraStatusScanning)
	return scanYaraCmd(prompt.ID, prompt.Connection.ProcessPath, settings.YaraRuleDir)
}

type yaraResultMsg struct {
	promptID string
	result   yara.Result
	err      error
}

func scanYaraCmd(promptID, path, rulesDir string) tea.Cmd {
	return func() tea.Msg {
		debug := os.Getenv("TUI_DEBUG_YARA") != ""
		if debug {
			log.Printf("[yara] scanning prompt=%s path=%s rules=%s", promptID, path, rulesDir)
		}
		res, err := yara.ScanFile(path, rulesDir)
		if debug {
			if err != nil {
				log.Printf("[yara] scan error: %v", err)
			} else {
				log.Printf("[yara] scan matches: %d", len(res.Matches))
			}
		}
		return yaraResultMsg{promptID: promptID, result: res, err: err}
	}
}

func (m *Model) resetInspectViewport() {
	cardW, innerW, innerH := m.computeInspectDimensions()
	_ = cardW // unused here, but ensures consistency
	m.inspectVP = viewport.New(innerW, innerH)
	m.inspectVP.YPosition = 1
	m.inspectXOffset = 0
	m.updateInspectContent()
}

func (m *Model) updateInspectContent() {
	content := renderInspectContent(m.inspectInfo, m.inspectXOffset, m.inspectVP.Width)
	m.inspectVP.SetContent(content)
}

// setYaraStatus rebuilds inspect info with a single YARA status line above the process tree.
func (m *Model) setYaraStatus(status string, kind yaraStatusKind) {
	m.yaraStatus = status
	m.yaraKind = kind
}

func (m *Model) insertInspectLinesBefore(pred func(string) bool, lines ...string) {
	existing := m.inspectInfo.Lines
	idx := len(existing)
	for i, line := range existing {
		if pred(line) {
			idx = i
			break
		}
	}
	newLines := make([]string, 0, len(existing)+len(lines))
	newLines = append(newLines, existing[:idx]...)
	newLines = append(newLines, lines...)
	newLines = append(newLines, existing[idx:]...)
	m.inspectInfo.Lines = newLines
	// recompute max width
	maxW := 0
	for _, line := range newLines {
		if w := util.RuneWidth(line); w > maxW {
			maxW = w
		}
	}
	m.inspectInfo.MaxWidth = maxW
	m.updateInspectContent()
}

func (m *Model) computeInspectDimensions() (cardWidth, innerWidth, innerHeight int) {
	maxCardWidth := min(m.width-2, 96)
	frameW, frameH := m.theme.Card.GetFrameSize()
	cardWidth = max(frameW+10, maxCardWidth)
	innerWidth = cardWidth - frameW
	// header + footer = 2 lines
	innerHeight = max(3, m.height-frameH-2)
	return
}

func (m *Model) adjustInspectX(delta int) {
	if delta == 0 {
		return
	}
	maxOffset := 0
	if m.inspectInfo.MaxWidth > m.inspectVP.Width {
		maxOffset = m.inspectInfo.MaxWidth - m.inspectVP.Width
	}
	newOffset := m.inspectXOffset + delta
	if newOffset < 0 {
		newOffset = 0
	}
	if newOffset > maxOffset {
		newOffset = maxOffset
	}
	if newOffset == m.inspectXOffset {
		return
	}
	m.inspectXOffset = newOffset
	m.updateInspectContent()
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

type processInspect struct {
	Lines    []string
	MaxWidth int
}

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
	if m.inspect {
		m.resetInspectViewport()
	}
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
	switch key := msg.(type) {
	case tea.KeyMsg:
		if m.inspect {
			// handle inspect UI scrolling
			switch key.String() {
			case "i", "esc":
				local := isLocalNode(snapshot.Nodes, prompt.NodeID)
				cmd := m.toggleInspect(prompt, snapshot.Settings, local)
				return cmd, true
			case "tab", "shift+tab":
				// let global tab navigation (view switching) work while inspecting
				return nil, false
			case "up", "pgup":
				m.inspectVP.LineUp(1)
				return nil, true
			case "down", "pgdown":
				m.inspectVP.LineDown(1)
				return nil, true
			case "left":
				m.adjustInspectX(-4)
				return nil, true
			case "right":
				m.adjustInspectX(4)
				return nil, true
			}
			return nil, true
		}
		switch key.String() {
		case "i":
			local := isLocalNode(snapshot.Nodes, prompt.NodeID)
			cmd := m.toggleInspect(prompt, snapshot.Settings, local)
			return cmd, true
		case "down":
			m.focus = (m.focus + 1) % 3
			return nil, true
		case "up":
			m.focus--
			if m.focus < 0 {
				m.focus = fieldTarget
			}
			return nil, true
		case "left":
			m.stepSelection(-1, form, len(targets))
			return nil, true
		case "right":
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
			if m.inspect {
				local := isLocalNode(snapshot.Nodes, prompt.NodeID)
				cmd := m.toggleInspect(prompt, snapshot.Settings, local)
				return cmd, true
			}
			m.submit(prompt, targets, form)
			return nil, true
		}
	case yaraResultMsg:
		if !m.inspect || key.promptID != m.activeID {
			return nil, false
		}
		m.yaraPending = false
		if key.err != nil {
			m.setYaraStatus(fmt.Sprintf("YARA: error: %v", key.err), yaraStatusError)
		} else if len(key.result.Matches) == 0 {
			m.setYaraStatus("YARA: no matches", yaraStatusNoMatches)
		} else {
			m.setYaraStatus(fmt.Sprintf("YARA: matches (%d)", len(key.result.Matches)), yaraStatusMatches)
			lines := []string{m.theme.Danger.Render("YARA matches:")}
			for _, match := range key.result.Matches {
				lines = append(lines, m.theme.Danger.Render(" - "+match.Rule))
			}
			m.insertInspectLinesBefore(func(line string) bool { return strings.HasPrefix(line, "Process Tree:") }, lines...)
		}
		return nil, true
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

	if m.inspect {
		pauseOnInspect := snapshot.Settings.PausePromptOnInspect
		cardW, innerW, innerH := m.computeInspectDimensions()
		if m.inspectVP.Width != innerW || m.inspectVP.Height != innerH {
			m.inspectVP.Width = innerW
			m.inspectVP.Height = innerH
			m.updateInspectContent()
		}
		statusLine := "[esc/i] back · scroll ↑/↓ ←/→"
		if pauseOnInspect {
			statusLine += " · countdown paused"
		} else {
			statusLine += " · countdown running"
		}
		header := []string{m.theme.Header.Render("Process inspection")}
		if m.inspectRoot {
			header = append(header, m.theme.Danger.Render("⚠ Root user"))
		}
		if m.yaraStatus != "" {
			style := m.theme.Subtle
			switch m.yaraKind {
			case yaraStatusScanning:
				style = m.theme.Warning
			case yaraStatusNoMatches:
				style = m.theme.Success
			case yaraStatusMatches, yaraStatusError, yaraStatusNotAvailable, yaraStatusRuleDirMissing, yaraStatusPathUnknown, yaraStatusTimeout:
				style = m.theme.Danger
			case yaraStatusDisabled:
				style = m.theme.Subtle
			}
			header = append(header, style.Render(m.yaraStatus))
		}
		body := lipgloss.JoinVertical(lipgloss.Left,
			strings.Join(header, "\n"),
			m.inspectVP.View(),
			m.theme.Subtle.Render(statusLine),
		)
		card := m.theme.Card.Width(cardW)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Top, card.Render(body))
	}

	headline := fmt.Sprintf("Connection prompt · %s · node %s", prompt.ID, prompt.NodeName)
	dest := prompt.Connection.DstHost
	if dest == "" {
		dest = prompt.Connection.DstIP
	}
	command := strings.Join(prompt.Connection.ProcessArgs, " ")
	info := []string{
		fmt.Sprintf("Process: %s", util.Fallback(prompt.Connection.ProcessPath, "unknown")),
		fmt.Sprintf("Command: %s", util.Fallback(command, "-")),
		fmt.Sprintf("Destination: %s:%d (%s)", util.Fallback(dest, "unknown"), prompt.Connection.DstPort, prompt.Connection.Protocol),
		fmt.Sprintf("User %d · PID %d", prompt.Connection.UserID, prompt.Connection.ProcessID),
	}

	actionRow := m.renderChoices("Action", mapActionLabels(actionOptions), form.action, m.focus == fieldAction)
	durationRow := m.renderChoices("Duration", mapDurationLabels(durationOptions), form.duration, m.focus == fieldDuration)
	targetRow := m.renderChoices("Target", mapTargetLabels(targets), form.target, m.focus == fieldTarget)

	controls := m.theme.Subtle.Render("↑/↓ move · ←/→ change · enter confirm · i inspect · [/] cycle prompts")
	expiresAt := prompt.ExpiresAt
	if expiresAt.IsZero() && !prompt.RequestedAt.IsZero() {
		timeout := snapshot.Settings.PromptTimeout
		if timeout <= 0 {
			timeout = fallbackPromptTimeout
		}
		expiresAt = prompt.RequestedAt.Add(timeout)
	}
	status := m.status
	if status == "" {
		if prompt.Paused {
			remaining := prompt.Remaining
			if remaining < 0 {
				remaining = 0
			}
			status = fmt.Sprintf("Timeout paused (%s left)", remaining.Round(time.Second))
		} else if !expiresAt.IsZero() {
			remaining := time.Until(expiresAt)
			if remaining < 0 {
				remaining = 0
			}
			status = fmt.Sprintf("Timeout in %s", remaining.Round(time.Second))
		}
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
		form.action = util.WrapIndex(form.action, delta, len(actionOptions))
	case fieldDuration:
		form.duration = util.WrapIndex(form.duration, delta, len(durationOptions))
	case fieldTarget:
		form.target = util.WrapIndex(form.target, delta, max(1, targets))
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
	m.promptIdx = util.WrapIndex(m.promptIdx, delta, len(snapshot.Prompts))
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

// (fallback, min, max replaced by util helpers and Go built-ins)
