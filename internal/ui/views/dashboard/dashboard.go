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
	minTrafficCardWidth = 40
	maxTopBuckets       = 5
	shortTerminalHint   = "Short terminal:"
)

type statLayout int

const (
	statLayoutFull statLayout = iota
	statLayoutCompact
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
	if m.width <= 0 || m.height <= 0 {
		return ""
	}

	snapshot := m.store.Snapshot()
	stats := snapshot.Stats
	bodyStyle := m.theme.Body.Width(max(1, m.width)).Height(max(1, m.height))
	contentWidth := max(1, m.width-bodyStyle.GetHorizontalFrameSize())
	contentHeight := max(0, m.height-bodyStyle.GetVerticalFrameSize())
	if contentHeight == 0 {
		return bodyStyle.Padding(0).Render("")
	}
	if contentHeight == 1 {
		essential := fmt.Sprintf(
			"Rules %d · Connections %d · Accepted %d · Dropped %d · %s",
			stats.Rules,
			stats.Connections,
			stats.Accepted,
			stats.Dropped,
			m.metaLine(snapshot),
		)
		return bodyStyle.Render(trimToWidth(essential, contentWidth))
	}

	meta := m.theme.Subtle.Render(trimToWidth(m.metaLine(snapshot), contentWidth))
	sections := []string{m.renderStats(stats, contentWidth, contentHeight-lipgloss.Height(meta))}
	remaining := contentHeight - renderedHeight(sections) - lipgloss.Height(meta)

	traffic := m.renderTraffic(stats, min(contentWidth, max(minTrafficCardWidth, contentWidth/3)), false)
	top := m.renderTopLists(stats, contentWidth, maxTopBuckets, false)
	fullDetailsFit := lipgloss.Height(traffic)+lipgloss.Height(top) <= remaining
	reserveHint := 0
	if !fullDetailsFit && remaining > 0 {
		reserveHint = 1
	}
	detailBudget := max(0, remaining-reserveHint)

	trafficIncluded := false
	for _, compact := range []bool{false, true} {
		candidate := m.renderTraffic(stats, min(contentWidth, max(minTrafficCardWidth, contentWidth/3)), compact)
		if lipgloss.Height(candidate) <= detailBudget {
			sections = append(sections, candidate)
			detailBudget -= lipgloss.Height(candidate)
			trafficIncluded = true
			break
		}
	}

	topLimit := 0
	for limit := maxTopBuckets; limit >= 1; limit-- {
		for _, compact := range []bool{false, true} {
			candidate := m.renderTopLists(stats, contentWidth, limit, compact)
			if lipgloss.Height(candidate) <= detailBudget {
				sections = append(sections, candidate)
				detailBudget -= lipgloss.Height(candidate)
				topLimit = limit
				break
			}
		}
		if topLimit > 0 {
			break
		}
	}

	if !trafficIncluded || topLimit < maxTopBuckets {
		hint := m.shortTerminalHint(trafficIncluded, topLimit)
		if renderedHeight(sections)+lipgloss.Height(meta) < contentHeight {
			sections = append(sections, m.theme.Subtle.Render(trimToWidth(hint, contentWidth)))
		}
	}
	sections = append(sections, meta)

	return bodyStyle.Render(lipgloss.JoinVertical(lipgloss.Left, sections...))
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

func (m *Model) renderStat(label string, value uint64, totalWidth int, layout statLayout) string {
	cardWidth := max(1, totalWidth-cardHorizontalFrame)
	content := fmt.Sprintf("%d\n%s", value, label)
	if layout == statLayoutCompact {
		content = fmt.Sprintf("%s %d", label, value)
	}
	return m.cardStyle(layout != statLayoutFull).Width(cardWidth).Render(content)
}

func (m *Model) renderTraffic(stats state.Stats, totalWidth int, compact bool) string {
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
	if total == 0 && !compact {
		body = append(body, m.theme.Subtle.Render("No traffic yet"))
	}
	return m.cardStyle(compact).Width(cardWidth).Render(strings.Join(body, "\n"))
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

func (m *Model) renderStats(stats state.Stats, width, height int) string {
	columns := cardColumns(width, minStatCardWidth)
	cardWidth := max(1, width/columns)
	layout := statLayoutFull
	if columns < 4 {
		layout = statLayoutCompact
	}
	render := func(layout statLayout) string {
		return joinCardGrid([]string{
			m.renderStat("Rules", stats.Rules, cardWidth, layout),
			m.renderStat("Connections", stats.Connections, cardWidth, layout),
			m.renderStat("Accepted", stats.Accepted, cardWidth, layout),
			m.renderStat("Dropped", stats.Dropped, cardWidth, layout),
		}, columns)
	}
	result := render(layout)
	if lipgloss.Height(result) <= height {
		return result
	}
	result = render(statLayoutCompact)
	if lipgloss.Height(result) <= height {
		return result
	}
	return trimToWidth(
		fmt.Sprintf(
			"Rules %d · Connections %d · Accepted %d · Dropped %d",
			stats.Rules,
			stats.Connections,
			stats.Accepted,
			stats.Dropped,
		),
		width,
	)
}

func (m *Model) renderTopLists(stats state.Stats, width, limit int, compact bool) string {
	columns := cardColumns(width, minTopCardWidth)
	cardWidth := max(1, width/columns)
	compact = compact || columns < 4
	return joinCardGrid([]string{
		m.renderTopList("Top destinations", limitBuckets(stats.TopDestHosts, limit), cardWidth, compact),
		m.renderTopList("Top ports", limitBuckets(stats.TopDestPorts, limit), cardWidth, compact),
		m.renderTopList("Top executables", limitBuckets(stats.TopExecutables, limit), cardWidth, compact),
		m.renderTopList("Top users", limitBuckets(stats.TopUsers, limit), cardWidth, compact),
	}, columns)
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

func (m *Model) shortTerminalHint(trafficIncluded bool, topLimit int) string {
	switch {
	case !trafficIncluded:
		return shortTerminalHint + " traffic and top lists omitted"
	case topLimit == 0:
		return shortTerminalHint + " top lists omitted"
	default:
		return shortTerminalHint + " top lists reduced"
	}
}

func renderedHeight(sections []string) int {
	height := 0
	for _, section := range sections {
		height += lipgloss.Height(section)
	}
	return height
}

func limitBuckets(buckets []state.StatBucket, limit int) []state.StatBucket {
	if limit <= 0 {
		return nil
	}
	if len(buckets) <= limit {
		return buckets
	}
	return buckets[:limit]
}
