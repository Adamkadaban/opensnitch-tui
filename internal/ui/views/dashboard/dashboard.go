package dashboard

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
	trafficWidth := max(24, m.width/3)
	insights := m.renderTraffic(stats, trafficWidth)
	secondary := lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderTopList("Top destinations", stats.TopDestHosts, m.width/3),
		m.renderTopList("Top ports", stats.TopDestPorts, m.width/3),
		m.renderTopList("Top executables", stats.TopExecutables, m.width/3),
	)
	meta := m.theme.Subtle.Render(m.metaLine(stats))
	body := lipgloss.JoinVertical(lipgloss.Left, row, insights, secondary, meta)

	return m.theme.Body.Copy().Width(m.width).Height(max(3, m.height)).Render(body)
}

// Title returns the tab label for this view.
func (m *Model) Title() string { return "Dashboard" }

// SetSize updates the view's drawing bounds.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// SetTheme updates the active palette.
func (m *Model) SetTheme(th theme.Theme) {
	m.theme = th
}

func (m *Model) renderStat(label string, value uint64) string {
	const cardOverhead = 8 // border (2) + padding (4) + margin (2)
	cardWidth := max(16, m.width/4-cardOverhead)
	content := fmt.Sprintf("%d\n%s", value, label)
	return m.theme.Card.Copy().Width(cardWidth).Render(content)
}

func (m *Model) renderTraffic(stats state.Stats, cardWidth int) string {
	title := m.theme.Title.Render("Traffic mix")
	segments := []struct {
		label string
		value uint64
		style lipgloss.Style
	}{
		{"Accepted", stats.Accepted, m.theme.Success},
		{"Dropped", stats.Dropped, m.theme.Danger},
		{"Ignored", stats.Ignored, m.theme.Warning},
	}
	body := make([]string, 0, len(segments)+1)
	body = append(body, title)
	total := stats.Accepted + stats.Dropped + stats.Ignored
	barWidth := max(10, cardWidth-20)
	for _, seg := range segments {
		line := m.renderBreakdownLine(seg.label, seg.value, total, seg.style, barWidth)
		body = append(body, line)
	}
	if total == 0 {
		body = append(body, m.theme.Subtle.Render("No traffic yet"))
	}
	return m.theme.Card.Copy().Width(cardWidth).Render(strings.Join(body, "\n"))
}

func (m *Model) renderTopList(title string, buckets []state.StatBucket, width int) string {
	cardWidth := max(20, width-4)
	head := m.theme.Title.Render(title)
	if len(buckets) == 0 {
		return m.theme.Card.Copy().Width(cardWidth).Render(head + "\n" + m.theme.Subtle.Render("Waiting for data"))
	}
	lines := make([]string, 0, len(buckets)+1)
	lines = append(lines, head)
	maxValue := buckets[0].Value
	if maxValue == 0 {
		maxValue = 1
	}
	barWidth := max(6, cardWidth-14)
	for _, bucket := range buckets {
		bar := m.renderRelativeBar(bucket.Value, maxValue, barWidth)
		lines = append(lines, trimToWidth(bucket.Label, cardWidth-2))
		lines = append(lines, fmt.Sprintf("%-*s %6d", barWidth+1, bar, bucket.Value))
	}
	return m.theme.Card.Copy().Width(cardWidth).Render(strings.Join(lines, "\n"))
}

func (m *Model) renderBreakdownLine(label string, value, total uint64, style lipgloss.Style, width int) string {
	bar := m.renderRelativeBar(value, total, width)
	percent := 0
	if total > 0 {
		percent = int((value*100 + total/2) / total)
	}
	return fmt.Sprintf("%-8s %s %3d%%", label, style.Render(bar), percent)
}

func (m *Model) renderRelativeBar(value, max uint64, width int) string {
	if width <= 0 {
		return ""
	}
	filled := filledWidth(value, max, width)
	return strings.Repeat("█", filled) + strings.Repeat(" ", width-filled)
}

func filledWidth(value, max uint64, width int) int {
	if width <= 0 {
		return 0
	}
	if max == 0 {
		if value > 0 {
			return width
		}
		return 0
	}
	filled := int((value*uint64(width) + max/2) / max)
	if value > 0 && filled == 0 {
		filled = 1
	}
	if filled > width {
		filled = width
	}
	return filled
}

func trimToWidth(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
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
