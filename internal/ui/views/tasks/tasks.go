package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/view"
	"github.com/adamkadaban/opensnitch-tui/internal/util"
)

const (
	taskInterval   = "5s"
	stopTimeout    = 5 * time.Second
	maxPreviewRows = 14
)

type taskPhase string

const (
	phaseIdle     taskPhase = "idle"
	phaseStarting taskPhase = "starting"
	phaseRunning  taskPhase = "running"
	phaseStopping taskPhase = "stopping"
	phaseError    taskPhase = "error"
)

type taskProfile struct {
	name        controller.TaskName
	title       string
	description string
	config      string
	available   bool
	request     func(state.Node) controller.TaskRequest
}

var profiles = []taskProfile{
	{
		name:        controller.TaskNodeMonitor,
		title:       "Node monitor",
		description: "Safe host uptime, load, memory, swap, and process-count metrics.",
		config:      "Interval 5s · target follows the selected v1.8 daemon peer",
		available:   true,
		request: func(node state.Node) controller.TaskRequest {
			return controller.NewNodeMonitorTask(nodeMonitorTarget(node), taskInterval)
		},
	},
	{
		name:        controller.TaskSocketsMonitor,
		title:       "Sockets monitor",
		description: "Socket totals grouped by protocol, family, and state; process details stay hidden.",
		config:      "Interval 5s · state any (0) · protocol any (0) · family any (0)",
		available:   true,
		request: func(state.Node) controller.TaskRequest {
			return controller.NewSocketsMonitorTask(taskInterval, 0, 0, 0)
		},
	},
	{
		name:        controller.TaskPIDMonitor,
		title:       "PID monitor",
		description: "Unavailable until a safe PID-selection flow is implemented.",
		config:      "No free-form PID input",
		available:   false,
	},
}

type taskKey struct {
	nodeID string
	name   controller.TaskName
}

type taskRuntime struct {
	phase       taskPhase
	generation  uint64
	stream      controller.TaskStream
	cancel      context.CancelFunc
	lastUpdate  time.Time
	preview     string
	updateError string
}

type taskStartMsg struct {
	key        taskKey
	generation uint64
	stream     controller.TaskStream
	err        error
}

type taskUpdateMsg struct {
	key        taskKey
	generation uint64
	streamID   uint64
	update     controller.TaskUpdate
}

type taskDoneMsg struct {
	key        taskKey
	generation uint64
	streamID   uint64
	err        error
}

type taskStopMsg struct {
	key        taskKey
	generation uint64
	streamID   uint64
	err        error
}

// Model renders and controls daemon background task streams.
type Model struct {
	store   *state.Store
	theme   theme.Theme
	manager controller.TaskManager

	width      int
	height     int
	nodeIdx    int
	profileIdx int
	selectedID string

	nextGeneration uint64
	runtimes       map[taskKey]*taskRuntime
	statusLine     string
	statusError    bool
	closed         bool
}

// New constructs the tasks view.
func New(store *state.Store, th theme.Theme, manager controller.TaskManager) view.Model {
	return &Model{
		store:    store,
		theme:    th,
		manager:  manager,
		runtimes: make(map[taskKey]*taskRuntime),
	}
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.closed {
		if started, ok := msg.(taskStartMsg); ok && started.stream != nil && m.manager != nil {
			return m, discardTaskCmd(m.manager, started.stream)
		}
		return m, nil
	}

	snapshot := m.store.Snapshot()
	nodes := snapshot.Nodes
	m.clampNode(nodes)
	m.reconcileNodes(nodes)

	switch msg := msg.(type) {
	case taskStartMsg:
		return m, m.onStarted(msg)
	case taskUpdateMsg:
		return m, m.onUpdate(msg)
	case taskDoneMsg:
		m.onDone(msg)
	case taskStopMsg:
		m.onStopped(msg)
	case tea.KeyMsg:
		switch msg.String() {
		case "left":
			m.selectNode(nodes, -1)
		case "right":
			m.selectNode(nodes, 1)
		case "up":
			if m.profileIdx > 0 {
				m.profileIdx--
			}
		case "down":
			if m.profileIdx < len(profiles)-1 {
				m.profileIdx++
			}
		case "s":
			return m, m.startSelected(nodes)
		case "x":
			return m, m.stopSelected(nodes)
		}
	}

	return m, nil
}

func (m *Model) View() string {
	snapshot := m.store.Snapshot()
	nodes := snapshot.Nodes
	m.clampNode(nodes)

	lines := make([]string, 0, 24)
	if len(nodes) == 0 {
		lines = append(lines,
			m.theme.Subtle.Render("No daemon nodes are configured."),
			m.theme.Subtle.Render("←/→ nodes · ↑/↓ task profiles · s start · x stop"),
		)
		return m.wrap(strings.Join(lines, "\n"))
	}

	node := nodes[m.nodeIdx]
	lines = append(lines, m.renderNode(node, len(nodes)))
	lines = append(lines, m.theme.Header.Padding(0).Render("Task profiles"))
	for idx, profile := range profiles {
		lines = append(lines, m.renderProfile(node, profile, idx == m.profileIdx))
	}

	profile := profiles[m.profileIdx]
	runtime := m.runtime(taskKey{nodeID: node.ID, name: profile.name})
	lines = append(lines, "", m.clip(m.theme.Header.Padding(0).Render(profile.title+" details")))
	lines = append(lines, m.clip(profile.description), m.clip(profile.config))
	lines = append(lines, m.renderRuntime(runtime))
	lines = append(lines, m.renderPreview(runtime)...)
	lines = append(lines, m.renderStatus())

	return m.wrap(m.boundLines(lines))
}

func (m *Model) Title() string { return "Tasks" }

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *Model) SetTheme(th theme.Theme) {
	m.theme = th
}

// HandlesMessage keeps task streams active while another top-level view is selected.
func (m *Model) HandlesMessage(msg tea.Msg) bool {
	switch msg.(type) {
	case taskStartMsg, taskUpdateMsg, taskDoneMsg, taskStopMsg:
		return true
	default:
		return false
	}
}

// Close cancels every stream context owned by the view.
func (m *Model) Close() {
	if m.closed {
		return
	}
	m.closed = true
	for _, runtime := range m.runtimes {
		if runtime.cancel != nil {
			runtime.cancel()
			runtime.cancel = nil
		}
		runtime.stream = nil
	}
}

func (m *Model) startSelected(nodes []state.Node) tea.Cmd {
	node, profile, ok := m.selection(nodes)
	if !ok {
		m.setError("No task node is selected.")
		return nil
	}
	if !profile.available {
		m.setError("PID monitor is unavailable until a PID-selection flow exists.")
		return nil
	}
	if node.Status != state.NodeStatusReady {
		m.setError(fmt.Sprintf("%s is not connected.", util.DisplayName(node)))
		return nil
	}
	if m.manager == nil {
		m.setError("Task controls are unavailable.")
		return nil
	}

	key := taskKey{nodeID: node.ID, name: profile.name}
	runtime := m.runtime(key)
	switch runtime.phase {
	case phaseStarting, phaseRunning, phaseStopping:
		m.setError(fmt.Sprintf("%s is already %s for %s.", profile.title, runtime.phase, util.DisplayName(node)))
		return nil
	}

	m.nextGeneration++
	ctx, cancel := context.WithCancel(context.Background())
	*runtime = taskRuntime{
		phase:      phaseStarting,
		generation: m.nextGeneration,
		cancel:     cancel,
		lastUpdate: runtime.lastUpdate,
		preview:    runtime.preview,
	}
	m.setStatus(fmt.Sprintf("Starting %s for %s…", profile.title, util.DisplayName(node)))
	return startTaskCmd(ctx, m.manager, key, runtime.generation, profile.request(node))
}

func (m *Model) stopSelected(nodes []state.Node) tea.Cmd {
	node, profile, ok := m.selection(nodes)
	if !ok {
		m.setError("No task node is selected.")
		return nil
	}
	key := taskKey{nodeID: node.ID, name: profile.name}
	runtime := m.runtime(key)
	switch runtime.phase {
	case phaseIdle, phaseError:
		m.setError(fmt.Sprintf("%s is idle for %s.", profile.title, util.DisplayName(node)))
		return nil
	case phaseStarting:
		m.setError(fmt.Sprintf("%s is still starting for %s.", profile.title, util.DisplayName(node)))
		return nil
	case phaseStopping:
		m.setError(fmt.Sprintf("%s is already stopping for %s.", profile.title, util.DisplayName(node)))
		return nil
	}
	if node.Status != state.NodeStatusReady {
		m.setError(fmt.Sprintf("%s is not connected.", util.DisplayName(node)))
		return nil
	}
	if m.manager == nil || runtime.stream == nil {
		m.setError("Task controls are unavailable.")
		return nil
	}

	runtime.phase = phaseStopping
	m.setStatus(fmt.Sprintf("Stopping %s for %s…", profile.title, util.DisplayName(node)))
	return stopTaskCmd(m.manager, key, runtime.generation, runtime.stream)
}

func (m *Model) onStarted(msg taskStartMsg) tea.Cmd {
	runtime, ok := m.runtimes[msg.key]
	if !ok || runtime.generation != msg.generation || runtime.phase != phaseStarting {
		if msg.stream != nil && m.manager != nil {
			return discardTaskCmd(m.manager, msg.stream)
		}
		return nil
	}
	if msg.err != nil {
		m.cancelRuntime(runtime)
		runtime.phase = phaseError
		runtime.updateError = msg.err.Error()
		m.setError(fmt.Sprintf("Start %s failed: %v", msg.key.name, msg.err))
		if msg.stream != nil && m.manager != nil {
			return discardTaskCmd(m.manager, msg.stream)
		}
		return nil
	}
	if msg.stream == nil || msg.stream.NodeID() != msg.key.nodeID || msg.stream.Name() != msg.key.name {
		m.cancelRuntime(runtime)
		runtime.phase = phaseError
		runtime.updateError = "task manager returned a mismatched stream"
		m.setError(runtime.updateError)
		if msg.stream != nil && m.manager != nil {
			return discardTaskCmd(m.manager, msg.stream)
		}
		return nil
	}

	runtime.stream = msg.stream
	runtime.phase = phaseRunning
	runtime.updateError = ""
	m.setStatus(fmt.Sprintf("%s is running.", profileTitle(msg.key.name)))
	return waitTaskCmd(msg.key, msg.generation, msg.stream)
}

func (m *Model) onUpdate(msg taskUpdateMsg) tea.Cmd {
	runtime, ok := m.runtimes[msg.key]
	if !ok || runtime.generation != msg.generation || runtime.stream == nil ||
		runtime.stream.ID() != msg.streamID {
		return nil
	}

	preview, err := safePreview(msg.key.name, msg.update.Data)
	runtime.lastUpdate = time.Now()
	if err != nil {
		runtime.updateError = err.Error()
		m.setError(fmt.Sprintf("%s update rejected: %v", profileTitle(msg.key.name), err))
	} else {
		runtime.preview = preview
		runtime.updateError = ""
		m.setStatus(fmt.Sprintf("%s updated at %s.", profileTitle(msg.key.name), runtime.lastUpdate.Format("15:04:05")))
	}
	return waitTaskCmd(msg.key, msg.generation, runtime.stream)
}

func (m *Model) onDone(msg taskDoneMsg) {
	runtime, ok := m.runtimes[msg.key]
	if !ok || runtime.generation != msg.generation || runtime.stream == nil ||
		runtime.stream.ID() != msg.streamID {
		return
	}

	wasStopping := runtime.phase == phaseStopping
	m.cancelRuntime(runtime)
	if msg.err != nil {
		runtime.phase = phaseError
		runtime.updateError = msg.err.Error()
		m.setError(fmt.Sprintf("%s ended: %v", profileTitle(msg.key.name), msg.err))
		return
	}
	runtime.phase = phaseIdle
	runtime.updateError = ""
	if wasStopping {
		m.setStatus(fmt.Sprintf("%s stopped.", profileTitle(msg.key.name)))
	} else {
		m.setStatus(fmt.Sprintf("%s stream ended.", profileTitle(msg.key.name)))
	}
}

func (m *Model) onStopped(msg taskStopMsg) {
	runtime, ok := m.runtimes[msg.key]
	if !ok || runtime.generation != msg.generation || runtime.stream == nil ||
		runtime.stream.ID() != msg.streamID || runtime.phase != phaseStopping {
		return
	}
	if msg.err != nil {
		runtime.phase = phaseRunning
		runtime.updateError = msg.err.Error()
		m.setError(fmt.Sprintf("Stop %s failed: %v", msg.key.name, msg.err))
		return
	}

	m.cancelRuntime(runtime)
	runtime.phase = phaseIdle
	runtime.updateError = ""
	m.setStatus(fmt.Sprintf("%s stop acknowledged.", profileTitle(msg.key.name)))
}

func (m *Model) reconcileNodes(nodes []state.Node) {
	statuses := make(map[string]state.NodeStatus, len(nodes))
	for _, node := range nodes {
		statuses[node.ID] = node.Status
	}
	for key, runtime := range m.runtimes {
		if runtime.phase != phaseStarting && runtime.phase != phaseRunning && runtime.phase != phaseStopping {
			continue
		}
		status, exists := statuses[key.nodeID]
		if exists && status == state.NodeStatusReady {
			continue
		}
		m.cancelRuntime(runtime)
		m.nextGeneration++
		runtime.generation = m.nextGeneration
		runtime.phase = phaseError
		runtime.updateError = "node disconnected"
		m.setError(fmt.Sprintf("%s stopped because its node disconnected.", profileTitle(key.name)))
	}
}

func (m *Model) renderNode(node state.Node, count int) string {
	selector := fmt.Sprintf("Node %d/%d: ← %s →", m.nodeIdx+1, count, util.DisplayName(node))
	status := strings.ToUpper(string(node.Status))
	address := node.Address
	if address == "" {
		address = node.ID
	}
	return strings.Join([]string{
		m.clip(m.theme.Title.Render(selector)),
		m.clip(m.statusStyle(node.Status).Render(status + " · " + address)),
	}, "\n")
}

func (m *Model) renderProfile(node state.Node, profile taskProfile, selected bool) string {
	cursor := " "
	if selected {
		cursor = ">"
	}
	phase := m.runtime(taskKey{nodeID: node.ID, name: profile.name}).phase
	if !profile.available {
		phase = "unavailable"
	}
	label := fmt.Sprintf("%s %-18s %s", cursor, profile.title, strings.ToUpper(string(phase)))
	if selected {
		return m.clip(m.theme.Title.Render(label))
	}
	if !profile.available {
		return m.clip(m.theme.Subtle.Render(label))
	}
	return m.clip(label)
}

func (m *Model) renderRuntime(runtime *taskRuntime) string {
	updated := "never"
	if !runtime.lastUpdate.IsZero() {
		updated = runtime.lastUpdate.Format(time.RFC3339)
	}
	line := fmt.Sprintf("State %s · last update %s", strings.ToUpper(string(runtime.phase)), updated)
	if runtime.updateError != "" {
		line += " · " + runtime.updateError
		return m.clip(m.theme.Danger.Render(line))
	}
	return m.clip(m.phaseStyle(runtime.phase).Render(line))
}

func (m *Model) renderPreview(runtime *taskRuntime) []string {
	lines := []string{m.clip(m.theme.Header.Padding(0).Render("Latest safe summary"))}
	if runtime.preview == "" {
		return append(lines, m.clip(m.theme.Subtle.Render("No safe metrics received yet.")))
	}
	previewLines := strings.Split(runtime.preview, "\n")
	if len(previewLines) > maxPreviewRows {
		previewLines = append(previewLines[:maxPreviewRows-1], "…")
	}
	for _, line := range previewLines {
		lines = append(lines, m.clip(line))
	}
	return lines
}

func (m *Model) renderStatus() string {
	help := m.clip("←/→ nodes · ↑/↓ task profiles · s start · x stop")
	if m.statusLine == "" {
		return m.theme.Subtle.Render(help)
	}
	style := m.theme.Success
	if m.statusError {
		style = m.theme.Danger
	}
	return style.Render(m.clip(m.statusLine)) + "\n" + m.theme.Subtle.Render(help)
}

func (m *Model) selection(nodes []state.Node) (state.Node, taskProfile, bool) {
	if len(nodes) == 0 || m.nodeIdx < 0 || m.nodeIdx >= len(nodes) ||
		m.profileIdx < 0 || m.profileIdx >= len(profiles) {
		return state.Node{}, taskProfile{}, false
	}
	return nodes[m.nodeIdx], profiles[m.profileIdx], true
}

func (m *Model) selectNode(nodes []state.Node, delta int) {
	next := m.nodeIdx + delta
	if next < 0 || next >= len(nodes) {
		return
	}
	m.nodeIdx = next
	m.selectedID = nodes[next].ID
}

func (m *Model) clampNode(nodes []state.Node) {
	if len(nodes) == 0 {
		m.nodeIdx = 0
		m.selectedID = ""
		return
	}
	if m.selectedID != "" {
		for idx, node := range nodes {
			if node.ID == m.selectedID {
				m.nodeIdx = idx
				return
			}
		}
	}
	m.nodeIdx = min(max(0, m.nodeIdx), len(nodes)-1)
	m.selectedID = nodes[m.nodeIdx].ID
}

func (m *Model) runtime(key taskKey) *taskRuntime {
	runtime, ok := m.runtimes[key]
	if !ok {
		runtime = &taskRuntime{phase: phaseIdle}
		m.runtimes[key] = runtime
	}
	return runtime
}

func (m *Model) cancelRuntime(runtime *taskRuntime) {
	if runtime.cancel != nil {
		runtime.cancel()
		runtime.cancel = nil
	}
	runtime.stream = nil
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

func (m *Model) phaseStyle(phase taskPhase) lipgloss.Style {
	switch phase {
	case phaseRunning:
		return m.theme.Success
	case phaseStarting, phaseStopping:
		return m.theme.Warning
	case phaseError:
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
	capacity := m.contentHeight()
	flattened := strings.Split(strings.Join(lines, "\n"), "\n")
	if len(flattened) > capacity {
		flattened = flattened[:capacity]
		flattened[capacity-1] = m.clip("…")
	}
	return strings.Join(flattened, "\n")
}

func startTaskCmd(
	ctx context.Context,
	manager controller.TaskManager,
	key taskKey,
	generation uint64,
	request controller.TaskRequest,
) tea.Cmd {
	return func() tea.Msg {
		stream, err := manager.StartTask(ctx, key.nodeID, request)
		return taskStartMsg{key: key, generation: generation, stream: stream, err: err}
	}
}

func waitTaskCmd(key taskKey, generation uint64, stream controller.TaskStream) tea.Cmd {
	return func() tea.Msg {
		select {
		case update, ok := <-stream.Updates():
			if ok {
				return taskUpdateMsg{
					key:        key,
					generation: generation,
					streamID:   stream.ID(),
					update:     update,
				}
			}
			return taskDoneMsg{key: key, generation: generation, streamID: stream.ID(), err: stream.Err()}
		case <-stream.Done():
			return taskDoneMsg{key: key, generation: generation, streamID: stream.ID(), err: stream.Err()}
		}
	}
}

func stopTaskCmd(
	manager controller.TaskManager,
	key taskKey,
	generation uint64,
	stream controller.TaskStream,
) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
		defer cancel()
		err := manager.StopTask(ctx, stream)
		return taskStopMsg{
			key:        key,
			generation: generation,
			streamID:   stream.ID(),
			err:        err,
		}
	}
}

func discardTaskCmd(manager controller.TaskManager, stream controller.TaskStream) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
		defer cancel()
		_ = manager.StopTask(ctx, stream)
		return nil
	}
}

func nodeMonitorTarget(node state.Node) string {
	if strings.HasPrefix(node.ID, "unix://") || strings.HasPrefix(node.Address, "unix://") {
		return "unix:/local"
	}
	if node.Address != "" {
		return strings.TrimPrefix(node.Address, "tcp://")
	}
	return strings.TrimPrefix(node.ID, "tcp://")
}

func profileTitle(name controller.TaskName) string {
	for _, profile := range profiles {
		if profile.name == name {
			return profile.title
		}
	}
	return string(name)
}

func safePreview(name controller.TaskName, data json.RawMessage) (string, error) {
	switch name {
	case controller.TaskNodeMonitor:
		return nodePreview(data)
	case controller.TaskSocketsMonitor:
		return socketsPreview(data)
	default:
		return "", fmt.Errorf("unsupported safe preview for %s", name)
	}
}

func nodePreview(data json.RawMessage) (string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", fmt.Errorf("invalid JSON")
	}

	uptime, err := numberField(raw, "Uptime")
	if err != nil {
		return "", err
	}
	var loads []float64
	if value, ok := raw["Loads"]; ok {
		if err := json.Unmarshal(value, &loads); err != nil {
			return "", errors.New("invalid Loads metric")
		}
	}
	unit, _ := optionalNumberField(raw, "Unit")
	if unit == 0 {
		unit = 1
	}

	summary := map[string]any{"uptime_seconds": uptime}
	if len(loads) > 0 {
		scaled := make([]float64, len(loads))
		for idx, load := range loads {
			scaled[idx] = load / 65536
		}
		summary["load_average"] = scaled
	}
	if procs, ok := optionalNumberField(raw, "Procs"); ok {
		summary["process_count"] = procs
	}
	memory := map[string]float64{}
	for source, target := range map[string]string{
		"Totalram":  "total_bytes",
		"Freeram":   "free_bytes",
		"Sharedram": "shared_bytes",
		"Bufferram": "buffer_bytes",
	} {
		if value, ok := optionalNumberField(raw, source); ok {
			memory[target] = value * unit
		}
	}
	if len(memory) > 0 {
		summary["memory"] = memory
	}
	swap := map[string]float64{}
	for source, target := range map[string]string{
		"Totalswap": "total_bytes",
		"Freeswap":  "free_bytes",
	} {
		if value, ok := optionalNumberField(raw, source); ok {
			swap[target] = value * unit
		}
	}
	if len(swap) > 0 {
		summary["swap"] = swap
	}
	return prettyJSON(summary)
}

func socketsPreview(data json.RawMessage) (string, error) {
	var payload struct {
		Table []struct {
			Socket *struct {
				Family uint8
				State  uint8
			}
			Proto uint16
		}
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", fmt.Errorf("invalid JSON")
	}

	protocols := make(map[string]int)
	families := make(map[string]int)
	states := make(map[string]int)
	for _, socket := range payload.Table {
		protocols[strconv.Itoa(int(socket.Proto))]++
		if socket.Socket != nil {
			families[strconv.Itoa(int(socket.Socket.Family))]++
			states[strconv.Itoa(int(socket.Socket.State))]++
		}
	}
	summary := struct {
		SocketCount int            `json:"socket_count"`
		Protocols   map[string]int `json:"protocols"`
		Families    map[string]int `json:"families"`
		States      map[string]int `json:"states"`
	}{
		SocketCount: len(payload.Table),
		Protocols:   protocols,
		Families:    families,
		States:      states,
	}
	return prettyJSON(summary)
}

func numberField(raw map[string]json.RawMessage, name string) (float64, error) {
	value, ok := optionalNumberField(raw, name)
	if !ok {
		return 0, fmt.Errorf("missing numeric %s metric", name)
	}
	return value, nil
}

func optionalNumberField(raw map[string]json.RawMessage, name string) (float64, bool) {
	value, ok := raw[name]
	if !ok {
		return 0, false
	}
	var number float64
	if err := json.Unmarshal(value, &number); err != nil {
		return 0, false
	}
	return number, true
}

func prettyJSON(value any) (string, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
