package firewall

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/components/table"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/view"
	"github.com/adamkadaban/opensnitch-tui/internal/util"
)

const firewallActionTimeout = 10 * time.Second

type action string

const (
	actionEnable  action = "enable"
	actionDisable action = "disable"
	actionReload  action = "reload"
)

type firewallResultMsg struct {
	action action
	nodeID string
	err    error
}

type selectedNode struct {
	node     state.Node
	firewall state.SystemFirewall
}

// Model renders node-scoped system firewall state and controls.
type Model struct {
	store      *state.Store
	theme      theme.Theme
	controller controller.FirewallManager

	width  int
	height int

	nodeIdx  int
	chainIdx int
	offset   int

	inProgress     action
	inProgressNode string
	statusLine     string
	statusError    bool
}

// New constructs the firewall view.
func New(store *state.Store, th theme.Theme, ctrl controller.FirewallManager) view.Model {
	return &Model{store: store, theme: th, controller: ctrl}
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	snapshot := m.store.Snapshot()
	nodes := availableNodes(snapshot)
	m.clampSelection(nodes)

	switch msg := msg.(type) {
	case firewallResultMsg:
		if msg.nodeID != m.inProgressNode || msg.action != m.inProgress {
			return m, nil
		}
		m.inProgress = ""
		m.inProgressNode = ""
		if msg.err != nil {
			m.setError(fmt.Sprintf("%s firewall failed: %v", msg.action, msg.err))
		} else {
			m.setStatus(fmt.Sprintf("Firewall %s acknowledged for %s.", msg.action, displayNodeID(snapshot, msg.nodeID)))
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "left":
			m.selectNode(nodes, -1)
		case "right":
			m.selectNode(nodes, 1)
		case "up":
			if m.chainIdx > 0 {
				m.chainIdx--
			}
		case "down":
			if selected, ok := m.current(nodes); ok {
				chains := flattenChains(selected.firewall)
				if m.chainIdx < len(chains)-1 {
					m.chainIdx++
				}
			}
		case "e":
			return m, m.request(nodes, actionEnable)
		case "d":
			return m, m.request(nodes, actionDisable)
		case "r":
			return m, m.request(nodes, actionReload)
		}
	}

	return m, nil
}

func (m *Model) View() string {
	snapshot := m.store.Snapshot()
	nodes := availableNodes(snapshot)
	m.clampSelection(nodes)
	if len(nodes) == 0 {
		empty := m.theme.Subtle.Render("No system firewall data reported by connected nodes.")
		return m.wrap(lipgloss.JoinVertical(lipgloss.Left, empty, m.renderStatus()))
	}

	selected, _ := m.current(nodes)
	chains := flattenChains(selected.firewall)
	header := m.renderNodeHeader(selected, len(nodes))
	chainTable := m.renderChains(chains)
	detail := m.renderDetail(chains)
	status := m.renderStatus()
	return m.wrap(lipgloss.JoinVertical(lipgloss.Left, header, chainTable, detail, status))
}

func (m *Model) Title() string { return "Firewall" }

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *Model) SetTheme(th theme.Theme) {
	m.theme = th
}

func (m *Model) request(nodes []selectedNode, requested action) tea.Cmd {
	if m.inProgress != "" {
		m.setError(fmt.Sprintf("Firewall %s is already in progress.", m.inProgress))
		return nil
	}
	selected, ok := m.current(nodes)
	if !ok {
		m.setError("No firewall node selected.")
		return nil
	}
	if selected.node.Status != state.NodeStatusReady {
		m.setError(fmt.Sprintf("%s is not connected.", util.DisplayName(selected.node)))
		return nil
	}
	if m.controller == nil {
		m.setError("Firewall controls are unavailable.")
		return nil
	}
	switch requested {
	case actionEnable:
		if selected.firewall.Running {
			m.setError("Firewall is already running.")
			return nil
		}
	case actionDisable:
		if !selected.firewall.Running {
			m.setError("Firewall is already stopped.")
			return nil
		}
	}

	m.inProgress = requested
	m.inProgressNode = selected.node.ID
	m.setStatus(fmt.Sprintf("Firewall %s in progress for %s…", requested, util.DisplayName(selected.node)))
	return firewallCmd(m.controller, requested, selected.node.ID)
}

func firewallCmd(ctrl controller.FirewallManager, requested action, nodeID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), firewallActionTimeout)
		defer cancel()

		var err error
		switch requested {
		case actionEnable:
			err = ctrl.EnableFirewall(ctx, nodeID)
		case actionDisable:
			err = ctrl.DisableFirewall(ctx, nodeID)
		case actionReload:
			err = ctrl.ReloadFirewall(ctx, nodeID)
		}
		return firewallResultMsg{action: requested, nodeID: nodeID, err: err}
	}
}

func (m *Model) renderNodeHeader(selected selectedNode, count int) string {
	name := util.DisplayName(selected.node)
	selector := fmt.Sprintf("Node %d/%d: ← %s →", m.nodeIdx+1, count, name)
	status := strings.ToUpper(string(selected.node.Status))
	firewallStatus := "stopped"
	if selected.firewall.Running {
		firewallStatus = "running"
	}
	configStatus := "disabled"
	if selected.firewall.Enabled {
		configStatus = "enabled"
	}
	meta := fmt.Sprintf(
		"%s · daemon v%s · firewall v%d · %s/%s",
		status,
		valueOrDash(selected.node.Version),
		selected.firewall.Version,
		firewallStatus,
		configStatus,
	)
	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.clip(m.theme.Title.Render(selector)),
		m.clip(m.statusStyle(selected.node.Status).Render(meta)),
	)
}

func (m *Model) renderChains(chains []state.FirewallChain) string {
	if len(chains) == 0 {
		return m.theme.Subtle.Render("No chains reported for this firewall.")
	}

	widths := m.tableWidths()
	rows := []string{m.renderChainHeader(widths)}
	capacity := m.tableCapacity()
	m.adjustOffset(len(chains), capacity)
	end := min(len(chains), m.offset+capacity)
	for idx := m.offset; idx < end; idx++ {
		rows = append(rows, m.renderChainRow(chains[idx], widths, idx == m.chainIdx))
	}
	if end < len(chains) {
		rows = append(rows, table.RenderCaretRow(m.contentWidth(), m.theme.Subtle))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (m *Model) renderChainHeader(widths [7]int) string {
	labels := []string{"", "FAMILY", "TABLE", "NAME", "HOOK", "POLICY", "RULES"}
	cells := make([]string, len(labels))
	style := m.theme.Header.Padding(0)
	for i := range labels {
		cells[i] = table.PadAndStyle(style, labels[i], widths[i], true)
	}
	return strings.Join(cells, " ")
}

func (m *Model) renderChainRow(chain state.FirewallChain, widths [7]int, selected bool) string {
	cursor := " "
	if selected {
		cursor = ">"
	}
	style := m.theme.Body.Padding(0)
	nameStyle := style
	if selected {
		nameStyle = m.theme.Title.Padding(0)
	}
	values := []string{
		cursor,
		chain.Family,
		chain.Table,
		chain.Name,
		chain.Hook,
		chain.Policy,
		fmt.Sprintf("%d", len(chain.Rules)),
	}
	cells := make([]string, len(values))
	for i := range values {
		cellStyle := style
		if i == 3 {
			cellStyle = nameStyle
		}
		cells[i] = table.PadAndStyle(cellStyle, values[i], widths[i], true)
	}
	return strings.Join(cells, " ")
}

func (m *Model) renderDetail(chains []state.FirewallChain) string {
	if len(chains) == 0 {
		return m.theme.Subtle.Render("Select another node or wait for firewall rules.")
	}
	chain := chains[m.chainIdx]
	lines := []string{
		m.theme.Header.Padding(0).Render("Chain detail"),
		fmt.Sprintf("%s/%s/%s", valueOrDash(chain.Family), valueOrDash(chain.Table), valueOrDash(chain.Name)),
		fmt.Sprintf(
			"Type %s · Priority %s · Hook %s · Policy %s · Rules %d",
			valueOrDash(chain.Type),
			valueOrDash(chain.Priority),
			valueOrDash(chain.Hook),
			valueOrDash(chain.Policy),
			len(chain.Rules),
		),
	}
	if len(chain.Rules) > 0 {
		rule := chain.Rules[0]
		lines = append(lines, fmt.Sprintf("First rule: %s → %s", valueOrDash(rule.Description), valueOrDash(rule.Target)))
	} else {
		lines = append(lines, "No rules in this chain.")
	}
	for i := range lines {
		lines[i] = m.clip(lines[i])
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderStatus() string {
	help := m.clip("←/→ nodes · ↑/↓ chains · e enable · d disable · r reload")
	if m.statusLine == "" {
		return m.theme.Subtle.Render(help)
	}
	style := m.theme.Success
	if m.statusError {
		style = m.theme.Danger
	} else if m.inProgress != "" {
		style = m.theme.Warning
	}
	return fmt.Sprintf("%s\n%s", style.Render(m.clip(m.statusLine)), m.theme.Subtle.Render(help))
}

func (m *Model) tableWidths() [7]int {
	content := max(7, m.contentWidth())
	widths := [7]int{2, 8, 12, 16, 10, 10, 5}
	gaps := len(widths) - 1
	total := gaps
	for _, width := range widths {
		total += width
	}
	if total < content {
		widths[3] += content - total
	} else if total > content {
		deficit := total - content
		for _, idx := range []int{3, 2, 4, 5, 1} {
			minimum := 4
			if idx == 3 {
				minimum = 6
			}
			delta := min(deficit, max(0, widths[idx]-minimum))
			widths[idx] -= delta
			deficit -= delta
			if deficit == 0 {
				break
			}
		}
	}
	return widths
}

func (m *Model) tableCapacity() int {
	if m.height <= 0 {
		return 5
	}
	return max(3, min(12, m.height-11))
}

func (m *Model) adjustOffset(chainCount, capacity int) {
	if m.chainIdx < m.offset {
		m.offset = m.chainIdx
	}
	if m.chainIdx >= m.offset+capacity {
		m.offset = m.chainIdx - capacity + 1
	}
	maxOffset := max(0, chainCount-capacity)
	m.offset = min(max(0, m.offset), maxOffset)
}

func (m *Model) selectNode(nodes []selectedNode, delta int) {
	next := m.nodeIdx + delta
	if next < 0 || next >= len(nodes) {
		return
	}
	m.nodeIdx = next
	m.chainIdx = 0
	m.offset = 0
}

func (m *Model) current(nodes []selectedNode) (selectedNode, bool) {
	if len(nodes) == 0 || m.nodeIdx < 0 || m.nodeIdx >= len(nodes) {
		return selectedNode{}, false
	}
	return nodes[m.nodeIdx], true
}

func (m *Model) clampSelection(nodes []selectedNode) {
	if len(nodes) == 0 {
		m.nodeIdx = 0
		m.chainIdx = 0
		m.offset = 0
		return
	}
	m.nodeIdx = min(max(0, m.nodeIdx), len(nodes)-1)
	chains := flattenChains(nodes[m.nodeIdx].firewall)
	if len(chains) == 0 {
		m.chainIdx = 0
		m.offset = 0
		return
	}
	m.chainIdx = min(max(0, m.chainIdx), len(chains)-1)
}

func (m *Model) setStatus(message string) {
	m.statusLine = message
	m.statusError = false
}

func (m *Model) setError(message string) {
	m.statusLine = message
	m.statusError = true
}

func (m *Model) statusStyle(status state.NodeStatus) lipgloss.Style {
	switch status {
	case state.NodeStatusReady:
		return m.theme.Success
	case state.NodeStatusConnecting:
		return m.theme.Warning
	case state.NodeStatusError:
		return m.theme.Danger
	default:
		return m.theme.Subtle
	}
}

func (m *Model) wrap(body string) string {
	return m.theme.Body.Width(max(1, m.width)).Height(max(3, m.height)).Render(body)
}

func (m *Model) contentWidth() int {
	if m.width <= 0 {
		return 80
	}
	return max(1, m.width-m.theme.Body.GetHorizontalFrameSize())
}

func (m *Model) clip(value string) string {
	return util.AnsiSlice(value, 0, m.contentWidth())
}

func availableNodes(snapshot state.Snapshot) []selectedNode {
	nodes := make([]selectedNode, 0, len(snapshot.SystemFirewalls))
	for _, node := range snapshot.Nodes {
		firewall, ok := snapshot.SystemFirewalls[node.ID]
		if !ok {
			continue
		}
		nodes = append(nodes, selectedNode{node: node, firewall: firewall})
	}
	return nodes
}

func flattenChains(firewall state.SystemFirewall) []state.FirewallChain {
	var chains []state.FirewallChain
	for _, group := range firewall.SystemRules {
		chains = append(chains, group.Chains...)
	}
	return chains
}

func displayNodeID(snapshot state.Snapshot, nodeID string) string {
	for _, node := range snapshot.Nodes {
		if node.ID == nodeID {
			return util.DisplayName(node)
		}
	}
	return nodeID
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
