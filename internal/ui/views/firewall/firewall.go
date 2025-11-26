package firewall

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/view"
)

// Model renders firewall state and dispatches actions to the daemon.
type Model struct {
	store      *state.Store
	theme      theme.Theme
	controller controller.Firewall
	width      int
	height     int
	selected   int
	statusLine string
	viewport   viewport.Model
}

// New constructs a firewall management view.
func New(store *state.Store, th theme.Theme, ctrl controller.Firewall) view.Model {
	return &Model{store: store, theme: th, controller: ctrl, viewport: viewport.Model{}}
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	snapshot := m.store.Snapshot()
	nodes := snapshot.Nodes

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected < len(nodes)-1 {
				m.selected++
			}
		case "e":
			m.trigger(nodes, func(id string) error {
				if m.controller == nil {
					return fmt.Errorf("firewall controller unavailable")
				}
				return m.controller.EnableFirewall(id)
			}, "enable")
		case "d":
			m.trigger(nodes, func(id string) error {
				if m.controller == nil {
					return fmt.Errorf("firewall controller unavailable")
				}
				return m.controller.DisableFirewall(id)
			}, "disable")
		case "r":
			m.trigger(nodes, func(id string) error {
				if m.controller == nil {
					return fmt.Errorf("firewall controller unavailable")
				}
				return m.controller.ReloadFirewall(id)
			}, "reload")
		}
	}

	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	return m, vpCmd
}

func (m *Model) trigger(nodes []state.Node, fn func(string) error, action string) {
	if len(nodes) == 0 {
		return
	}
	if m.selected >= len(nodes) {
		m.selected = len(nodes) - 1
	}
	node := nodes[m.selected]
	if err := fn(node.ID); err != nil {
		m.statusLine = m.theme.Danger.Render(fmt.Sprintf("%s firewall on %s failed: %v", action, node.Name, err))
		return
	}
	m.statusLine = m.theme.Success.Render(fmt.Sprintf("Requested %s firewall on %s", action, node.Name))
}

func (m *Model) View() string {
	snapshot := m.store.Snapshot()
	nodes := snapshot.Nodes

	if len(nodes) == 0 {
		msg := m.theme.Subtle.Render("No nodes connected. Configure daemon endpoints to manage firewalls.")
		return m.render(msg)
	}
	if m.selected >= len(nodes) {
		m.selected = len(nodes) - 1
	}

	rows := make([]string, 0, len(nodes))
	for idx, node := range nodes {
		cursor := " "
		if idx == m.selected {
			cursor = ">"
		}
		status := m.renderStatus(node, snapshot.Firewalls[node.ID])
		row := lipgloss.JoinHorizontal(lipgloss.Top,
			m.theme.Title.Copy().Width(2).Render(cursor),
			m.theme.Body.Copy().Width(max(24, m.width/4)).Render(displayName(node)),
			status,
		)
		rows = append(rows, row)
	}

	detail := m.renderDetail(snapshot.Firewalls[nodes[m.selected].ID])
	help := m.theme.Subtle.Render("↑/↓ select · e enable · d disable · r reload")
	status := m.statusLine
	if status == "" {
		status = help
	} else {
		status = fmt.Sprintf("%s\n%s", status, help)
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinVertical(lipgloss.Left, rows...),
		detail,
		status,
	)

	return m.render(content)
}

func (m *Model) Title() string { return "Firewall" }

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.viewport.Width = width
	m.viewport.Height = max(5, height)
}

func (m *Model) render(body string) string {
	m.viewport.SetContent(m.theme.Body.Copy().Width(m.width).Height(max(5, m.height)).Render(body))
	return m.viewport.View()
}

func (m *Model) renderStatus(node state.Node, fw state.Firewall) string {
	label := "disabled"
	style := m.theme.Warning
	if fw.Enabled {
		label = "enabled"
		style = m.theme.Success
	}
	if node.Status != state.NodeStatusReady {
		label = strings.ToLower(string(node.Status))
		style = m.theme.Subtle
	}
	return style.Copy().Width(max(12, m.width/6)).Render(label)
}

func (m *Model) renderDetail(fw state.Firewall) string {
	if len(fw.Chains) == 0 {
		return m.theme.Subtle.Render("No firewall chains reported by daemon.")
	}
	lines := []string{}
	limit := min(len(fw.Chains), 5)
	for i := 0; i < limit; i++ {
		chain := fw.Chains[i]
		lines = append(lines, fmt.Sprintf("%s/%s (%s) · %d rules", chain.Table, chain.Name, fallback(chain.Policy, "policy"), len(chain.Rules)))
	}
	if len(fw.Chains) > limit {
		lines = append(lines, fmt.Sprintf("… %d more chains", len(fw.Chains)-limit))
	}
	return m.theme.Subtle.Render(strings.Join(lines, "\n"))
}

func displayName(node state.Node) string {
	if node.Name != "" {
		return fmt.Sprintf("%s (%s)", node.Name, node.Address)
	}
	return node.Address
}

func fallback(value, def string) string {
	if value == "" {
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
