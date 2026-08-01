package dashboard

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/view"
	"github.com/adamkadaban/opensnitch-tui/internal/util"
)

// Model renders the high-level telemetry summary.
type Model struct {
	store  *state.Store
	theme  theme.Theme
	width  int
	height int
}

const (
	cardHorizontalFrame = 8
	minStatCardWidth    = 24
	minTopCardWidth     = 28
	minTrafficCardWidth = 32
	maxTopBuckets       = 5
)

// New creates a dashboard view backed by the provided store.
func New(store *state.Store, th theme.Theme) view.Model {
	return &Model{store: store, theme: th}
}

// Init satisfies tea.Model.
func (m *Model) Init() tea.Cmd { return nil }

// Update satisfies tea.Model. The dashboard currently reacts only to store updates.
func (m *Model) Update(_ tea.Msg) (tea.Model, tea.Cmd) { return m, nil }

// View renders the dashboard contents.
func (m *Model) View() string {
	if m.width == 0 {
		return ""
	}

	snapshot := m.store.Snapshot()
	stats := snapshot.Stats

	statColumns := cardColumns(m.width, minStatCardWidth)
	statWidth := max(1, m.width/statColumns)
	cards := []string{
		m.renderStat("Rules", stats.Rules, statWidth, statColumns < 4),
		m.renderStat("Connections", stats.Connections, statWidth, statColumns < 4),
		m.renderStat("Accepted", stats.Accepted, statWidth, statColumns < 4),
		m.renderStat("Dropped", stats.Dropped, statWidth, statColumns < 4),
	}

	row := joinCardGrid(cards, statColumns)
	trafficWidth := min(m.width, max(minTrafficCardWidth, m.width/3))
	insights := m.renderTraffic(stats, trafficWidth)
	topColumns := cardColumns(m.width, minTopCardWidth)
	topWidth := max(1, m.width/topColumns)
	topLimit := m.topBucketLimit(stats, statColumns, topColumns)
	secondary := joinCardGrid([]string{
		m.renderTopList("Top destinations", limitBuckets(stats.TopDestHosts, topLimit), topWidth, topColumns < 4),
		m.renderTopList("Top ports", limitBuckets(stats.TopDestPorts, topLimit), topWidth, topColumns < 4),
		m.renderTopList("Top executables", limitBuckets(stats.TopExecutables, topLimit), topWidth, topColumns < 4),
		m.renderTopList("Top users", limitBuckets(stats.TopUsers, topLimit), topWidth, topColumns < 4),
	}, topColumns)
	meta := m.theme.Subtle.Render(m.metaLine(snapshot))
	body := lipgloss.JoinVertical(lipgloss.Left, row, insights, secondary, meta)

	return m.theme.Body.Width(max(1, m.width)).Height(max(3, m.height)).Render(body)
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

func (m *Model) renderStat(label string, value uint64, totalWidth int, compact bool) string {
	cardWidth := max(1, totalWidth-cardHorizontalFrame)
	content := fmt.Sprintf("%d\n%s", value, label)
	return m.cardStyle(compact).Width(cardWidth).Render(content)
}

func (m *Model) renderTraffic(stats state.Stats, totalWidth int) string {
	cardWidth := max(1, totalWidth-cardHorizontalFrame)
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
	return m.theme.Card.Width(cardWidth).Render(strings.Join(body, "\n"))
}

func (m *Model) renderTopList(title string, buckets []state.StatBucket, totalWidth int, compact bool) string {
	cardWidth := max(1, totalWidth-cardHorizontalFrame)
	head := m.theme.Title.Render(title)
	if len(buckets) == 0 {
		return m.cardStyle(compact).Width(cardWidth).Render(head + "\n" + m.theme.Subtle.Render("Waiting for data"))
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
	return m.cardStyle(compact).Width(cardWidth).Render(strings.Join(lines, "\n"))
}

func (m *Model) renderBreakdownLine(label string, value, total uint64, style lipgloss.Style, width int) string {
	bar := m.renderRelativeBar(value, total, width)
	percent := 0
	if total > 0 {
		percent = int((value*100 + total/2) / total)
	}
	return fmt.Sprintf("%-8s %s %3d%%", label, style.Render(bar), percent)
}

func (m *Model) renderRelativeBar(value, total uint64, width int) string {
	if width <= 0 {
		return ""
	}
	filled := filledWidth(value, total, width)
	return strings.Repeat("█", filled) + strings.Repeat(" ", width-filled)
}

func filledWidth(value, total uint64, width int) int {
	if width <= 0 {
		return 0
	}
	if total == 0 {
		if value > 0 {
			return width
		}
		return 0
	}
	filled := int((value*uint64(width) + total/2) / total)
	if value > 0 && filled == 0 {
		filled = 1
	}
	if filled > width {
		filled = width
	}
	return filled
}

func trimToWidth(value string, width int) string {
	return util.TruncateString(value, width)
}

func cardColumns(width, minimumWidth int) int {
	switch {
	case width >= minimumWidth*4:
		return 4
	case width >= minimumWidth*2:
		return 2
	default:
		return 1
	}
}

func joinCardGrid(cards []string, columns int) string {
	rows := make([]string, 0, (len(cards)+columns-1)/columns)
	for start := 0; start < len(cards); start += columns {
		end := min(len(cards), start+columns)
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cards[start:end]...))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (m *Model) cardStyle(compact bool) lipgloss.Style {
	if compact {
		return m.theme.Card.Padding(0, 2)
	}
	return m.theme.Card
}

func (m *Model) metaLine(snapshot state.Snapshot) string {
	ready := readyNodes(snapshot.Nodes)
	if len(ready) == 0 {
		if len(snapshot.Nodes) == 0 {
			return "Waiting for daemon telemetry"
		}
		return "No ready daemons · Telemetry paused until reconnect"
	}
	if snapshot.Stats.UpdatedAt.IsZero() {
		if len(ready) == 1 {
			return "1 daemon ready · Waiting for telemetry"
		}
		return fmt.Sprintf("%d daemons ready · Waiting for telemetry", len(ready))
	}
	if len(ready) > 1 {
		return fmt.Sprintf("%d daemons aggregated · Updated %s", len(ready), util.RelativeTime(snapshot.Stats.UpdatedAt))
	}
	stats := snapshot.Stats
	node := util.Fallback(stats.NodeName, util.Fallback(ready[0].Name, util.Fallback(stats.NodeID, ready[0].ID)))
	version := util.Fallback(stats.DaemonVersion, util.Fallback(ready[0].Version, "unknown"))
	return fmt.Sprintf("Node %s · Daemon %s · Updated %s", node, version, util.RelativeTime(stats.UpdatedAt))
}

func readyNodes(nodes []state.Node) []state.Node {
	ready := make([]state.Node, 0, len(nodes))
	for _, node := range nodes {
		if node.Status == state.NodeStatusReady {
			ready = append(ready, node)
		}
	}
	return ready
}

func (m *Model) topBucketLimit(stats state.Stats, statColumns, topColumns int) int {
	if m.height <= 0 {
		return maxTopBuckets
	}
	statRows := (4 + statColumns - 1) / statColumns
	statCardHeight := 6
	if statColumns < 4 {
		statCardHeight = 4
	}
	trafficHeight := 8
	if stats.Accepted+stats.Dropped+stats.Ignored == 0 {
		trafficHeight++
	}
	topRows := (4 + topColumns - 1) / topColumns
	topCardBase := 5
	if topColumns < 4 {
		topCardBase = 3
	}
	available := m.height - 2 - statRows*statCardHeight - trafficHeight - 1
	limit := (available/topRows - topCardBase) / 2
	return min(max(1, limit), maxTopBuckets)
}

func limitBuckets(buckets []state.StatBucket, limit int) []state.StatBucket {
	if limit <= 0 || len(buckets) <= limit {
		return buckets
	}
	return buckets[:limit]
}
