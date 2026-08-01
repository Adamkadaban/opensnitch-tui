package root

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	"github.com/adamkadaban/opensnitch-tui/internal/keymap"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
)

type rootTaskManager struct {
	stream controller.TaskStream
}

func (f *rootTaskManager) StartTask(
	context.Context,
	string,
	controller.TaskRequest,
) (controller.TaskStream, error) {
	return f.stream, nil
}

func (f *rootTaskManager) StopTask(context.Context, controller.TaskStream) error {
	return nil
}

type rootTaskStream struct {
	updates chan controller.TaskUpdate
	done    chan struct{}
}

func (f *rootTaskStream) ID() uint64                            { return 1 }
func (f *rootTaskStream) NodeID() string                        { return "node-1" }
func (f *rootTaskStream) Name() controller.TaskName             { return controller.TaskNodeMonitor }
func (f *rootTaskStream) Updates() <-chan controller.TaskUpdate { return f.updates }
func (f *rootTaskStream) Done() <-chan struct{}                 { return f.done }
func (f *rootTaskStream) Err() error                            { return nil }

func TestFooterLineIncludesError(t *testing.T) {
	th := theme.New(theme.Options{})
	km := keymap.DefaultGlobal()

	model := &Model{keymap: km, theme: th}
	snapshot := state.Snapshot{ActiveView: state.ViewDashboard, Nodes: []state.Node{{}}, LastError: "boom"}

	line := model.footerLine(snapshot)
	if !strings.Contains(line, "boom") {
		t.Fatalf("expected footer to include error text, got %q", line)
	}
}

func TestFooterLineWithoutError(t *testing.T) {
	th := theme.New(theme.Options{})
	km := keymap.DefaultGlobal()

	model := &Model{keymap: km, theme: th}
	snapshot := state.Snapshot{ActiveView: state.ViewDashboard, Nodes: []state.Node{{}}, LastError: ""}

	line := model.footerLine(snapshot)
	if strings.Contains(line, "boom") {
		t.Fatalf("did not expect footer to include error text, got %q", line)
	}
}

func TestViewFitsWindow(t *testing.T) {
	for _, size := range []struct {
		width  int
		height int
	}{
		{width: 120, height: 40},
		{width: 80, height: 40},
	} {
		store := state.NewStore()
		model := New(store, Options{Theme: theme.New(theme.Options{})})
		model.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})

		rendered := model.View()
		if got := lipgloss.Width(rendered); got > size.width {
			t.Fatalf("rendered width %d exceeds terminal width %d", got, size.width)
		}
		if got := lipgloss.Height(rendered); got > size.height {
			t.Fatalf("rendered height %d exceeds terminal height %d", got, size.height)
		}
	}
}

func TestNewIncludesManagedViews(t *testing.T) {
	model := New(state.NewStore(), Options{Theme: theme.New(theme.Options{})})
	if model.views[state.ViewFirewall] == nil {
		t.Fatal("expected firewall view to be registered")
	}
	if indexOf(model.order, state.ViewFirewall) == 0 {
		t.Fatal("expected firewall view in routed view order")
	}
	if model.views[state.ViewTasks] == nil {
		t.Fatal("expected tasks view to be registered")
	}
	if indexOf(model.order, state.ViewTasks) == 0 {
		t.Fatal("expected tasks view in routed view order")
	}
}

func TestTaskMessagesReachInactiveTasksView(t *testing.T) {
	store := state.NewStore()
	store.SetNodes([]state.Node{{ID: "node-1", Name: "alpha", Status: state.NodeStatusReady}})
	stream := &rootTaskStream{
		updates: make(chan controller.TaskUpdate, 1),
		done:    make(chan struct{}),
	}
	model := New(store, Options{
		Theme: theme.New(theme.Options{}),
		Tasks: &rootTaskManager{stream: stream},
	})
	model.Update(tea.WindowSizeMsg{Width: 100, Height: 36})
	model.active = state.ViewTasks
	store.SetActiveView(state.ViewTasks)

	_, startBatch := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	started := runSingleCommand(t, startBatch)

	model.cycle(1)
	if model.active == state.ViewTasks {
		t.Fatal("expected tasks view to become inactive")
	}
	_, waitBatch := model.Update(started)

	stream.updates <- controller.TaskUpdate{Data: json.RawMessage(
		`{"Uptime":9,"Loads":[65536,0,0]}`,
	)}
	_, nextBatch := model.Update(runSingleCommand(t, waitBatch))
	if nextBatch == nil {
		t.Fatal("expected recurring task command while view is inactive")
	}

	model.active = state.ViewTasks
	store.SetActiveView(state.ViewTasks)
	if output := model.View(); !strings.Contains(output, `"uptime_seconds": 9`) {
		t.Fatalf("inactive task update was lost: %q", output)
	}
}

func runSingleCommand(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected command")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return msg
	}
	var commands []tea.Cmd
	for _, candidate := range batch {
		if candidate != nil {
			commands = append(commands, candidate)
		}
	}
	if len(commands) != 1 {
		t.Fatalf("expected one batch command, got %d", len(commands))
	}
	return runSingleCommand(t, commands[0])
}
