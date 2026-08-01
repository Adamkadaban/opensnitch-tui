package nodes

import (
	"context"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/view"
	"github.com/adamkadaban/opensnitch-tui/internal/util"
)

const nodeConfigApplyTimeout = 10 * time.Second

var logLevelNames = []string{"DEBUG", "INFO", "IMPORTANT", "WARNING", "ERROR", "FATAL"}

type configResultMsg struct {
	generation uint64
	nodeID     string
	err        error
}

type selectedNode struct {
	node   state.Node
	config state.NodeConfigState
	hasCfg bool
}

// Model renders node selection, read-only metadata, and safe daemon configuration controls.
type Model struct {
	store   *state.Store
	theme   theme.Theme
	manager controller.NodeConfigManager

	width  int
	height int

	nodeIdx    int
	selectedID string
	detail     bool
	fieldIdx   int
	editing    bool
	draft      state.NodeDaemonConfig
	baseline   state.NodeDaemonConfig
	draftNode  string

	generation uint64
	applying   bool
	applyNode  string
	statusLine string
	statusErr  bool
}

// New constructs the nodes view.
func New(store *state.Store, th theme.Theme, manager controller.NodeConfigManager) view.Model {
	return &Model{store: store, theme: th, manager: manager}
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	snapshot := m.store.Snapshot()
	nodes := availableNodes(snapshot)
	m.clampSelection(nodes)

	switch msg := msg.(type) {
	case configResultMsg:
		if msg.generation != m.generation || msg.nodeID != m.applyNode {
			return m, nil
		}
		m.applying = false
		m.applyNode = ""
		if msg.err != nil {
			m.setError(fmt.Sprintf("Apply failed: %v", msg.err))
			return m, nil
		}
		if updated, ok := snapshot.NodeConfigs[msg.nodeID]; ok &&
			reflect.DeepEqual(updated.Config, m.draft) {
			m.baseline = updated.Config
			m.draft = updated.Config
		} else {
			m.baseline = m.draft
		}
		m.editing = false
		m.setStatus("Configuration applied successfully.")
	case tea.KeyMsg:
		if !m.detail {
			switch msg.String() {
			case "up":
				m.selectNode(nodes, -1)
			case "down":
				m.selectNode(nodes, 1)
			case "enter":
				if len(nodes) > 0 {
					m.detail = true
					m.loadDraft(nodes[m.nodeIdx])
				}
			}
			return m, nil
		}
		if m.applying {
			m.setError("Configuration apply is already in progress.")
			return m, nil
		}

		switch msg.String() {
		case "esc":
			if m.editing || m.dirty() {
				m.draft = m.baseline
				m.editing = false
				m.setStatus("Edits cancelled.")
			} else {
				m.detail = false
				m.statusLine = ""
			}
		case "e":
			m.toggleEdit(nodes)
		case "s":
			return m, m.apply(nodes)
		case "up":
			if m.fieldIdx > 0 {
				m.fieldIdx--
			}
		case "down":
			if m.fieldIdx < nodeConfigFieldCount-1 {
				m.fieldIdx++
			}
		case "left":
			if m.editing {
				m.adjustField(-1)
			}
		case "right", "enter", " ":
			if m.editing {
				m.adjustField(1)
			}
		}
	}

	return m, nil
}

func (m *Model) View() string {
	snapshot := m.store.Snapshot()
	nodes := availableNodes(snapshot)
	m.clampSelection(nodes)
	if len(nodes) == 0 {
		return m.wrap(m.theme.Subtle.Render("No nodes configured. Add entries under nodes[] in config.yaml."))
	}
	if !m.detail {
		return m.renderList(nodes)
	}
	return m.renderDetail(nodes[m.nodeIdx], len(nodes))
}

func (m *Model) Title() string { return "Nodes" }

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *Model) SetTheme(th theme.Theme) {
	m.theme = th
}

// HandlesMessage accepts completed configuration requests while this view is inactive.
func (m *Model) HandlesMessage(msg tea.Msg) bool {
	_, ok := msg.(configResultMsg)
	return ok
}

func (m *Model) renderList(nodes []selectedNode) string {
	lines := []string{
		m.clip(m.theme.Header.Padding(0).Render("Configured and connected nodes")),
		m.clip(m.theme.Subtle.Render("↑/↓ select · Enter details/config")),
		"",
	}
	for idx, selected := range nodes {
		cursor := " "
		style := m.theme.Body.Padding(0)
		if idx == m.nodeIdx {
			cursor = ">"
			style = m.theme.Title.Padding(0)
		}
		status := strings.ToUpper(string(selected.node.Status))
		configStatus := "config pending"
		if selected.hasCfg {
			configStatus = "config ready"
			if selected.config.ParseError != "" {
				configStatus = "config parse error"
			}
		}
		line := fmt.Sprintf(
			"%s %-24s %-12s %s",
			cursor,
			util.DisplayName(selected.node),
			status,
			configStatus,
		)
		lines = append(lines, m.clip(style.Render(line)))
	}
	return m.wrap(m.boundLines(lines))
}

func (m *Model) renderDetail(selected selectedNode, count int) string {
	node := selected.node
	config := selected.config
	lines := []string{
		m.clip(m.theme.Title.Render(fmt.Sprintf(
			"Node %d/%d · %s · %s",
			m.nodeIdx+1,
			count,
			util.DisplayName(node),
			strings.ToUpper(string(node.Status)),
		))),
		m.clip(fmt.Sprintf("Address: %s (read-only)", redactAddress(node.Address))),
		m.clip(fmt.Sprintf(
			"Authentication: %s · TLS: %s",
			authLabel(config.Metadata.AuthenticationType),
			tlsLabel(config.Metadata.TLSConfigured),
		)),
		m.clip(fmt.Sprintf("Rules path: %s (read-only)", valueOrUnknown(config.Config.Rules.Path))),
	}

	if !selected.hasCfg {
		lines = append(lines, "", m.theme.Subtle.Render("Daemon configuration has not been received for this node."), m.renderStatus())
		return m.wrap(m.boundLines(lines))
	}
	if config.ParseError != "" {
		lines = append(lines, "", m.theme.Danger.Render("Configuration parse error: "+config.ParseError), m.renderStatus())
		return m.wrap(m.boundLines(lines))
	}
	if m.draftNode != node.ID ||
		(!m.editing && !m.applying && !m.dirty() && m.baseline != selected.config.Config) {
		m.loadDraft(selected)
	}

	mode := "read-only"
	if m.editing {
		mode = "editing"
	}
	if m.applying {
		mode = "applying"
	}
	if m.dirty() {
		mode += " · dirty"
	}
	lines = append(lines, m.clip(m.theme.Subtle.Render(
		fmt.Sprintf("Mode: %s · ↑/↓ field · ←/→ or Enter/space change · e edit/exit · s save · esc cancel/back", mode),
	)))
	for idx := 0; idx < nodeConfigFieldCount; idx++ {
		lines = append(lines, m.renderField(idx))
	}
	lines = append(lines, m.renderStatus())
	return m.wrap(m.boundLines(lines))
}

func (m *Model) dirty() bool {
	return m.draft != m.baseline
}

const nodeConfigFieldCount = 14

func (m *Model) renderField(index int) string {
	labels := []string{
		"Default action",
		"Default duration",
		"Process monitor",
		"Log level",
		"UTC timestamps",
		"Microsecond timestamps",
		"Intercept unknown",
		"Rule checksums",
		"Flush connections on start",
		"GC percent",
		"Firewall monitor interval",
		"Netfilter queue bypass",
		"Maximum events",
		"Maximum statistics",
	}
	values := []string{
		m.draft.DefaultAction,
		m.draft.DefaultDuration,
		m.draft.ProcMonitorMethod,
		logLevelLabel(m.draft.LogLevel),
		boolLabel(m.draft.LogUTC),
		boolLabel(m.draft.LogMicro),
		boolLabel(m.draft.InterceptUnknown),
		boolLabel(m.draft.Rules.EnableChecksums),
		boolLabel(m.draft.Internal.FlushConnsOnStart),
		fmt.Sprintf("%d", m.draft.Internal.GCPercent),
		m.draft.FwOptions.MonitorInterval,
		boolLabel(m.draft.FwOptions.QueueBypass),
		fmt.Sprintf("%d", m.draft.Stats.MaxEvents),
		fmt.Sprintf("%d", m.draft.Stats.MaxStats),
	}
	cursor := " "
	style := m.theme.Body.Padding(0)
	if index == m.fieldIdx {
		cursor = ">"
		style = m.theme.Title.Padding(0)
	}
	return m.clip(style.Render(fmt.Sprintf("%s %-30s %s", cursor, labels[index], values[index])))
}

func (m *Model) toggleEdit(nodes []selectedNode) {
	if m.applying {
		m.setError("Configuration apply is already in progress.")
		return
	}
	selected, ok := m.current(nodes)
	if !ok || !selected.hasCfg {
		m.setError("Daemon configuration is unavailable.")
		return
	}
	if selected.config.ParseError != "" {
		m.setError("Malformed daemon configuration cannot be edited.")
		return
	}
	if m.editing {
		m.editing = false
		m.setStatus("Edit mode exited; unsaved changes are retained.")
		return
	}
	if m.draftNode != selected.node.ID {
		m.loadDraft(selected)
	}
	m.editing = true
	m.setStatus("Edit mode enabled.")
}

func (m *Model) apply(nodes []selectedNode) tea.Cmd {
	if m.applying {
		m.setError("Configuration apply is already in progress.")
		return nil
	}
	selected, ok := m.current(nodes)
	if !ok || !selected.hasCfg {
		m.setError("Daemon configuration is unavailable.")
		return nil
	}
	if selected.node.Status != state.NodeStatusReady {
		m.setError(fmt.Sprintf("%s is not connected.", util.DisplayName(selected.node)))
		return nil
	}
	if selected.config.ParseError != "" {
		m.setError("Malformed daemon configuration cannot be applied.")
		return nil
	}
	if m.manager == nil {
		m.setError("Node configuration controls are unavailable.")
		return nil
	}
	if err := state.ValidateNodeDaemonConfig(m.draft); err != nil {
		m.setError("Invalid configuration: " + err.Error())
		return nil
	}
	if !m.dirty() {
		m.setError("No configuration changes to apply.")
		return nil
	}

	m.generation++
	m.applying = true
	m.applyNode = selected.node.ID
	m.setStatus(fmt.Sprintf("Applying configuration to %s…", util.DisplayName(selected.node)))
	return applyConfigCmd(m.manager, m.generation, selected.node.ID, m.draft)
}

func applyConfigCmd(
	manager controller.NodeConfigManager,
	generation uint64,
	nodeID string,
	config state.NodeDaemonConfig,
) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), nodeConfigApplyTimeout)
		defer cancel()
		return configResultMsg{
			generation: generation,
			nodeID:     nodeID,
			err:        manager.ApplyNodeConfig(ctx, nodeID, config),
		}
	}
}

func (m *Model) adjustField(delta int) {
	switch m.fieldIdx {
	case 0:
		m.draft.DefaultAction = nextOption(state.NodeDefaultActions, m.draft.DefaultAction, delta)
	case 1:
		m.draft.DefaultDuration = nextOption(state.NodeDefaultDurations, m.draft.DefaultDuration, delta)
	case 2:
		m.draft.ProcMonitorMethod = nextOption(state.NodeProcMonitorMethods, m.draft.ProcMonitorMethod, delta)
	case 3:
		m.draft.LogLevel = clamp(m.draft.LogLevel+delta, state.NodeLogLevelDebug, state.NodeLogLevelFatal)
	case 4:
		m.draft.LogUTC = !m.draft.LogUTC
	case 5:
		m.draft.LogMicro = !m.draft.LogMicro
	case 6:
		m.draft.InterceptUnknown = !m.draft.InterceptUnknown
	case 7:
		m.draft.Rules.EnableChecksums = !m.draft.Rules.EnableChecksums
	case 8:
		m.draft.Internal.FlushConnsOnStart = !m.draft.Internal.FlushConnsOnStart
	case 9:
		m.draft.Internal.GCPercent = clamp(
			m.draft.Internal.GCPercent+(delta*10),
			state.NodeGCPercentMin,
			state.NodeGCPercentMax,
		)
	case 10:
		interval, err := time.ParseDuration(m.draft.FwOptions.MonitorInterval)
		if err != nil {
			interval = 15 * time.Second
		}
		interval = time.Duration(clamp(
			int(interval/time.Second)+(delta*5),
			0,
			int(state.NodeMonitorIntervalMax/time.Second),
		)) * time.Second
		m.draft.FwOptions.MonitorInterval = interval.String()
	case 11:
		m.draft.FwOptions.QueueBypass = !m.draft.FwOptions.QueueBypass
	case 12:
		m.draft.Stats.MaxEvents = clamp(
			m.draft.Stats.MaxEvents+(delta*25),
			state.NodeMaxEventsMin,
			state.NodeMaxEventsMax,
		)
	case 13:
		m.draft.Stats.MaxStats = clamp(
			m.draft.Stats.MaxStats+(delta*5),
			state.NodeMaxStatsMin,
			state.NodeMaxStatsMax,
		)
	}
	if err := state.ValidateNodeDaemonConfig(m.draft); err != nil {
		m.setError("Invalid configuration: " + err.Error())
	} else {
		m.statusLine = ""
		m.statusErr = false
	}
}

func availableNodes(snapshot state.Snapshot) []selectedNode {
	nodes := make([]selectedNode, 0, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		config, ok := snapshot.NodeConfigs[node.ID]
		nodes = append(nodes, selectedNode{node: node, config: config, hasCfg: ok})
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		left := strings.ToLower(util.DisplayName(nodes[i].node))
		right := strings.ToLower(util.DisplayName(nodes[j].node))
		if left == right {
			return nodes[i].node.ID < nodes[j].node.ID
		}
		return left < right
	})
	return nodes
}

func (m *Model) clampSelection(nodes []selectedNode) {
	if len(nodes) == 0 {
		m.nodeIdx = 0
		m.selectedID = ""
		return
	}
	if m.selectedID != "" {
		for idx, selected := range nodes {
			if selected.node.ID == m.selectedID {
				m.nodeIdx = idx
				return
			}
		}
	}
	m.nodeIdx = clamp(m.nodeIdx, 0, len(nodes)-1)
	m.selectedID = nodes[m.nodeIdx].node.ID
}

func (m *Model) selectNode(nodes []selectedNode, delta int) {
	if len(nodes) == 0 || m.applying {
		return
	}
	m.nodeIdx = clamp(m.nodeIdx+delta, 0, len(nodes)-1)
	m.selectedID = nodes[m.nodeIdx].node.ID
	m.statusLine = ""
}

func (m *Model) current(nodes []selectedNode) (selectedNode, bool) {
	if len(nodes) == 0 || m.nodeIdx < 0 || m.nodeIdx >= len(nodes) {
		return selectedNode{}, false
	}
	return nodes[m.nodeIdx], true
}

func (m *Model) loadDraft(selected selectedNode) {
	m.draftNode = selected.node.ID
	m.draft = selected.config.Config
	m.baseline = selected.config.Config
	m.editing = false
	m.fieldIdx = 0
}

func (m *Model) setStatus(message string) {
	m.statusLine = message
	m.statusErr = false
}

func (m *Model) setError(message string) {
	m.statusLine = message
	m.statusErr = true
}

func (m *Model) renderStatus() string {
	if m.statusLine == "" {
		return ""
	}
	if m.statusErr {
		return m.clip(m.theme.Danger.Render(m.statusLine))
	}
	return m.clip(m.theme.Success.Render(m.statusLine))
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

func (m *Model) contentHeight() int {
	if m.height <= 0 {
		return 24
	}
	return max(1, m.height-m.theme.Body.GetVerticalFrameSize())
}

func (m *Model) clip(value string) string {
	return util.AnsiSlice(value, 0, m.contentWidth())
}

func (m *Model) boundLines(lines []string) string {
	for idx := range lines {
		lines[idx] = m.clip(lines[idx])
	}
	if len(lines) > m.contentHeight() {
		lines = lines[:m.contentHeight()]
		lines[len(lines)-1] = m.clip("…")
	}
	return strings.Join(lines, "\n")
}

func nextOption(options []string, current string, delta int) string {
	if len(options) == 0 {
		return current
	}
	index := 0
	for idx, option := range options {
		if option == current {
			index = idx
			break
		}
	}
	index = (index + delta) % len(options)
	if index < 0 {
		index += len(options)
	}
	return options[index]
}

func boolLabel(value bool) string {
	if value {
		return "enabled"
	}
	return "disabled"
}

func logLevelLabel(level int) string {
	if level < 0 || level >= len(logLevelNames) {
		return fmt.Sprintf("INVALID (%d)", level)
	}
	return logLevelNames[level]
}

func tlsLabel(configured bool) string {
	if configured {
		return "configured (redacted)"
	}
	return "not configured"
}

func authLabel(authType string) string {
	switch strings.ToLower(authType) {
	case "simple", "simple-tls", "mutual-tls", "token", "google":
		return authType + " (read-only)"
	case "":
		return "unknown"
	default:
		return "custom (redacted)"
	}
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func redactAddress(address string) string {
	parsed, err := url.Parse(address)
	if err != nil {
		return address
	}
	if parsed.User != nil {
		parsed.User = url.User("redacted")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func clamp(value, minimum, maximum int) int {
	return min(max(value, minimum), maximum)
}
