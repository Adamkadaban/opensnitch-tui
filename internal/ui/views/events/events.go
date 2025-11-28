package events

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/components/table"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/view"
	"github.com/adamkadaban/opensnitch-tui/internal/util"
)

// Model renders recent events in a table styled like the Rules view.
type Model struct {
	store *state.Store
	theme theme.Theme

	width  int
	height int

	rowIdx        int
	tableOffset   int
	tableXOffset  int
	tableMaxWidth int
}

const (
	defaultTableRows = 5
	minTableRows     = 3
	maxTableRows     = 8
	tableChrome      = 12
	columnGap        = 1
	minCursorWidth   = 2
	minTimeWidth     = 20
	minActionWidth   = 6
	minDstIPWidth    = 12
	minDstHostWidth  = 14
	minProtoWidth    = 5
	minProcessWidth  = 12
	minCmdlineWidth  = 12
	minRuleWidth     = 10
)

type tableLayout struct {
	cursor  int
	time    int
	action  int
	dstIP   int
	dstHost int
	proto   int
	process int
	cmdline int
	rule    int
}

func (tl tableLayout) total() int {
	return tl.cursor + tl.time + tl.action + tl.dstIP + tl.dstHost + tl.proto + tl.process + tl.cmdline + tl.rule
}

func (tl tableLayout) count() int { return 9 }

func New(store *state.Store, th theme.Theme) view.Model {
	return &Model{store: store, theme: th}
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	snapshot := m.store.Snapshot()
	m.clampSelection(snapshot)

	switch key := msg.(type) {
	case tea.KeyMsg:
		switch key.String() {
		case "left":
			m.adjustTableX(-4)
		case "right":
			m.adjustTableX(4)
		case "up":
			if m.rowIdx > 0 {
				m.rowIdx--
			}
		case "down":
			if m.rowIdx < len(snapshot.Stats.Events)-1 {
				m.rowIdx++
			}
		case "pgup":
			m.rowIdx -= m.tableCapacity()
			if m.rowIdx < 0 {
				m.rowIdx = 0
			}
		case "pgdown":
			m.rowIdx += m.tableCapacity()
			if m.rowIdx >= len(snapshot.Stats.Events) {
				m.rowIdx = max(0, len(snapshot.Stats.Events)-1)
			}
		case "home", "g":
			m.rowIdx = 0
		case "end", "G":
			if n := len(snapshot.Stats.Events); n > 0 {
				m.rowIdx = n - 1
			}
		}
	}

	return m, nil
}

func (m *Model) View() string {
	snapshot := m.store.Snapshot()
	m.clampSelection(snapshot)

	events := snapshot.Stats.Events
	if len(events) == 0 {
		msg := m.theme.Subtle.Render("No events yet.")
		return m.wrap(msg)
	}

	table := m.renderEventsTable(events)
	detail := m.renderEventDetail(snapshot)
	status := m.renderStatus()
	body := lipgloss.JoinVertical(lipgloss.Left, table, detail, status)
	return m.wrap(body)
}

func (m *Model) Title() string { return "Events" }

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *Model) SetTheme(th theme.Theme) {
	m.theme = th
}

func (m *Model) renderEventsTable(events []state.Event) string {
	layout := m.tableColumns()
	start := min(m.tableOffset, max(0, len(events)-1))
	capacity := m.tableCapacity()
	if start > len(events)-capacity {
		start = max(0, len(events)-capacity)
	}
	end := min(len(events), start+capacity)
	moreBelow := end < len(events)
	gap := strings.Repeat(" ", columnGap)

	rows := make([]string, 0, (end-start)+1)
	rows = append(rows, m.renderTableHeader(layout, gap))
	for idx := start; idx < end; idx++ {
		ev := eventAt(events, idx)
		rows = append(rows, m.renderEventRow(layout, ev, idx, idx == m.rowIdx, gap))
	}
	if moreBelow {
		tableWidth := layout.total() + columnGap*(layout.count()-1)
		rows = append(rows, table.RenderCaretRow(tableWidth, m.theme.Subtle))
	}

	m.tableMaxWidth = table.ComputeMaxWidth(rows)

	visibleWidth := max(1, m.contentWidth())
	clipped := table.ClipRows(rows, m.tableXOffset, visibleWidth)
	return lipgloss.JoinVertical(lipgloss.Left, clipped...)
}

func (m *Model) renderEventDetail(snapshot state.Snapshot) string {
	events := snapshot.Stats.Events
	if len(events) == 0 {
		return ""
	}
	ev := eventAt(events, m.rowIdx)
	inner := max(20, m.contentWidth())
	fmtLine := func(label, value string) string {
		line := fmt.Sprintf("%s: %s", label, value)
		return util.TruncateString(line, inner)
	}

	nodeLabel := findNodeLabel(snapshot.Nodes, ev.NodeID)
	lines := []string{
		fmtLine("Time", formatEventTime(ev)),
		fmtLine("Node", nodeLabel),
		fmtLine("Action", formatEventAction(ev)),
		fmtLine("Protocol", util.Fallback(ev.Connection.Protocol, "-")),
		fmtLine("Src", formatEndpoint(ev.Connection.SrcIP, ev.Connection.SrcPort)),
		fmtLine("Dst", formatEndpoint(ev.Connection.DstIP, ev.Connection.DstPort)),
		fmtLine("DstHost", util.Fallback(ev.Connection.DstHost, "-")),
		fmtLine("Process", util.Fallback(ev.Connection.ProcessPath, "-")),
		fmtLine("PID/UID", formatPIDUID(ev.Connection.ProcessID, ev.Connection.UserID)),
		fmtLine("Args", formatCmdline(ev)),
		fmtLine("CWD", util.Fallback(ev.Connection.ProcessCWD, "-")),
		fmtLine("Rule", util.Fallback(ev.Rule.Name, "-")),
	}
	if cs := formatChecksums(ev.Connection.ProcessChecksums); cs != "-" {
		lines = append(lines, fmtLine("Checksums", cs))
	}
	return m.theme.Body.Render(strings.Join(lines, "\n"))
}

func (m *Model) renderTableHeader(layout tableLayout, gap string) string {
	headerStyle := m.theme.Header.Bold(true).Padding(0)
	labels := []string{"", "TIME", "ACTION", "DSTIP", "DSTHOST", "PROTO", "PROCESS", "CMDLINE", "RULE"}
	widths := []int{layout.cursor, layout.time, layout.action, layout.dstIP, layout.dstHost, layout.proto, layout.process, layout.cmdline, layout.rule}
	cells := make([]string, len(labels))
	for i := range labels {
		cells[i] = table.PadAndStyle(headerStyle, labels[i], widths[i], true)
	}
	return strings.Join(cells, gap)
}

func (m *Model) renderEventRow(layout tableLayout, ev state.Event, rowIdx int, selected bool, gap string) string {
	bg := m.rowStripeColor(rowIdx)
	if selected {
		bg = m.selectedRowColor()
	}
	cursor := " "
	if selected {
		cursor = ">"
	}

	cursorStyle := stripBackground(m.theme.Body).Background(bg).Padding(0)
	timeStyle := stripBackground(m.theme.Title).Background(bg).Padding(0)
	actionStyle := stripBackground(m.theme.Body).Background(bg).Padding(0)
	dstIPStyle := stripBackground(m.theme.Body).Background(bg).Padding(0)
	dstHostStyle := stripBackground(m.theme.Body).Background(bg).Padding(0)
	protoStyle := stripBackground(m.theme.Body).Background(bg).Padding(0)
	processStyle := stripBackground(m.theme.Body).Background(bg).Padding(0)
	cmdlineStyle := stripBackground(m.theme.Body).Background(bg).Padding(0)
	ruleStyle := stripBackground(m.theme.Body).Background(bg).Padding(0)

	columns := []string{
		table.PadAndStyle(cursorStyle, cursor, layout.cursor, true),
		table.PadAndStyle(timeStyle, formatEventTime(ev), layout.time, true),
		table.PadAndStyle(actionStyle, formatEventAction(ev), layout.action, true),
		table.PadAndStyle(dstIPStyle, util.Fallback(ev.Connection.DstIP, "-"), layout.dstIP, true),
		table.PadAndStyle(dstHostStyle, util.Fallback(ev.Connection.DstHost, "-"), layout.dstHost, true),
		table.PadAndStyle(protoStyle, util.Fallback(ev.Connection.Protocol, "-"), layout.proto, true),
		table.PadAndStyle(processStyle, formatProcess(ev), layout.process, true),
		table.PadAndStyle(cmdlineStyle, formatCmdline(ev), layout.cmdline, true),
		table.PadAndStyle(ruleStyle, util.Fallback(ev.Rule.Name, "-"), layout.rule, true),
	}

	gapStyle := lipgloss.NewStyle().Background(bg)
	rowGap := gapStyle.Render(gap)
	return strings.Join(columns, rowGap)
}

func eventAt(events []state.Event, displayIdx int) state.Event {
	if len(events) == 0 {
		return state.Event{}
	}
	idx := len(events) - 1 - displayIdx
	if idx < 0 || idx >= len(events) {
		return state.Event{}
	}
	return events[idx]
}

func formatEventTime(ev state.Event) string {
	if ev.UnixNano != 0 {
		return time.Unix(0, ev.UnixNano).UTC().Format(time.RFC3339)
	}
	if ev.Time != "" {
		return ev.Time
	}
	return "unknown"
}

func formatEventAction(ev state.Event) string {
	if ev.Rule.Action != "" {
		return ev.Rule.Action
	}
	return "-"
}

func formatProcess(ev state.Event) string {
	if ev.Connection.ProcessPath != "" {
		return ev.Connection.ProcessPath
	}
	return "-"
}

func formatCmdline(ev state.Event) string {
	if len(ev.Connection.ProcessArgs) == 0 {
		return "-"
	}
	return strings.Join(ev.Connection.ProcessArgs, " ")
}

func formatEndpoint(ip string, port uint32) string {
	if ip == "" && port == 0 {
		return "-"
	}
	if port > 0 && ip != "" {
		return fmt.Sprintf("%s:%d", ip, port)
	}
	if port > 0 {
		return fmt.Sprintf(":%d", port)
	}
	return ip
}

func formatPIDUID(pid, uid uint32) string {
	if pid == 0 && uid == 0 {
		return "-"
	}
	return fmt.Sprintf("%d/%d", pid, uid)
}

func formatChecksums(checksums map[string]string) string {
	if len(checksums) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(checksums))
	for k := range checksums {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%s", k, checksums[k])
	}
	return strings.Join(parts, " ")
}

func findNodeLabel(nodes []state.Node, nodeID string) string {
	for _, n := range nodes {
		if n.ID == nodeID {
			name := util.DisplayName(n)
			if name != "" {
				return name
			}
		}
	}
	if nodeID == "" {
		return "-"
	}
	return nodeID
}

func (m *Model) renderStatus() string {
	return m.theme.Subtle.Render("←/→ scroll · ↑/↓ events · pgup/pgdn · home/end")
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
		cursor:  minCursorWidth,
		time:    minTimeWidth,
		action:  minActionWidth,
		dstIP:   minDstIPWidth,
		dstHost: minDstHostWidth,
		proto:   minProtoWidth,
		process: minProcessWidth,
		cmdline: minCmdlineWidth,
		rule:    minRuleWidth,
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
			{&layout.cmdline, 6},
			{&layout.process, 6},
			{&layout.dstHost, 6},
			{&layout.dstIP, 6},
			{&layout.action, 4},
			{&layout.time, 10},
			{&layout.rule, 6},
		}
		for deficit > 0 {
			progressed := false
			for i := range reducers {
				if deficit == 0 {
					break
				}
				curr := *reducers[i].field
				if curr <= reducers[i].min {
					continue
				}
				d := min(deficit, curr-reducers[i].min)
				*reducers[i].field -= d
				deficit -= d
				progressed = true
			}
			if !progressed {
				break
			}
		}
	} else if usable > base {
		extra := usable - base
		expanders := []*int{&layout.time, &layout.cmdline, &layout.process, &layout.dstHost}
		for extra > 0 {
			for _, field := range expanders {
				if extra == 0 {
					break
				}
				(*field)++
				extra--
			}
		}
	}
	layout.cursor = max(1, layout.cursor)
	layout.time = max(10, layout.time)
	layout.action = max(4, layout.action)
	layout.dstIP = max(6, layout.dstIP)
	layout.dstHost = max(6, layout.dstHost)
	layout.proto = max(3, layout.proto)
	layout.process = max(6, layout.process)
	layout.cmdline = max(6, layout.cmdline)
	layout.rule = max(4, layout.rule)
	return layout
}

func (m *Model) clampSelection(snapshot state.Snapshot) {
	events := snapshot.Stats.Events
	if len(events) == 0 {
		m.rowIdx = 0
		m.tableOffset = 0
		return
	}
	if m.rowIdx >= len(events) {
		m.rowIdx = len(events) - 1
	}
	capacity := m.tableCapacity()
	if len(events) <= capacity {
		m.tableOffset = 0
		return
	}
	if m.rowIdx < m.tableOffset {
		m.tableOffset = m.rowIdx
	}
	if m.rowIdx >= m.tableOffset+capacity {
		m.tableOffset = m.rowIdx - capacity + 1
	}
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

func stripBackground(style lipgloss.Style) lipgloss.Style {
	return style.UnsetBackground()
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
