package root

import (
	"context"
	"encoding/json"
	"errors"
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

type rootFirewallManager struct {
	err     error
	started chan struct{}
	release chan struct{}
}

func (m *rootFirewallManager) EnableFirewall(context.Context, string) error {
	return m.run()
}

func (m *rootFirewallManager) DisableFirewall(context.Context, string) error {
	return m.run()
}

func (m *rootFirewallManager) ReloadFirewall(context.Context, string) error {
	return m.run()
}

func (m *rootFirewallManager) run() error {
	if m.started != nil {
		close(m.started)
	}
	if m.release != nil {
		<-m.release
	}
	return m.err
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

func TestFirewallResultsReachInactiveFirewallView(t *testing.T) {
	tests := []struct {
		name    string
		key     rune
		running bool
		err     error
		want    string
	}{
		{name: "enable success", key: 'e', running: false, want: "acknowledged for alpha"},
		{name: "enable error", key: 'e', running: false, err: errors.New("enable failed"), want: "enable failed"},
		{name: "disable success", key: 'd', running: true, want: "acknowledged for alpha"},
		{name: "disable error", key: 'd', running: true, err: errors.New("disable failed"), want: "disable failed"},
		{name: "reload success", key: 'r', running: true, want: "acknowledged for alpha"},
		{name: "reload error", key: 'r', running: true, err: errors.New("reload failed"), want: "reload failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := state.NewStore()
			store.SetNodes([]state.Node{{ID: "node-1", Name: "alpha", Status: state.NodeStatusReady}})
			store.SetSystemFirewall("node-1", state.SystemFirewall{
				Enabled: tt.running,
				Running: tt.running,
			})
			manager := &rootFirewallManager{
				err:     tt.err,
				started: make(chan struct{}),
				release: make(chan struct{}),
			}
			model := New(store, Options{
				Theme:    theme.New(theme.Options{}),
				Firewall: manager,
			})
			model.Update(tea.WindowSizeMsg{Width: 100, Height: 36})
			model.active = state.ViewFirewall
			store.SetActiveView(state.ViewFirewall)

			_, actionBatch := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tt.key}})
			if actionBatch == nil {
				t.Fatal("expected firewall action command")
			}
			resultCh := make(chan tea.Msg, 1)
			go func() {
				resultCh <- actionBatch()
			}()
			<-manager.started
			model.cycle(1)
			if model.active == state.ViewFirewall {
				t.Fatal("expected firewall view to become inactive")
			}

			close(manager.release)
			result := <-resultCh
			model.Update(result)
			model.active = state.ViewFirewall
			store.SetActiveView(state.ViewFirewall)
			output := model.View()
			if strings.Contains(output, "in progress") {
				t.Fatalf("inactive result did not clear progress: %q", output)
			}
			if !strings.Contains(output, tt.want) {
				t.Fatalf("inactive result feedback missing %q: %q", tt.want, output)
			}
		})
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
