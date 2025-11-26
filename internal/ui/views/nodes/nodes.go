package nodes

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/view"
)

// Model renders configured daemon nodes and their connection status.
type Model struct {
	store  *state.Store
	theme  theme.Theme
	width  int
	height int
}

// New constructs the nodes view.
func New(store *state.Store, th theme.Theme) view.Model {
	return &Model{store: store, theme: th}
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m *Model) View() string {
	snapshot := m.store.Snapshot()

	if len(snapshot.Nodes) == 0 {
		msg := m.theme.Subtle.Render("No nodes configured. Add entries under nodes[] in config.yaml.")
		return m.theme.Body.Copy().Width(m.width).Height(max(3, m.height)).Render(msg)
	}

	rows := make([]string, 0, len(snapshot.Nodes))
	for idx, node := range snapshot.Nodes {
		label := fmt.Sprintf("%02d · %s", idx+1, displayName(node))
		status := m.statusStyle(node.Status).Render(strings.ToUpper(string(node.Status)))
		meta := nodeDetails(node)

		row := lipgloss.JoinHorizontal(lipgloss.Top,
			m.theme.Title.Copy().Width(max(20, m.width/3)).Render(label),
			m.theme.Subtle.Copy().Width(max(14, m.width/6)).Render(status),
			m.theme.Body.Copy().Width(max(20, m.width/3)).Render(meta),
		)
		rows = append(rows, row)
	}

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	return m.theme.Body.Copy().Width(m.width).Height(max(3, m.height)).Render(content)
}

func (m *Model) Title() string { return "Nodes" }

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
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

func displayName(node state.Node) string {
	if node.Name != "" {
		return fmt.Sprintf("%s (%s)", node.Name, node.Address)
	}
	return node.Address
}

func nodeDetails(node state.Node) string {
	parts := []string{}
	if node.Version != "" {
		parts = append(parts, fmt.Sprintf("v%s", node.Version))
	}
	if node.Message != "" {
		parts = append(parts, node.Message)
	}
	if !node.LastSeen.IsZero() {
		parts = append(parts, fmt.Sprintf("seen %s ago", time.Since(node.LastSeen).Truncate(time.Second)))
	}
	if node.FirewallEnabled {
		parts = append(parts, "firewall: on")
	}
	if len(parts) == 0 {
		return "awaiting connection"
	}
	return strings.Join(parts, " · ")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
