package dashboard

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/view"
)

// Model renders the high-level telemetry summary.
type Model struct {
	store  *state.Store
	theme  theme.Theme
	width  int
	height int
}

// New creates a dashboard view backed by the provided store.
func New(store *state.Store, th theme.Theme) view.Model {
	return &Model{store: store, theme: th}
}

// Init satisfies tea.Model.
func (m *Model) Init() tea.Cmd { return nil }

// Update satisfies tea.Model. The dashboard currently reacts only to store updates.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

// View renders the dashboard contents.
func (m *Model) View() string {
	if m.width == 0 {
		return ""
	}

	snapshot := m.store.Snapshot()
	stats := snapshot.Stats

	cards := []string{
		m.renderStat("Rules", stats.Rules),
		m.renderStat("Connections", stats.Connections),
		m.renderStat("Accepted", stats.Accepted),
		m.renderStat("Dropped", stats.Dropped),
	}

	row := lipgloss.JoinHorizontal(lipgloss.Top, cards...)

	meta := m.theme.Subtle.Render(m.metaLine(stats))

	body := lipgloss.JoinVertical(lipgloss.Left, row, meta)

	return m.theme.Body.Copy().Width(m.width).Height(max(3, m.height)).Render(body)
}

// Title returns the tab label for this view.
func (m *Model) Title() string { return "Dashboard" }

// SetSize updates the view's drawing bounds.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *Model) renderStat(label string, value uint64) string {
	const cardOverhead = 8 // border (2) + padding (4) + margin (2)
	cardWidth := max(16, m.width/4-cardOverhead)
	content := fmt.Sprintf("%d\n%s", value, label)
	return m.theme.Card.Copy().Width(cardWidth).Render(content)
}

func (m *Model) metaLine(stats state.Stats) string {
	if stats.UpdatedAt.IsZero() {
		return "Waiting for daemon telemetry"
	}
	node := fallback(stats.NodeName, fallback(stats.NodeID, "unknown node"))
	return fmt.Sprintf("Node %s · Daemon %s · Updated %s", node, fallback(stats.DaemonVersion, "unknown"), relativeTime(stats.UpdatedAt))
}

func fallback(value, def string) string {
	if value == "" {
		return def
	}
	return value
}

func relativeTime(ts time.Time) string {
	delta := time.Since(ts)
	if delta < time.Second {
		delta = time.Second
	}
	return fmt.Sprintf("%s ago", delta.Truncate(time.Second))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
