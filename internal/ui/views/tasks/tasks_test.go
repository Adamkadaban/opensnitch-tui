package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
)

type fakeTaskManager struct {
	mu sync.Mutex

	stream      controller.TaskStream
	startErr    error
	stopErr     error
	startCalls  int
	stopCalls   int
	startCtx    context.Context
	stopCtx     context.Context
	nodeID      string
	request     controller.TaskRequest
	stoppedTask controller.TaskStream
}

type fakeTaskManagerSnapshot struct {
	startCalls  int
	stopCalls   int
	startCtx    context.Context
	stopCtx     context.Context
	nodeID      string
	request     controller.TaskRequest
	stoppedTask controller.TaskStream
}

func (f *fakeTaskManager) StartTask(
	ctx context.Context,
	nodeID string,
	request controller.TaskRequest,
) (controller.TaskStream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCalls++
	f.startCtx = ctx
	f.nodeID = nodeID
	f.request = request
	return f.stream, f.startErr
}

func (f *fakeTaskManager) StopTask(ctx context.Context, stream controller.TaskStream) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCalls++
	f.stopCtx = ctx
	f.stoppedTask = stream
	return f.stopErr
}

func (f *fakeTaskManager) snapshot() fakeTaskManagerSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fakeTaskManagerSnapshot{
		startCalls:  f.startCalls,
		stopCalls:   f.stopCalls,
		startCtx:    f.startCtx,
		stopCtx:     f.stopCtx,
		nodeID:      f.nodeID,
		request:     f.request,
		stoppedTask: f.stoppedTask,
	}
}

type fakeTaskStream struct {
	id     uint64
	nodeID string
	name   controller.TaskName

	updates chan controller.TaskUpdate
	done    chan struct{}
	once    sync.Once
	mu      sync.Mutex
	err     error
}

func newFakeTaskStream(id uint64, nodeID string, name controller.TaskName) *fakeTaskStream {
	return &fakeTaskStream{
		id:      id,
		nodeID:  nodeID,
		name:    name,
		updates: make(chan controller.TaskUpdate, 4),
		done:    make(chan struct{}),
	}
}

func (f *fakeTaskStream) ID() uint64                            { return f.id }
func (f *fakeTaskStream) NodeID() string                        { return f.nodeID }
func (f *fakeTaskStream) Name() controller.TaskName             { return f.name }
func (f *fakeTaskStream) Updates() <-chan controller.TaskUpdate { return f.updates }
func (f *fakeTaskStream) Done() <-chan struct{}                 { return f.done }

func (f *fakeTaskStream) Err() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

func (f *fakeTaskStream) finish(err error) {
	f.once.Do(func() {
		f.mu.Lock()
		f.err = err
		f.mu.Unlock()
		close(f.updates)
		close(f.done)
	})
}

var _ controller.TaskManager = (*fakeTaskManager)(nil)
var _ controller.TaskStream = (*fakeTaskStream)(nil)

func TestTaskViewStartsAsynchronouslyAndConsumesMultipleUpdates(t *testing.T) {
	store := readyTaskStore(state.Node{
		ID:      "unix://@opensnitch",
		Name:    "local",
		Address: "unix://@opensnitch",
		Status:  state.NodeStatusReady,
	})
	stream := newFakeTaskStream(41, "unix://@opensnitch", controller.TaskNodeMonitor)
	manager := &fakeTaskManager{stream: stream}
	model := New(store, theme.New(theme.Options{}), manager).(*Model)
	model.SetSize(100, 36)

	_, startCmd := model.Update(keyRune('s'))
	if startCmd == nil {
		t.Fatal("expected asynchronous start command")
	}
	if got := manager.snapshot().startCalls; got != 0 {
		t.Fatalf("Update blocked on StartTask; got %d calls before command execution", got)
	}

	started := startCmd()
	_, waitCmd := model.Update(started)
	if waitCmd == nil {
		t.Fatal("expected recurring stream command")
	}
	call := manager.snapshot()
	if call.nodeID != "unix://@opensnitch" {
		t.Fatalf("unexpected node ID %q", call.nodeID)
	}
	config, ok := call.request.Data.(controller.NodeMonitorConfig)
	if !ok {
		t.Fatalf("unexpected node monitor config type %T", call.request.Data)
	}
	if config.Node != "unix:/local" || config.Interval != taskInterval {
		t.Fatalf("unexpected node monitor config: %+v", config)
	}
	if err := call.startCtx.Err(); err != nil {
		t.Fatalf("task context ended after StartTask returned: %v", err)
	}

	stream.updates <- controller.TaskUpdate{Data: json.RawMessage(
		`{"Uptime":42,"Loads":[65536,131072,32768],"Totalram":100,"Freeram":25,"Unit":1024,"Procs":7,"Environment":"SECRET_TOKEN=hidden"}`,
	)}
	firstUpdate := waitCmd()
	_, waitCmd = model.Update(firstUpdate)
	if waitCmd == nil {
		t.Fatal("expected stream command after first update")
	}

	stream.updates <- controller.TaskUpdate{Data: json.RawMessage(
		`{"Uptime":47,"Loads":[32768,65536,98304],"Totalram":100,"Freeram":20,"Unit":1024,"Procs":8}`,
	)}
	secondUpdate := waitCmd()
	_, waitCmd = model.Update(secondUpdate)
	if waitCmd == nil {
		t.Fatal("expected stream command after second update")
	}

	output := model.View()
	for _, want := range []string{"RUNNING", `"uptime_seconds": 47`, `"process_count": 8`, "last update"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in task view, got %q", want, output)
		}
	}
	if strings.Contains(output, "SECRET_TOKEN") {
		t.Fatalf("sensitive update data was rendered: %q", output)
	}

	_, duplicate := model.Update(keyRune('s'))
	if duplicate != nil {
		t.Fatal("expected duplicate start to be rejected")
	}
	if output := model.View(); !strings.Contains(output, "already running") {
		t.Fatalf("expected duplicate feedback, got %q", output)
	}
}

func TestSocketsProfileUsesTypedDefaultFiltersAndSanitizesProcesses(t *testing.T) {
	store := readyTaskStore(state.Node{
		ID:      "tcp://10.0.0.2:50051",
		Name:    "remote",
		Address: "tcp://10.0.0.2:50051",
		Status:  state.NodeStatusReady,
	})
	stream := newFakeTaskStream(42, "tcp://10.0.0.2:50051", controller.TaskSocketsMonitor)
	manager := &fakeTaskManager{stream: stream}
	model := New(store, theme.New(theme.Options{}), manager).(*Model)
	model.SetSize(100, 36)
	model.Update(tea.KeyMsg{Type: tea.KeyDown})

	_, startCmd := model.Update(keyRune('s'))
	_, waitCmd := model.Update(startCmd())
	call := manager.snapshot()
	config, ok := call.request.Data.(controller.SocketsMonitorConfig)
	if !ok {
		t.Fatalf("unexpected sockets config type %T", call.request.Data)
	}
	if config.Interval != taskInterval || config.State != 0 || config.Proto != 0 || config.Family != 0 {
		t.Fatalf("unexpected default socket filters: %+v", config)
	}

	stream.updates <- controller.TaskUpdate{Data: json.RawMessage(
		`{"Table":[{"Socket":{"Family":2,"State":1},"PID":12,"Proto":6},{"Socket":{"Family":10,"State":7},"PID":13,"Proto":17}],"Processes":{"12":{"Path":"/private/process","Args":["--token","secret"]}}}`,
	)}
	_, _ = model.Update(waitCmd())

	output := model.View()
	for _, want := range []string{
		"state any (0)",
		"protocol any (0)",
		"family any (0)",
		`"socket_count": 2`,
		`"6": 1`,
		`"17": 1`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in sockets view, got %q", want, output)
		}
	}
	for _, secret := range []string{"/private/process", "--token", "secret"} {
		if strings.Contains(output, secret) {
			t.Fatalf("sensitive process detail %q was rendered: %q", secret, output)
		}
	}
}

func TestTaskViewStopAcknowledgementAndIdleGuard(t *testing.T) {
	model, manager, _, waitCmd := startNodeTask(t)
	call := manager.snapshot()

	_, stopCmd := model.Update(keyRune('x'))
	if stopCmd == nil {
		t.Fatal("expected asynchronous stop command")
	}
	if got := manager.snapshot().stopCalls; got != 0 {
		t.Fatalf("Update blocked on StopTask; got %d calls before command execution", got)
	}
	_, _ = model.Update(stopCmd())

	stopped := manager.snapshot()
	if stopped.stopCalls != 1 || stopped.stoppedTask == nil {
		t.Fatalf("unexpected stop call: %+v", stopped)
	}
	if _, ok := stopped.stopCtx.Deadline(); !ok {
		t.Fatal("expected bounded stop request context")
	}
	select {
	case <-call.startCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("task stream context was not canceled after stop acknowledgement")
	}
	if runtime := model.runtime(taskKey{nodeID: "node-1", name: controller.TaskNodeMonitor}); runtime.phase != phaseIdle {
		t.Fatalf("expected idle task after stop, got %s", runtime.phase)
	}
	if output := model.View(); !strings.Contains(output, "stop acknowledged") {
		t.Fatalf("expected stop acknowledgement, got %q", output)
	}

	_, idleStop := model.Update(keyRune('x'))
	if idleStop != nil {
		t.Fatal("expected idle stop to be rejected")
	}
	if output := model.View(); !strings.Contains(output, "is idle") {
		t.Fatalf("expected idle feedback, got %q", output)
	}

	if waitCmd == nil {
		t.Fatal("expected original recurring command")
	}
}

func TestTaskViewStopErrorKeepsStreamRunning(t *testing.T) {
	model, manager, _, _ := startNodeTask(t)
	manager.mu.Lock()
	manager.stopErr = errors.New("stop unavailable")
	manager.mu.Unlock()

	_, stopCmd := model.Update(keyRune('x'))
	_, _ = model.Update(stopCmd())

	runtime := model.runtime(taskKey{nodeID: "node-1", name: controller.TaskNodeMonitor})
	if runtime.phase != phaseRunning || runtime.stream == nil {
		t.Fatalf("expected stream to remain running after stop error, got %+v", runtime)
	}
	if output := model.View(); !strings.Contains(output, "stop unavailable") {
		t.Fatalf("expected stop error feedback, got %q", output)
	}
}

func TestTaskViewHandlesStartAndStreamErrors(t *testing.T) {
	t.Run("start", func(t *testing.T) {
		store := readyTaskStore(readyNode("node-1"))
		manager := &fakeTaskManager{startErr: errors.New("not supported")}
		model := New(store, theme.New(theme.Options{}), manager).(*Model)

		_, cmd := model.Update(keyRune('s'))
		_, _ = model.Update(cmd())

		runtime := model.runtime(taskKey{nodeID: "node-1", name: controller.TaskNodeMonitor})
		if runtime.phase != phaseError {
			t.Fatalf("expected start error state, got %s", runtime.phase)
		}
		if output := model.View(); !strings.Contains(output, "not supported") {
			t.Fatalf("expected start error feedback, got %q", output)
		}
	})

	t.Run("stream", func(t *testing.T) {
		model, _, stream, waitCmd := startNodeTask(t)
		stream.finish(errors.New("connection lost"))
		_, _ = model.Update(waitCmd())

		runtime := model.runtime(taskKey{nodeID: "node-1", name: controller.TaskNodeMonitor})
		if runtime.phase != phaseError || runtime.stream != nil {
			t.Fatalf("expected closed error state, got %+v", runtime)
		}
		if output := model.View(); !strings.Contains(output, "connection lost") {
			t.Fatalf("expected stream error feedback, got %q", output)
		}
	})
}

func TestTaskViewDisconnectCancelsContextAndIgnoresStaleUpdates(t *testing.T) {
	model, manager, stream, _ := startNodeTask(t)
	call := manager.snapshot()
	key := taskKey{nodeID: "node-1", name: controller.TaskNodeMonitor}
	oldGeneration := model.runtime(key).generation

	store := model.store
	store.UpdateNodeStatus("node-1", state.NodeStatusDisconnected, "gone", time.Now())
	model.Update(struct{}{})

	select {
	case <-call.startCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("task context was not canceled on disconnect")
	}
	runtime := model.runtime(key)
	if runtime.phase != phaseError || runtime.stream != nil {
		t.Fatalf("expected disconnected error state, got %+v", runtime)
	}
	before := runtime.preview
	model.Update(taskUpdateMsg{
		key:        key,
		generation: oldGeneration,
		streamID:   stream.ID(),
		update: controller.TaskUpdate{Data: json.RawMessage(
			`{"Uptime":999,"Loads":[0,0,0]}`,
		)},
	})
	if runtime.preview != before {
		t.Fatalf("stale task update changed preview from %q to %q", before, runtime.preview)
	}
	if output := model.View(); !strings.Contains(output, "node disconnected") {
		t.Fatalf("expected disconnect feedback, got %q", output)
	}
}

func TestTaskViewCleansUpStaleStartResult(t *testing.T) {
	store := readyTaskStore(readyNode("node-1"))
	stream := newFakeTaskStream(52, "node-1", controller.TaskNodeMonitor)
	manager := &fakeTaskManager{stream: stream}
	model := New(store, theme.New(theme.Options{}), manager).(*Model)

	_, startCmd := model.Update(keyRune('s'))
	store.UpdateNodeStatus("node-1", state.NodeStatusDisconnected, "gone", time.Now())
	model.Update(struct{}{})

	_, cleanupCmd := model.Update(startCmd())
	if cleanupCmd == nil {
		t.Fatal("expected stale stream cleanup command")
	}
	_ = cleanupCmd()
	if got := manager.snapshot().stopCalls; got != 1 {
		t.Fatalf("expected stale stream to be stopped, got %d calls", got)
	}
}

func TestTaskViewSelectionAndInvalidActions(t *testing.T) {
	store := readyTaskStore(
		readyNode("node-1"),
		state.Node{ID: "node-2", Name: "offline", Status: state.NodeStatusDisconnected},
	)
	manager := &fakeTaskManager{}
	model := New(store, theme.New(theme.Options{}), manager).(*Model)
	model.SetSize(80, 30)

	model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if model.nodeIdx != 0 {
		t.Fatalf("left moved before first node: %d", model.nodeIdx)
	}
	model.Update(tea.KeyMsg{Type: tea.KeyRight})
	if model.nodeIdx != 1 {
		t.Fatalf("right did not select second node: %d", model.nodeIdx)
	}
	_, disconnectedStart := model.Update(keyRune('s'))
	if disconnectedStart != nil || manager.snapshot().startCalls != 0 {
		t.Fatal("expected disconnected start to be rejected")
	}
	if output := model.View(); !strings.Contains(output, "not connected") {
		t.Fatalf("expected disconnected feedback, got %q", output)
	}

	model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if model.profileIdx != 2 {
		t.Fatalf("expected PID profile selection, got %d", model.profileIdx)
	}
	_, unavailableStart := model.Update(keyRune('s'))
	if unavailableStart != nil {
		t.Fatal("expected unavailable PID monitor not to start")
	}
	if output := model.View(); !strings.Contains(output, "PID-selection flow") {
		t.Fatalf("expected PID unavailable feedback, got %q", output)
	}

	model.Update(keyRune('j'))
	if model.profileIdx != 2 {
		t.Fatal("vi key unexpectedly changed task selection")
	}
}

func TestTaskViewCloseCancelsOwnedContexts(t *testing.T) {
	model, manager, _, _ := startNodeTask(t)
	ctx := manager.snapshot().startCtx
	model.Close()
	model.Close()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel task context")
	}
}

func TestTaskViewFitsTerminalBounds(t *testing.T) {
	for _, size := range []struct {
		width  int
		height int
	}{
		{width: 120, height: 40},
		{width: 80, height: 40},
	} {
		store := readyTaskStore(state.Node{
			ID:      "tcp://10.0.0.2:50051",
			Name:    "a-node-with-a-long-display-name",
			Address: "tcp://10.0.0.2:50051",
			Status:  state.NodeStatusReady,
		})
		model := New(store, theme.New(theme.Options{}), nil).(*Model)
		model.SetSize(size.width, size.height)
		runtime := model.runtime(taskKey{
			nodeID: "tcp://10.0.0.2:50051",
			name:   controller.TaskNodeMonitor,
		})
		runtime.phase = phaseRunning
		runtime.lastUpdate = time.Now()
		runtime.preview = strings.Repeat(`{"bounded":"value"}`+"\n", 30)

		output := model.View()
		if got := lipgloss.Width(output); got > size.width {
			t.Fatalf("rendered width %d exceeds terminal width %d", got, size.width)
		}
		if got := lipgloss.Height(output); got > size.height {
			t.Fatalf("rendered height %d exceeds terminal height %d", got, size.height)
		}
	}
}

func TestNodeMonitorTargetV18Identity(t *testing.T) {
	tests := []struct {
		node state.Node
		want string
	}{
		{node: state.Node{ID: "unix://@opensnitch"}, want: "unix:/local"},
		{node: state.Node{ID: "configured", Address: "unix:///run/opensnitch.sock"}, want: "unix:/local"},
		{node: state.Node{ID: "tcp://10.0.0.2:50051"}, want: "10.0.0.2:50051"},
		{
			node: state.Node{ID: "peer", Address: "tcp://10.0.0.3:50051"},
			want: "10.0.0.3:50051",
		},
	}
	for _, test := range tests {
		if got := nodeMonitorTarget(test.node); got != test.want {
			t.Fatalf("nodeMonitorTarget(%+v) = %q, want %q", test.node, got, test.want)
		}
	}
}

func startNodeTask(t *testing.T) (*Model, *fakeTaskManager, *fakeTaskStream, tea.Cmd) {
	t.Helper()
	store := readyTaskStore(readyNode("node-1"))
	stream := newFakeTaskStream(51, "node-1", controller.TaskNodeMonitor)
	manager := &fakeTaskManager{stream: stream}
	model := New(store, theme.New(theme.Options{}), manager).(*Model)
	model.SetSize(100, 36)

	_, startCmd := model.Update(keyRune('s'))
	if startCmd == nil {
		t.Fatal("expected start command")
	}
	_, waitCmd := model.Update(startCmd())
	if waitCmd == nil {
		t.Fatal("expected wait command")
	}
	return model, manager, stream, waitCmd
}

func readyTaskStore(nodes ...state.Node) *state.Store {
	store := state.NewStore()
	store.SetNodes(nodes)
	return store
}

func readyNode(id string) state.Node {
	return state.Node{ID: id, Name: id, Status: state.NodeStatusReady}
}

func keyRune(value rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{value}}
}
