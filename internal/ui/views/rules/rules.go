package rules

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/components/table"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/view"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/widget"
	"github.com/adamkadaban/opensnitch-tui/internal/util"
)

type Model struct {
	store      *state.Store
	theme      theme.Theme
	controller controller.RuleManager

	width  int
	height int

	nodeIdx       int
	ruleIdx       int
	tableOffset   int
	tableXOffset  int
	tableMaxWidth int

	statusLine string

	editing        bool
	editFocus      int
	editInputs     []textinput.Model
	editRuleName   string
	editActionIdx  int
	editDurIdx     int
	editNoLog      bool
	editPrecedence bool
}

const (
	defaultTableRows   = 5
	minTableRows       = 3
	maxTableRows       = 8
	tableChrome        = 8
	columnGap          = 1
	minCursorWidth     = 2
	minNameWidth       = 8
	minActionWidth     = 6
	minDurationWidth   = 8
	minStatusWidth     = 8
	minPrecedenceWidth = 10
	minNoLogWidth      = 6
	minOperatorWidth   = 14
)

const (
	editFieldDescription = iota
	editFieldAction
	editFieldDuration
	editFieldNoLog
	editFieldPrecedence
	editFieldCount
)

var editPlaceholders = []string{"", "allow|deny|ask", "always|once|until restart", "yes/no", "yes/no"}

var ruleActionOptions = []widget.Option{
	{Label: "Allow", Value: "allow"},
	{Label: "Deny", Value: "deny"},
	{Label: "Ask", Value: "ask"},
}

var ruleDurationOptions = []widget.Option{
	{Label: "Once", Value: "once"},
	{Label: "Until restart", Value: "until restart"},
	{Label: "Always", Value: "always"},
}

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
		if m.editing {
			switch key.Type {
			case tea.KeyEsc:
				m.cancelEdit()
				return m, nil
			case tea.KeyEnter:
				m.submitEdit(snapshot)
				return m, nil
			case tea.KeyTab:
				m.cycleEditFocus(1)
				return m, nil
			case tea.KeyShiftTab:
				m.cycleEditFocus(-1)
				return m, nil
			}
			switch key.String() {
			case "up":
				m.cycleEditFocus(-1)
				return m, nil
			case "down":
				m.cycleEditFocus(1)
				return m, nil
			case "left":
				m.adjustEditSelection(-1)
				return m, nil
			case "right":
				m.adjustEditSelection(1)
				return m, nil
			}
			var cmd tea.Cmd
			if m.editFocus == editFieldDescription && len(m.editInputs) > 0 {
				m.editInputs[0], cmd = m.editInputs[0].Update(msg)
			}
			return m, cmd
		}
		switch key.String() {
		case "left":
			m.adjustTableX(-4)
		case "right":
			m.adjustTableX(4)
		case "[":
			if m.nodeIdx > 0 {
				m.nodeIdx--
				m.ruleIdx = 0
				m.tableOffset = 0
				m.tableXOffset = 0
			}
		case "]":
			nodes := snapshot.Nodes
			if len(nodes) > 0 && m.nodeIdx < len(nodes)-1 {
				m.nodeIdx++
				m.ruleIdx = 0
				m.tableOffset = 0
				m.tableXOffset = 0
			}
		case "up":
			if m.ruleIdx > 0 {
				m.ruleIdx--
			}
		case "down":
			if _, rules, ok := m.current(snapshot); ok && m.ruleIdx < len(rules)-1 {
				m.ruleIdx++
			}
		case "e":
			m.requestToggle(snapshot, true)
		case "d":
			m.requestToggle(snapshot, false)
		case "x", "delete":
			m.requestDelete(snapshot)
		case "m":
			m.startEdit(snapshot)
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
	var content string
	if m.editing {
		content = m.renderEditModal(rules)
	} else {
		content = m.renderRuleDetail(rules)
	}
	status := m.renderStatus()

	body := lipgloss.JoinVertical(lipgloss.Left, header, table, content, status)
	return m.wrap(body)
}

func (m *Model) Title() string { return "Rules" }

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *Model) SetTheme(th theme.Theme) {
	m.theme = th
}

func (m *Model) renderNodes(snapshot state.Snapshot) string {
	nodes := snapshot.Nodes
	items := make([]string, 0, len(nodes))
	for idx, node := range nodes {
		label := fmt.Sprintf("%s (%d)", util.DisplayName(node), len(snapshot.Rules[node.ID]))
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
	capacity := m.tableCapacity()
	if start > len(rules)-capacity {
		start = max(0, len(rules)-capacity)
	}
	end := min(len(rules), start+capacity)
	moreBelow := end < len(rules)
	gap := strings.Repeat(" ", columnGap)
	rows := make([]string, 0, (end-start)+1)
	rows = append(rows, m.renderTableHeader(layout, gap))
	for idx := start; idx < end; idx++ {
		rule := rules[idx]
		rows = append(rows, m.renderRuleRow(layout, rule, idx, idx == m.ruleIdx, gap))
	}
	if moreBelow {
		tableWidth := layout.total() + columnGap*(layout.count()-1)
		rows = append(rows, table.RenderCaretRow(tableWidth, m.theme.Subtle))
	}
	// compute max width and apply horizontal slicing
	m.tableMaxWidth = table.ComputeMaxWidth(rows)
	visibleWidth := max(1, m.contentWidth())
	clipped := table.ClipRows(rows, m.tableXOffset, visibleWidth)
	return lipgloss.JoinVertical(lipgloss.Left, clipped...)
}

func (m *Model) renderTableHeader(layout tableLayout, gap string) string {
	headerStyle := m.theme.Header.Bold(true).Padding(0)
	labels := []string{"", "NAME", "ACTION", "DURATION", "STATUS", "PRECEDENCE", "NOLOG", "OPERATOR"}
	widths := []int{layout.cursor, layout.name, layout.action, layout.duration, layout.status, layout.precedence, layout.noLog, layout.operator}
	cells := make([]string, len(labels))
	for i := range labels {
		cells[i] = table.PadAndStyle(headerStyle, labels[i], widths[i], true)
	}
	return strings.Join(cells, gap)
}

func (m *Model) renderRuleRow(layout tableLayout, rule state.Rule, rowIdx int, selected bool, gap string) string {
	bg := m.rowStripeColor(rowIdx)
	if selected {
		bg = m.selectedRowColor()
	}
	cursor := " "
	if selected {
		cursor = ">"
	}
	cursorStyle := stripBackground(m.theme.Body).Background(bg).Padding(0)
	nameStyle := stripBackground(m.theme.Title).Background(bg).Padding(0)
	actionStyle := stripBackground(m.theme.Body).Background(bg).Padding(0)
	durationStyle := stripBackground(m.theme.Subtle).Background(bg).Padding(0)
	statusEnabled := stripBackground(m.theme.Success).Background(bg).Padding(0)
	statusDisabled := stripBackground(m.theme.Warning).Background(bg).Padding(0)
	flagStyle := stripBackground(m.theme.Body).Background(bg).Padding(0)
	operatorStyle := stripBackground(m.theme.Body).Background(bg).Padding(0)
	statusLabel := "disabled"
	statusStyle := statusDisabled
	if rule.Enabled {
		statusLabel = "enabled"
		statusStyle = statusEnabled
	}
	cells := []string{
		table.PadAndStyle(cursorStyle, cursor, layout.cursor, true),
		table.PadAndStyle(nameStyle, rule.Name, layout.name, true),
		table.PadAndStyle(actionStyle, rule.Action, layout.action, true),
		table.PadAndStyle(durationStyle, rule.Duration, layout.duration, true),
		table.PadAndStyle(statusStyle, statusLabel, layout.status, true),
		table.PadAndStyle(flagStyle, boolLabel(rule.Precedence), layout.precedence, true),
		table.PadAndStyle(flagStyle, boolLabel(rule.NoLog), layout.noLog, true),
		table.PadAndStyle(operatorStyle, describeOperator(rule.Operator), layout.operator, false),
	}
	gapStyle := lipgloss.NewStyle().Background(bg)
	rowGap := gapStyle.Render(gap)
	return strings.Join(cells, rowGap)
}

func (m *Model) renderRuleDetail(rules []state.Rule) string {
	if len(rules) == 0 {
		return ""
	}
	rule := rules[min(m.ruleIdx, len(rules)-1)]
	inner := max(20, m.contentWidth())
	fmtLine := func(label, value string) string {
		line := fmt.Sprintf("%s: %s", label, value)
		return util.TruncateString(line, inner)
	}
	desc := util.Fallback(rule.Description, "NONE")
	created := "unknown"
	if !rule.CreatedAt.IsZero() {
		created = rule.CreatedAt.UTC().Format(time.RFC3339)
	}
	lines := []string{
		fmtLine("Name", util.Fallback(rule.Name, "-")),
		fmtLine("Node", util.Fallback(rule.NodeID, "-")),
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

func (m *Model) renderEditModal(rules []state.Rule) string {
	name := ""
	if len(rules) > 0 && m.ruleIdx < len(rules) {
		name = rules[m.ruleIdx].Name
	}
	header := m.theme.Header.Render(fmt.Sprintf("Modify rule %s", util.Fallback(name, "-")))
	rows := []string{
		m.renderEditInput("Description", m.editInputs, m.editFocus == editFieldDescription),
		m.renderEditRow("Action", ruleActionOptions, m.editActionIdx, m.editFocus == editFieldAction),
		m.renderEditRow("Duration", ruleDurationOptions, m.editDurIdx, m.editFocus == editFieldDuration),
		m.renderEditToggle("NoLog", m.editNoLog, m.editFocus == editFieldNoLog),
		m.renderEditToggle("Precedence", m.editPrecedence, m.editFocus == editFieldPrecedence),
	}
	body := strings.Join(rows, "\n")
	return m.theme.Body.Render(fmt.Sprintf("%s\n%s", header, body))
}

func (m *Model) renderEditInput(label string, inputs []textinput.Model, focused bool) string {
	if len(inputs) == 0 {
		return fmt.Sprintf("%s: -", label)
	}
	ti := inputs[0]
	if focused {
		ti.Prompt = m.theme.Warning.Render("> ")
	} else {
		ti.Prompt = "  "
	}
	return fmt.Sprintf("%s: %s", label, ti.View())
}

func (m *Model) renderEditToggle(label string, enabled bool, focused bool) string {
	return widget.RenderToggle(m.theme, label, enabled, focused)
}

func (m *Model) renderEditRow(label string, opts []widget.Option, selected int, focused bool) string {
	return widget.RenderOptionRow(m.theme, label, opts, selected, focused)
}

func (m *Model) startEdit(snapshot state.Snapshot) {
	_, rules, ok := m.current(snapshot)
	if !ok || len(rules) == 0 {
		return
	}
	if m.controller == nil {
		m.statusLine = m.theme.Danger.Render("Rules controller unavailable")
		return
	}
	rule := rules[min(m.ruleIdx, len(rules)-1)]
	inputs := make([]textinput.Model, 1)
	desc := textinput.New()
	desc.Placeholder = editPlaceholders[editFieldDescription]
	desc.CharLimit = 0
	desc.Width = 40
	desc.SetValue(rule.Description)
	desc.Focus()
	inputs[0] = desc
	m.editInputs = inputs
	m.editFocus = editFieldDescription
	m.editRuleName = rule.Name
	m.editActionIdx = widget.IndexOf(ruleActionOptions, strings.ToLower(rule.Action))
	m.editDurIdx = widget.IndexOf(ruleDurationOptions, strings.ToLower(rule.Duration))
	m.editNoLog = rule.NoLog
	m.editPrecedence = rule.Precedence
	m.editing = true
}

func (m *Model) cancelEdit() {
	m.editing = false
	m.editInputs = nil
	m.editRuleName = ""
	m.editActionIdx = 0
	m.editDurIdx = 0
	m.editNoLog = false
	m.editPrecedence = false
}

func (m *Model) cycleEditFocus(delta int) {
	if editFieldCount == 0 {
		return
	}
	if m.editFocus == editFieldDescription && len(m.editInputs) > 0 {
		m.editInputs[0].Blur()
	}
	m.editFocus = (m.editFocus + delta) % editFieldCount
	if m.editFocus < 0 {
		m.editFocus += editFieldCount
	}
	if m.editFocus == editFieldDescription && len(m.editInputs) > 0 {
		m.editInputs[0].Focus()
	}
}

func (m *Model) adjustEditSelection(delta int) {
	if delta == 0 {
		return
	}
	switch m.editFocus {
	case editFieldAction:
		m.editActionIdx = util.WrapIndex(m.editActionIdx, delta, len(ruleActionOptions))
	case editFieldDuration:
		m.editDurIdx = util.WrapIndex(m.editDurIdx, delta, len(ruleDurationOptions))
	case editFieldNoLog:
		m.editNoLog = !m.editNoLog
	case editFieldPrecedence:
		m.editPrecedence = !m.editPrecedence
	}
}

func (m *Model) submitEdit(snapshot state.Snapshot) {
	node, rules, ok := m.current(snapshot)
	if !ok || len(rules) == 0 {
		return
	}
	if m.controller == nil {
		m.statusLine = m.theme.Danger.Render("Rules controller unavailable")
		return
	}
	if len(ruleActionOptions) == 0 {
		m.statusLine = m.theme.Danger.Render("No action options configured")
		return
	}
	if len(ruleDurationOptions) == 0 {
		m.statusLine = m.theme.Danger.Render("No duration options configured")
		return
	}
	var rule state.Rule
	for _, r := range rules {
		if r.Name == m.editRuleName {
			rule = r
			break
		}
	}
	if rule.Name == "" {
		m.statusLine = m.theme.Danger.Render("Rule not found")
		return
	}
	desc := ""
	if len(m.editInputs) > 0 {
		desc = strings.TrimSpace(m.editInputs[0].Value())
	}
	rule.Description = desc
	actIdx := util.WrapIndex(m.editActionIdx, 0, len(ruleActionOptions))
	durIdx := util.WrapIndex(m.editDurIdx, 0, len(ruleDurationOptions))
	rule.Action = ruleActionOptions[actIdx].Value
	rule.Duration = ruleDurationOptions[durIdx].Value
	rule.NoLog = m.editNoLog
	rule.Precedence = m.editPrecedence
	if rule.NodeID == "" {
		rule.NodeID = node.ID
	}
	err := m.controller.ChangeRule(rule.NodeID, rule)
	m.renderActionResult(err, "change", node, rule)
	if err == nil {
		m.cancelEdit()
	}
}

func (m *Model) renderStatus() string {
	var help string
	if m.editing {
		help = "esc cancel · enter save · tab/shift+tab · ←/→ change"
	} else {
		help = "←/→ scroll · [/] nodes · ↑/↓ rules · e enable · d disable · x delete · m modify"
	}
	helpRendered := m.theme.Subtle.Render(help)
	if m.statusLine == "" {
		return helpRendered
	}
	return fmt.Sprintf("%s\n%s", m.statusLine, helpRendered)
}

func (m *Model) wrap(body string) string {
	return m.theme.Body.Width(max(1, m.width)).Height(max(5, m.height)).Render(body)
}

func (m *Model) tableCapacity() int {
	if m.height <= 0 {
		return defaultTableRows
	}
	capacity := m.height - tableChrome
	if capacity < minTableRows {
		capacity = minTableRows
	}
	if capacity > maxTableRows {
		capacity = maxTableRows
	}
	return capacity
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

func (m *Model) adjustTableX(delta int) {
	if delta == 0 {
		return
	}
	maxOffset := 0
	visible := m.contentWidth()
	if m.tableMaxWidth > visible {
		maxOffset = m.tableMaxWidth - visible
	}
	newOffset := m.tableXOffset + delta
	if newOffset < 0 {
		newOffset = 0
	}
	if newOffset > maxOffset {
		newOffset = maxOffset
	}
	m.tableXOffset = newOffset
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
	capacity := m.tableCapacity()
	if len(rules) <= capacity {
		m.tableOffset = 0
		return
	}
	if m.ruleIdx < m.tableOffset {
		m.tableOffset = m.ruleIdx
	}
	if m.ruleIdx >= m.tableOffset+capacity {
		m.tableOffset = m.ruleIdx - capacity + 1
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
		m.statusLine = m.theme.Danger.Render(fmt.Sprintf("Failed to %s %s on %s: %v", action, rule.Name, util.DisplayName(node), err))
		return
	}
	m.statusLine = m.theme.Success.Render(fmt.Sprintf("Requested %s %s on %s", action, rule.Name, util.DisplayName(node)))
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

func stripBackground(style lipgloss.Style) lipgloss.Style {
	return style.UnsetBackground()
}

func boolLabel(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func (m *Model) rowStripeColor(rowIdx int) lipgloss.Color {
	if rowIdx%2 == 0 {
		return m.theme.TableRowEven
	}
	return m.theme.TableRowOdd
}

func (m *Model) selectedRowColor() lipgloss.Color {
	return m.theme.TableRowSelect
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
