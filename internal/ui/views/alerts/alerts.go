package alerts

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/adamkadaban/opensnitch-tui/internal/alertsafety"
	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/view"
	"github.com/adamkadaban/opensnitch-tui/internal/util"
)

const (
	maxRenderText       = 512
	maxRenderItems      = 16
	maxRenderTreeDepth  = 8
	maxRenderTreeNodes  = 64
	alertExportTimeout  = 5 * time.Second
	listStatusLineCount = 1
)

type exportMsg struct {
	path string
	err  error
}

// Model renders recent alert entries pushed by the daemon.
type Model struct {
	store   *state.Store
	theme   theme.Theme
	archive controller.AlertArchive

	width        int
	height       int
	rowIdx       int
	listOffset   int
	detail       bool
	detailOffset int
	statusLine   string
	exporting    bool
}

// New constructs the alerts view backed by the shared store.
func New(store *state.Store, th theme.Theme, archives ...controller.AlertArchive) view.Model {
	model := &Model{store: store, theme: th}
	if len(archives) > 0 {
		model.archive = archives[0]
	}
	return model
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	snapshot := m.store.Snapshot()
	m.clampSelection(snapshot.Alerts)

	switch msg := msg.(type) {
	case exportMsg:
		m.exporting = false
		if msg.err != nil {
			m.statusLine = m.theme.Danger.Render(fmt.Sprintf("Export failed: %v", msg.err))
		} else {
			m.statusLine = m.theme.Success.Render(fmt.Sprintf("Exported alert to %s.", msg.path))
		}
	case tea.KeyMsg:
		if m.detail {
			switch msg.String() {
			case "esc":
				m.detail = false
				m.detailOffset = 0
			case "up":
				m.detailOffset = max(0, m.detailOffset-1)
			case "down":
				m.detailOffset++
			case "x":
				m.deleteSelected(snapshot.Alerts)
			case "o":
				return m, m.exportSelected(snapshot.Alerts)
			}
			return m, nil
		}
		switch msg.String() {
		case "up":
			if m.rowIdx > 0 {
				m.rowIdx--
			}
		case "down":
			if m.rowIdx < len(snapshot.Alerts)-1 {
				m.rowIdx++
			}
		case "enter":
			if len(snapshot.Alerts) > 0 {
				m.detail = true
				m.detailOffset = 0
			}
		case "x":
			m.deleteSelected(snapshot.Alerts)
		case "o":
			return m, m.exportSelected(snapshot.Alerts)
		}
	}
	m.clampSelection(m.store.Snapshot().Alerts)
	return m, nil
}

// HandlesMessage accepts completed exports while this view is inactive.
func (m *Model) HandlesMessage(msg tea.Msg) bool {
	_, ok := msg.(exportMsg)
	return ok
}

func (m *Model) View() string {
	if m.width == 0 {
		return ""
	}

	alerts := m.store.Snapshot().Alerts
	m.clampSelection(alerts)
	if len(alerts) == 0 {
		msg := m.theme.Subtle.Render("No alerts yet. Pending notifications will appear here.")
		return m.wrap(lipgloss.JoinVertical(lipgloss.Left, msg, m.renderStatus()))
	}

	var body string
	if m.detail {
		body = m.renderDetail(alerts[m.rowIdx])
	} else {
		body = m.renderList(alerts)
	}
	return m.wrap(body)
}

func (m *Model) Title() string { return "Alerts" }

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *Model) SetTheme(th theme.Theme) {
	m.theme = th
}

func (m *Model) renderList(alerts []state.Alert) string {
	capacity := max(1, m.contentHeight()-listStatusLineCount)
	if m.rowIdx < m.listOffset {
		m.listOffset = m.rowIdx
	}
	if m.rowIdx >= m.listOffset+capacity {
		m.listOffset = m.rowIdx - capacity + 1
	}
	maxOffset := max(0, len(alerts)-capacity)
	m.listOffset = min(m.listOffset, maxOffset)
	end := min(len(alerts), m.listOffset+capacity)

	rows := make([]string, 0, end-m.listOffset+1)
	for idx := m.listOffset; idx < end; idx++ {
		rows = append(rows, m.renderAlertRow(alerts[idx], idx == m.rowIdx))
	}
	rows = append(rows, m.renderStatus())
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (m *Model) renderAlertRow(alert state.Alert, selected bool) string {
	cursor := "  "
	if selected {
		cursor = "› "
	}
	timestamp := "time unknown"
	if !alert.CreatedAt.IsZero() {
		timestamp = alert.CreatedAt.Local().Format("2006-01-02 15:04:05")
	}
	line := fmt.Sprintf(
		"%s%s · %s · %s · %s · %s · %s",
		cursor,
		timestamp,
		fallback(alert.NodeID),
		fallback(string(alert.Priority)),
		fallback(string(alert.Type)),
		fallback(string(alert.What)),
		alertSummary(alert),
	)
	line = m.clip(alertsafety.LimitText(alertsafety.RedactInline(line), maxRenderText))
	if selected {
		return m.theme.Title.Render(line)
	}
	return line
}

func (m *Model) renderDetail(alert state.Alert) string {
	lines := []string{
		fmt.Sprintf("Alert %s", fallback(alert.ID)),
		fmt.Sprintf("Time: %s", formatTime(alert.CreatedAt)),
		fmt.Sprintf("Node: %s", fallback(alert.NodeID)),
		fmt.Sprintf("Priority / type: %s / %s", fallback(string(alert.Priority)), fallback(string(alert.Type))),
		fmt.Sprintf("What / action: %s / %s", fallback(string(alert.What)), fallback(string(alert.Action))),
		fmt.Sprintf("Payload: %s", fallback(string(alert.PayloadKind))),
		"",
	}
	lines = append(lines, payloadDetail(alert)...)
	lines = append(lines, "", m.renderStatus())

	capacity := max(1, m.contentHeight())
	maxOffset := max(0, len(lines)-capacity)
	m.detailOffset = min(m.detailOffset, maxOffset)
	visible := lines[m.detailOffset:min(len(lines), m.detailOffset+capacity)]
	for i := range visible {
		if m.detailOffset+i == len(lines)-1 {
			visible[i] = m.clip(visible[i])
			continue
		}
		visible[i] = m.clip(alertsafety.LimitText(alertsafety.RedactInline(visible[i]), maxRenderText))
	}
	return strings.Join(visible, "\n")
}

func payloadDetail(alert state.Alert) []string {
	switch alert.PayloadKind {
	case state.AlertPayloadText:
		return []string{"Text: " + safe(alert.Text)}
	case state.AlertPayloadProcess:
		if alert.Process == nil {
			return []string{"Process payload unavailable."}
		}
		return processDetail(*alert.Process)
	case state.AlertPayloadConnection:
		if alert.Connection == nil {
			return []string{"Connection payload unavailable."}
		}
		return connectionDetail(*alert.Connection)
	case state.AlertPayloadRule:
		if alert.Rule == nil {
			return []string{"Rule payload unavailable."}
		}
		return ruleDetail(*alert.Rule)
	case state.AlertPayloadFirewall:
		if alert.FirewallRule == nil {
			return []string{"Firewall rule payload unavailable."}
		}
		return firewallDetail(*alert.FirewallRule)
	default:
		if alert.Text != "" {
			return []string{"Text: " + safe(alert.Text)}
		}
		return []string{"No structured payload."}
	}
}

func processDetail(process state.Process) []string {
	lines := []string{
		fmt.Sprintf("PID / PPID / UID: %d / %d / %d", process.PID, process.PPID, process.UID),
		"Command: " + safe(process.Comm),
		"Path: " + safe(process.Path),
		"CWD: " + safe(process.CWD),
		"Args: " + safeArgs(process.Args),
		fmt.Sprintf("I/O reads/writes: %d / %d", process.IOReads, process.IOWrites),
		fmt.Sprintf("Network reads/writes: %d / %d", process.NetReads, process.NetWrites),
		"Environment: " + environmentSummary(process.Env),
		"Checksums: " + mapSummary(process.Checksums, false),
	}
	return append(lines, treeDetail(process.ProcessTree)...)
}

func connectionDetail(connection state.Connection) []string {
	lines := []string{
		"Protocol: " + safe(connection.Protocol),
		fmt.Sprintf("Source: %s:%d", safe(connection.SrcIP), connection.SrcPort),
		fmt.Sprintf("Destination: %s:%d (%s)", safe(connection.DstIP), connection.DstPort, safe(connection.DstHost)),
		fmt.Sprintf("PID / UID: %d / %d", connection.ProcessID, connection.UserID),
		"Process: " + safe(connection.ProcessPath),
		"CWD: " + safe(connection.ProcessCWD),
		"Args: " + safeArgs(connection.ProcessArgs),
		"Environment: " + environmentSummary(connection.ProcessEnv),
		"Checksums: " + mapSummary(connection.ProcessChecksums, false),
	}
	return append(lines, treeDetail(connection.ProcessTree)...)
}

func ruleDetail(rule state.Rule) []string {
	lines := []string{
		"Name: " + safe(rule.Name),
		"Description: " + safe(rule.Description),
		fmt.Sprintf("Action / duration: %s / %s", safe(rule.Action), safe(rule.Duration)),
		fmt.Sprintf("Enabled / precedence / no-log: %t / %t / %t", rule.Enabled, rule.Precedence, rule.NoLog),
		"Operator:",
	}
	budget := maxRenderTreeNodes
	return append(lines, operatorDetail(rule.Operator, 0, &budget)...)
}

func operatorDetail(operator state.RuleOperator, depth int, budget *int) []string {
	if *budget <= 0 {
		return []string{strings.Repeat("  ", depth) + "… operator limit reached"}
	}
	if depth >= maxRenderTreeDepth {
		return []string{strings.Repeat("  ", depth) + "… depth limit reached"}
	}
	*budget = *budget - 1
	data := safe(operator.Data)
	if operator.Sensitive || alertsafety.SensitiveName(operator.Operand) {
		data = alertsafety.Redacted
	}
	line := fmt.Sprintf("%s- %s %s %s", strings.Repeat("  ", depth), safe(operator.Type), safe(operator.Operand), data)
	lines := []string{strings.TrimRight(line, " ")}
	for _, child := range operator.Children[:min(len(operator.Children), maxRenderItems)] {
		lines = append(lines, operatorDetail(child, depth+1, budget)...)
		if *budget <= 0 {
			break
		}
	}
	if len(operator.Children) > maxRenderItems {
		lines = append(lines, strings.Repeat("  ", depth+1)+"… children truncated")
	}
	return lines
}

func firewallDetail(rule state.FirewallRule) []string {
	lines := []string{
		fmt.Sprintf("Table / chain: %s / %s", safe(rule.Table), safe(rule.Chain)),
		fmt.Sprintf("UUID / position: %s / %d", safe(rule.UUID), rule.Position),
		fmt.Sprintf("Enabled: %t", rule.Enabled),
		"Description: " + safe(rule.Description),
		"Parameters: " + safe(rule.Parameters),
		fmt.Sprintf("Target: %s %s", safe(rule.Target), safe(rule.TargetParameters)),
		"Expressions:",
	}
	for idx, expression := range rule.Expressions[:min(len(rule.Expressions), maxRenderItems)] {
		if expression.Statement == nil {
			lines = append(lines, fmt.Sprintf("  %d. unavailable", idx+1))
			continue
		}
		statement := expression.Statement
		lines = append(lines, fmt.Sprintf("  %d. %s %s", idx+1, safe(statement.Op), safe(statement.Name)))
		for _, value := range statement.Values[:min(len(statement.Values), maxRenderItems)] {
			rendered := safe(value.Value)
			if alertsafety.SensitiveName(value.Key) {
				rendered = alertsafety.Redacted
			}
			lines = append(lines, fmt.Sprintf("     %s=%s", safe(value.Key), rendered))
		}
	}
	if len(rule.Expressions) > maxRenderItems {
		lines = append(lines, "  … expressions truncated")
	}
	return lines
}

func treeDetail(entries []state.ProcessTreeEntry) []string {
	if len(entries) == 0 {
		return []string{"Process tree: -"}
	}
	lines := []string{"Process tree:"}
	for _, entry := range entries[:min(len(entries), maxRenderItems)] {
		lines = append(lines, fmt.Sprintf("  %d %s", entry.PID, safe(entry.Path)))
	}
	if len(entries) > maxRenderItems {
		lines = append(lines, "  … entries truncated")
	}
	return lines
}

func alertSummary(alert state.Alert) string {
	switch alert.PayloadKind {
	case state.AlertPayloadText:
		return safe(alert.Text)
	case state.AlertPayloadProcess:
		if alert.Process == nil {
			return "process unavailable"
		}
		return strings.TrimSpace(fmt.Sprintf("process %s pid %d", fallback(alert.Process.Comm), alert.Process.PID))
	case state.AlertPayloadConnection:
		if alert.Connection == nil {
			return "connection unavailable"
		}
		host := fallback(alert.Connection.DstHost)
		if host == "-" {
			host = fallback(alert.Connection.DstIP)
		}
		return fmt.Sprintf("%s → %s:%d", fallback(alert.Connection.ProcessPath), host, alert.Connection.DstPort)
	case state.AlertPayloadRule:
		if alert.Rule == nil {
			return "rule unavailable"
		}
		return fmt.Sprintf("rule %s (%s)", fallback(alert.Rule.Name), fallback(alert.Rule.Action))
	case state.AlertPayloadFirewall:
		if alert.FirewallRule == nil {
			return "firewall rule unavailable"
		}
		return fmt.Sprintf("%s/%s → %s", fallback(alert.FirewallRule.Table), fallback(alert.FirewallRule.Chain), fallback(alert.FirewallRule.Target))
	default:
		if alert.Text != "" {
			return safe(alert.Text)
		}
		return "no payload"
	}
}

func (m *Model) deleteSelected(alerts []state.Alert) {
	if len(alerts) == 0 {
		m.statusLine = m.theme.Danger.Render("No alert selected.")
		return
	}
	alert := alerts[m.rowIdx]
	if !m.store.DeleteAlert(alert) {
		m.statusLine = m.theme.Danger.Render("Selected alert is no longer available.")
		return
	}
	m.detail = false
	m.detailOffset = 0
	m.statusLine = m.theme.Success.Render("Deleted selected alert locally.")
}

func (m *Model) exportSelected(alerts []state.Alert) tea.Cmd {
	if len(alerts) == 0 {
		m.statusLine = m.theme.Danger.Render("No alert selected.")
		return nil
	}
	if m.archive == nil {
		m.statusLine = m.theme.Danger.Render("Alert archive unavailable.")
		return nil
	}
	if m.exporting {
		return nil
	}
	alert := alerts[m.rowIdx]
	m.exporting = true
	m.statusLine = m.theme.Subtle.Render("Exporting selected alert…")
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), alertExportTimeout)
		defer cancel()
		path, err := m.archive.Export(ctx, alert)
		return exportMsg{path: path, err: err}
	}
}

func (m *Model) renderStatus() string {
	help := "↑/↓ select · enter details · x delete · o export"
	if m.detail {
		help = "↑/↓ scroll · esc back · x delete · o export"
	}
	if m.statusLine == "" {
		return m.theme.Subtle.Render(help)
	}
	return m.clip(m.statusLine + " · " + m.theme.Subtle.Render(help))
}

func (m *Model) clampSelection(alerts []state.Alert) {
	if len(alerts) == 0 {
		m.rowIdx = 0
		m.listOffset = 0
		m.detail = false
		m.detailOffset = 0
		return
	}
	m.rowIdx = min(max(0, m.rowIdx), len(alerts)-1)
	m.listOffset = min(max(0, m.listOffset), m.rowIdx)
}

func (m *Model) wrap(body string) string {
	return m.theme.Body.Width(max(1, m.width)).Height(max(3, m.height)).Render(body)
}

func (m *Model) contentWidth() int {
	return max(1, m.width-m.theme.Body.GetHorizontalFrameSize())
}

func (m *Model) contentHeight() int {
	return max(1, m.height-m.theme.Body.GetVerticalFrameSize())
}

func (m *Model) clip(value string) string {
	return util.AnsiSlice(value, 0, m.contentWidth())
}

func safe(value string) string {
	value = alertsafety.LimitText(alertsafety.RedactInline(value), maxRenderText)
	return fallback(value)
}

func safeArgs(args []string) string {
	values := alertsafety.RedactArgs(args, maxRenderItems, maxRenderText)
	if len(values) == 0 {
		return "-"
	}
	result := strings.Join(values, " ")
	if len(args) > maxRenderItems {
		result += " …"
	}
	return result
}

func environmentSummary(values map[string]string) string {
	if len(values) == 0 {
		return "-"
	}
	keys := make([]string, 0, min(len(values), maxRenderItems))
	for key := range values {
		keys = append(keys, safe(key))
		if len(keys) == maxRenderItems {
			break
		}
	}
	sort.Strings(keys)
	suffix := ""
	if len(values) > maxRenderItems {
		suffix = ", …"
	}
	return fmt.Sprintf("%d key(s), values redacted: %s%s", len(values), strings.Join(keys, ", "), suffix)
}

func mapSummary(values map[string]string, redactValues bool) string {
	if len(values) == 0 {
		return "-"
	}
	keys := make([]string, 0, min(len(values), maxRenderItems))
	for key := range values {
		keys = append(keys, key)
		if len(keys) == maxRenderItems {
			break
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := safe(values[key])
		if redactValues || alertsafety.SensitiveName(key) {
			value = alertsafety.Redacted
		}
		parts = append(parts, safe(key)+"="+value)
	}
	if len(values) > maxRenderItems {
		parts = append(parts, "…")
	}
	return strings.Join(parts, ", ")
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "unknown"
	}
	return value.Local().Format(time.RFC3339)
}

func fallback(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}
