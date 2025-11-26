package alerts

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

// Model renders recent alert entries pushed by the daemon.
type Model struct {
	store  *state.Store
	theme  theme.Theme
	width  int
	height int
}

// New constructs the alerts view backed by the shared store.
func New(store *state.Store, th theme.Theme) view.Model {
	return &Model{store: store, theme: th}
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

func (m *Model) View() string {
	if m.width == 0 {
		return ""
	}

	snapshot := m.store.Snapshot()
	if len(snapshot.Alerts) == 0 {
		msg := m.theme.Subtle.Render("No alerts yet. Pending notifications will appear here.")
		return m.theme.Body.Copy().Width(m.width).Height(max(3, m.height)).Render(msg)
	}

	rows := make([]string, 0, len(snapshot.Alerts))
	maxRows := len(snapshot.Alerts)
	if m.height > 3 { // approximate available rows minus padding
		limit := m.height - 2
		if limit > 0 && limit < maxRows {
			maxRows = limit
		}
	}
	for idx := 0; idx < maxRows; idx++ {
		rows = append(rows, m.renderAlert(snapshot.Alerts[idx]))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	return m.theme.Body.Copy().Width(m.width).Height(max(3, m.height)).Render(content)
}

func (m *Model) Title() string { return "Alerts" }

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *Model) SetTheme(th theme.Theme) {
	m.theme = th
}

func (m *Model) renderAlert(alert state.Alert) string {
	left := fmt.Sprintf("[%s][%s] %s", strings.ToUpper(alert.Priority), strings.ToUpper(alert.Type), alert.Text)
	meta := []string{}
	if alert.NodeID != "" {
		meta = append(meta, alert.NodeID)
	}
	if alert.CreatedAt.IsZero() {
		meta = append(meta, "time unknown")
	} else {
		meta = append(meta, relativeTime(alert.CreatedAt))
	}
	if alert.Action != "" {
		meta = append(meta, fmt.Sprintf("action %s", strings.ToLower(alert.Action)))
	}
	line := lipgloss.JoinVertical(lipgloss.Left,
		m.theme.Title.Copy().Width(m.width-4).Render(left),
		m.theme.Subtle.Render(strings.Join(meta, " · ")),
	)
	return m.theme.Card.Copy().Width(max(20, m.width-4)).Render(line)
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
