package rules

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/view"
)

type Model struct {
	store      *state.Store
	theme      theme.Theme
	controller controller.RuleManager

	width  int
	height int

	nodeIdx     int
	ruleIdx     int
	tableOffset int

	statusLine string
}

const (
	defaultTableRows   = 5
	minTableRows       = 3
	maxTableRows       = 8
	tableChrome        = 8
	columnGap          = 1
	minCursorWidth     = 2
	minNameWidth       = 18
	minActionWidth     = 6
	minDurationWidth   = 8
	minStatusWidth     = 8
	minPrecedenceWidth = 10
	minNoLogWidth      = 6
	minOperatorWidth   = 14
)

type tableLayout struct {
	cursor     int
	name       int
	action     int
	duration   int
	status     int
	precedence int
	noLog      int
	operator   int
}

func (tl tableLayout) total() int {
	return tl.cursor + tl.name + tl.action + tl.duration + tl.status + tl.precedence + tl.noLog + tl.operator
}

func (tl tableLayout) count() int { return 8 }

func New(store *state.Store, th theme.Theme, ctrl controller.RuleManager) view.Model {
	return &Model{store: store, theme: th, controller: ctrl}
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	snapshot := m.store.Snapshot()
	m.clampSelection(snapshot)

	switch key := msg.(type) {
	case tea.KeyMsg:
		switch key.String() {
		case "left", "h":
			if m.nodeIdx > 0 {
				m.nodeIdx--
				m.ruleIdx = 0
				m.tableOffset = 0
			}
		case "right", "l":
			nodes := snapshot.Nodes
			if len(nodes) > 0 && m.nodeIdx < len(nodes)-1 {
				m.nodeIdx++
				m.ruleIdx = 0
				m.tableOffset = 0
			}
		case "up", "k":
			if m.ruleIdx > 0 {
				m.ruleIdx--
			}
		case "down", "j":
			if _, rules, ok := m.current(snapshot); ok && m.ruleIdx < len(rules)-1 {
				m.ruleIdx++
			}
		case "e":
			m.requestToggle(snapshot, true)
		case "d":
			m.requestToggle(snapshot, false)
		case "x", "delete":
			m.requestDelete(snapshot)
		}
	}

	return m, nil
}

func (m *Model) View() string {
	snapshot := m.store.Snapshot()
	m.clampSelection(snapshot)

	nodes := snapshot.Nodes
	if len(nodes) == 0 {
		msg := m.theme.Subtle.Render("No nodes connected. Awaiting daemon subscriptions.")
		return m.wrap(msg)
	}

	_, rules, ok := m.current(snapshot)
	if !ok {
		msg := m.theme.Subtle.Render("Select a node to view its rules.")
		return m.wrap(msg)
	}

	header := m.renderNodes(snapshot)
	table := m.renderRulesTable(rules)
	detail := m.renderRuleDetail(rules)
	status := m.renderStatus()

	body := lipgloss.JoinVertical(lipgloss.Left, header, table, detail, status)
	return m.wrap(body)
}

func (m *Model) Title() string { return "Rules" }

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *Model) renderNodes(snapshot state.Snapshot) string {
	nodes := snapshot.Nodes
	items := make([]string, 0, len(nodes))
	for idx, node := range nodes {
		label := fmt.Sprintf("%s (%d)", displayName(node), len(snapshot.Rules[node.ID]))
		items = append(items, m.theme.RenderTab(label, idx == m.nodeIdx))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, items...)
}

func (m *Model) renderRulesTable(rules []state.Rule) string {
	if len(rules) == 0 {
		return m.theme.Subtle.Render("No rules reported for this node.")
	}
	layout := m.tableColumns()
	start := min(m.tableOffset, max(0, len(rules)-1))
	cap := m.tableCapacity()
	if start > len(rules)-cap {
		start = max(0, len(rules)-cap)
	}
	end := min(len(rules), start+cap)
	gap := strings.Repeat(" ", columnGap)
	rows := make([]string, 0, (end-start)+1)
	rows = append(rows, m.renderTableHeader(layout, gap))
	for idx := start; idx < end; idx++ {
		rule := rules[idx]
		rows = append(rows, m.renderRuleRow(layout, rule, idx == m.ruleIdx, gap))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (m *Model) renderTableHeader(layout tableLayout, gap string) string {
	headerStyle := m.theme.Header.Copy().Bold(true).Padding(0)
	labels := []string{"", "NAME", "ACTION", "DURATION", "STATUS", "PRECEDENCE", "NOLOG", "OPERATOR"}
	widths := []int{layout.cursor, layout.name, layout.action, layout.duration, layout.status, layout.precedence, layout.noLog, layout.operator}
	cells := make([]string, len(labels))
	for i := range labels {
		cells[i] = padAndStyle(headerStyle, labels[i], widths[i])
	}
	return strings.Join(cells, gap)
}

func (m *Model) renderRuleRow(layout tableLayout, rule state.Rule, selected bool, gap string) string {
	cursor := " "
	if selected {
		cursor = ">"
	}
	cursorStyle := m.theme.Body.Copy().Padding(0)
	nameStyle := m.theme.Title.Copy().Padding(0)
	actionStyle := m.theme.Body.Copy().Padding(0)
	durationStyle := m.theme.Subtle.Copy().Padding(0)
	statusEnabled := m.theme.Success.Copy().Padding(0)
	statusDisabled := m.theme.Warning.Copy().Padding(0)
	flagStyle := m.theme.Body.Copy().Padding(0)
	operatorStyle := m.theme.Body.Copy().Padding(0)
	statusLabel := "disabled"
	statusStyle := statusDisabled
	if rule.Enabled {
		statusLabel = "enabled"
		statusStyle = statusEnabled
	}
	cells := []string{
		padAndStyle(cursorStyle, cursor, layout.cursor),
		padAndStyle(nameStyle, rule.Name, layout.name),
		padAndStyle(actionStyle, rule.Action, layout.action),
		padAndStyle(durationStyle, rule.Duration, layout.duration),
		padAndStyle(statusStyle, statusLabel, layout.status),
		padAndStyle(flagStyle, boolLabel(rule.Precedence), layout.precedence),
		padAndStyle(flagStyle, boolLabel(rule.NoLog), layout.noLog),
		padAndStyle(operatorStyle, describeOperator(rule.Operator), layout.operator),
	}
	return strings.Join(cells, gap)
}

func (m *Model) renderRuleDetail(rules []state.Rule) string {
	if len(rules) == 0 {
		return ""
	}
	rule := rules[min(m.ruleIdx, len(rules)-1)]
	inner := max(20, m.contentWidth())
	fmtLine := func(label, value string) string {
		line := fmt.Sprintf("%s: %s", label, value)
		return truncateString(line, inner)
	}
	desc := fallback(rule.Description, "NONE")
	created := "unknown"
	if !rule.CreatedAt.IsZero() {
		created = rule.CreatedAt.UTC().Format(time.RFC3339)
	}
	lines := []string{
		fmtLine("Name", fallback(rule.Name, "-")),
		fmtLine("Node", fallback(rule.NodeID, "-")),
		fmtLine("Description", desc),
		fmtLine("Action", colorRuleAction(m.theme, rule.Action)),
		fmtLine("Duration", colorDuration(m.theme, rule.Duration)),
		fmtLine("Enabled", colorBool(m.theme, rule.Enabled)),
		fmtLine("Precedence", colorBool(m.theme, rule.Precedence)),
		fmtLine("NoLog", colorBool(m.theme, rule.NoLog)),
		fmtLine("Created", created),
		fmtLine("Operator", describeOperator(rule.Operator)),
	}
	return m.theme.Body.Render(strings.Join(lines, "\n"))
}

func (m *Model) renderStatus() string {
	help := m.theme.Subtle.Render("←/→ nodes · ↑/↓ rules · e enable · d disable · x delete")
	if m.statusLine == "" {
		return help
	}
	return fmt.Sprintf("%s\n%s", m.statusLine, help)
}

func (m *Model) wrap(body string) string {
	return m.theme.Body.Copy().Width(m.width).Height(max(5, m.height)).Render(body)
}

func (m *Model) tableCapacity() int {
	if m.height <= 0 {
		return defaultTableRows
	}
	cap := m.height - tableChrome
	if cap < minTableRows {
		cap = minTableRows
	}
	if cap > maxTableRows {
		cap = maxTableRows
	}
	return cap
}

func (m *Model) tableColumns() tableLayout {
	layout := tableLayout{
		cursor:     minCursorWidth,
		name:       minNameWidth,
		action:     minActionWidth,
		duration:   minDurationWidth,
		status:     minStatusWidth,
		precedence: minPrecedenceWidth,
		noLog:      minNoLogWidth,
		operator:   minOperatorWidth,
	}
	inner := max(40, m.contentWidth())
	gapWidth := columnGap * (layout.count() - 1)
	usable := inner - gapWidth
	if usable <= 0 {
		usable = layout.total()
	}
	base := layout.total()
	if usable < base {
		deficit := base - usable
		reducers := []struct {
			field *int
			min   int
		}{
			{&layout.operator, 6},
			{&layout.name, 10},
			{&layout.action, 6},
			{&layout.duration, 6},
			{&layout.status, 6},
		}
		for deficit > 0 {
			progressed := false
			for i := range reducers {
				if deficit == 0 {
					break
				}
				current := *reducers[i].field
				if current <= reducers[i].min {
					continue
				}
				delta := min(deficit, current-reducers[i].min)
				*reducers[i].field -= delta
				deficit -= delta
				progressed = true
			}
			if !progressed {
				break
			}
		}
		if deficit > 0 {
			catchAll := []*int{&layout.operator, &layout.name, &layout.action, &layout.duration, &layout.status, &layout.precedence, &layout.noLog}
			for deficit > 0 {
				progressed := false
				for _, field := range catchAll {
					if deficit == 0 {
						break
					}
					if *field <= 1 {
						continue
					}
					*field--
					deficit--
					progressed = true
				}
				if !progressed {
					break
				}
			}
		}
	} else if usable > base {
		extra := usable - base
		expanders := []*int{&layout.name, &layout.operator}
		for extra > 0 {
			for _, field := range expanders {
				if extra == 0 {
					break
				}
				*field++
				extra--
			}
		}
	}
	layout.cursor = max(1, layout.cursor)
	layout.name = max(6, layout.name)
	layout.action = max(4, layout.action)
	layout.duration = max(4, layout.duration)
	layout.status = max(4, layout.status)
	layout.precedence = max(minPrecedenceWidth, layout.precedence)
	layout.noLog = max(minNoLogWidth, layout.noLog)
	layout.operator = max(4, layout.operator)
	return layout
}

func (m *Model) contentWidth() int {
	if m.width <= 0 {
		return 80
	}
	if m.width <= 4 {
		return m.width
	}
	return m.width - 4
}

func padAndStyle(style lipgloss.Style, text string, width int) string {
	if width <= 0 {
		return ""
	}
	content := padString(truncateString(text, width), width)
	return style.Render(content)
}

func padString(value string, width int) string {
	padding := width - len([]rune(value))
	if padding > 0 {
		return value + strings.Repeat(" ", padding)
	}
	return value
}

func truncateString(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width <= 3 {
		return string(runes[:width])
	}
	return string(runes[:width-3]) + "..."
}

func (m *Model) clampSelection(snapshot state.Snapshot) {
	nodes := snapshot.Nodes
	if len(nodes) == 0 {
		m.nodeIdx = 0
		m.ruleIdx = 0
		m.tableOffset = 0
		return
	}
	if m.nodeIdx >= len(nodes) {
		m.nodeIdx = len(nodes) - 1
		m.ruleIdx = 0
		m.tableOffset = 0
	}
	rules := snapshot.Rules[nodes[m.nodeIdx].ID]
	if len(rules) == 0 {
		m.ruleIdx = 0
		m.tableOffset = 0
		return
	}
	if m.ruleIdx >= len(rules) {
		m.ruleIdx = len(rules) - 1
	}
	cap := m.tableCapacity()
	if len(rules) <= cap {
		m.tableOffset = 0
		return
	}
	if m.ruleIdx < m.tableOffset {
		m.tableOffset = m.ruleIdx
	}
	if m.ruleIdx >= m.tableOffset+cap {
		m.tableOffset = m.ruleIdx - cap + 1
	}
}

func (m *Model) current(snapshot state.Snapshot) (state.Node, []state.Rule, bool) {
	nodes := snapshot.Nodes
	if len(nodes) == 0 {
		return state.Node{}, nil, false
	}
	node := nodes[min(m.nodeIdx, len(nodes)-1)]
	rules := snapshot.Rules[node.ID]
	return node, rules, true
}

func (m *Model) requestToggle(snapshot state.Snapshot, enable bool) {
	node, rules, ok := m.current(snapshot)
	if !ok || len(rules) == 0 {
		return
	}
	rule := rules[min(m.ruleIdx, len(rules)-1)]
	if m.controller == nil {
		m.statusLine = m.theme.Danger.Render("Rules controller unavailable")
		return
	}
	var err error
	var verb string
	if enable {
		verb = "enable"
		err = m.controller.EnableRule(node.ID, rule.Name)
	} else {
		verb = "disable"
		err = m.controller.DisableRule(node.ID, rule.Name)
	}
	m.renderActionResult(err, verb, node, rule)
}

func (m *Model) requestDelete(snapshot state.Snapshot) {
	node, rules, ok := m.current(snapshot)
	if !ok || len(rules) == 0 {
		return
	}
	if m.controller == nil {
		m.statusLine = m.theme.Danger.Render("Rules controller unavailable")
		return
	}
	rule := rules[min(m.ruleIdx, len(rules)-1)]
	err := m.controller.DeleteRule(node.ID, rule.Name)
	if err == nil && m.ruleIdx >= len(rules)-1 {
		m.ruleIdx = max(0, m.ruleIdx-1)
	}
	m.renderActionResult(err, "delete", node, rule)
}

func (m *Model) renderActionResult(err error, action string, node state.Node, rule state.Rule) {
	if err != nil {
		m.statusLine = m.theme.Danger.Render(fmt.Sprintf("Failed to %s %s on %s: %v", action, rule.Name, displayName(node), err))
		return
	}
	m.statusLine = m.theme.Success.Render(fmt.Sprintf("Requested %s %s on %s", action, rule.Name, displayName(node)))
}

func describeOperator(op state.RuleOperator) string {
	if op.Type == "" && op.Operand == "" && op.Data == "" && len(op.Children) == 0 {
		return "-"
	}
	parts := []string{op.Type}
	if op.Operand != "" {
		parts = append(parts, op.Operand)
	}
	if op.Data != "" {
		parts = append(parts, op.Data)
	}
	if len(op.Children) > 0 {
		childParts := make([]string, len(op.Children))
		for i, child := range op.Children {
			childParts[i] = describeOperator(child)
		}
		parts = append(parts, fmt.Sprintf("[%s]", strings.Join(childParts, ", ")))
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func displayName(node state.Node) string {
	if node.Name != "" {
		return node.Name
	}
	return node.Address
}

func fallback(value, def string) string {
	if value == "" {
		return def
	}
	return value
}

func boolLabel(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func colorRuleAction(th theme.Theme, action string) string {
	if action == "" {
		return "-"
	}
	switch strings.ToLower(action) {
	case "allow":
		return th.Success.Render("allow")
	case "deny", "drop", "block":
		return th.Danger.Render(strings.ToLower(action))
	case "ask":
		return th.Warning.Render("ask")
	default:
		return th.Body.Render(strings.ToLower(action))
	}
}

func colorBool(th theme.Theme, v bool) string {
	if v {
		return th.Success.Render("true")
	}
	return th.Warning.Render("false")
}

func colorDuration(th theme.Theme, duration string) string {
	if duration == "" {
		return "-"
	}
	switch strings.ToLower(duration) {
	case "always", "forever", "permanent":
		return th.Success.Render(duration)
	case "once", "ask":
		return th.Warning.Render(duration)
	case "until restart", "session", "temporary":
		return th.Subtle.Render(duration)
	default:
		return th.Body.Render(duration)
	}
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
