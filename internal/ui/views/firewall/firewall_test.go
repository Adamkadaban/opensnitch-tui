package firewall

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
)

type fakeFirewallController struct {
	action      string
	nodeID      string
	err         error
	hasDeadline bool
}

func (f *fakeFirewallController) EnableFirewall(ctx context.Context, nodeID string) error {
	return f.record(ctx, "enable", nodeID)
}

func (f *fakeFirewallController) DisableFirewall(ctx context.Context, nodeID string) error {
	return f.record(ctx, "disable", nodeID)
}

func (f *fakeFirewallController) ReloadFirewall(ctx context.Context, nodeID string) error {
	return f.record(ctx, "reload", nodeID)
}

func (f *fakeFirewallController) record(ctx context.Context, action, nodeID string) error {
	f.action = action
	f.nodeID = nodeID
	_, f.hasDeadline = ctx.Deadline()
	return f.err
}

var _ controller.FirewallManager = (*fakeFirewallController)(nil)

func TestFirewallViewEmptyState(t *testing.T) {
	store := state.NewStore()
	store.SetNodes([]state.Node{{ID: "node-1", Status: state.NodeStatusReady}})
	model := New(store, theme.New(theme.Options{}), nil)
	model.SetSize(80, 20)

	if output := model.View(); !strings.Contains(output, "No system firewall data") {
		t.Fatalf("expected firewall empty state, got %q", output)
	}

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if cmd != nil {
		t.Fatal("expected no command without a selected firewall node")
	}
	if output := model.View(); !strings.Contains(output, "No firewall node selected") {
		t.Fatalf("expected visible no-selection error, got %q", output)
	}
}

func TestFirewallViewNodeAndChainSelection(t *testing.T) {
	store := state.NewStore()
	store.SetNodes([]state.Node{
		{ID: "node-1", Name: "alpha", Status: state.NodeStatusReady},
		{ID: "node-2", Name: "no-firewall", Status: state.NodeStatusReady},
		{ID: "node-3", Name: "charlie", Status: state.NodeStatusReady},
	})
	store.SetSystemFirewall("node-1", testFirewall(false, "alpha-output", "alpha-input"))
	store.SetSystemFirewall("node-3", testFirewall(true, "charlie-output"))
	model := New(store, theme.New(theme.Options{}), nil).(*Model)
	model.SetSize(100, 30)

	model.Update(tea.KeyMsg{Type: tea.KeyRight})
	if model.nodeIdx != 1 {
		t.Fatalf("expected second firewall node selected, got index %d", model.nodeIdx)
	}
	if output := model.View(); !strings.Contains(output, "charlie-output") || strings.Contains(output, "no-firewall") {
		t.Fatalf("expected selection to skip nodes without firewall data, got %q", output)
	}

	model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if model.chainIdx != 1 {
		t.Fatalf("expected second chain selected, got index %d", model.chainIdx)
	}
	if output := model.View(); !strings.Contains(output, "inet/opensnitch/alpha-input") {
		t.Fatalf("expected selected chain detail, got %q", output)
	}
}

func TestFirewallViewSelectionTracksNodeIDAcrossInsertAndReorder(t *testing.T) {
	store := state.NewStore()
	store.SetNodes([]state.Node{
		{ID: "node-a", Name: "alpha", Status: state.NodeStatusReady},
		{ID: "node-c", Name: "charlie", Status: state.NodeStatusReady},
	})
	store.SetSystemFirewall("node-a", testFirewall(true, "alpha-output"))
	store.SetSystemFirewall("node-c", testFirewall(true, "charlie-output"))
	ctrl := &fakeFirewallController{}
	model := New(store, theme.New(theme.Options{}), ctrl).(*Model)

	model.Update(tea.KeyMsg{Type: tea.KeyRight})
	if model.selectedID != "node-c" {
		t.Fatalf("expected node C selected, got %q", model.selectedID)
	}

	store.SetNodes([]state.Node{
		{ID: "node-b", Name: "bravo", Status: state.NodeStatusReady},
		{ID: "node-a", Name: "alpha", Status: state.NodeStatusReady},
		{ID: "node-c", Name: "charlie", Status: state.NodeStatusReady},
	})
	store.SetSystemFirewall("node-b", testFirewall(true, "bravo-output"))

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd == nil {
		t.Fatal("expected reload command")
	}
	cmd()
	if ctrl.nodeID != "node-c" {
		t.Fatalf("expected action to remain targeted at node C, got %q", ctrl.nodeID)
	}
	if model.nodeIdx != 2 {
		t.Fatalf("expected derived node index 2 after reorder, got %d", model.nodeIdx)
	}
}

func TestFirewallViewSelectedNodeRemovalUsesDeterministicNeighbor(t *testing.T) {
	tests := []struct {
		name       string
		selectNext int
		removeID   string
		wantID     string
		wantIdx    int
	}{
		{name: "middle selects next", selectNext: 1, removeID: "node-b", wantID: "node-c", wantIdx: 1},
		{name: "last selects previous", selectNext: 2, removeID: "node-c", wantID: "node-b", wantIdx: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := state.NewStore()
			store.SetNodes([]state.Node{
				{ID: "node-a", Name: "alpha", Status: state.NodeStatusReady},
				{ID: "node-b", Name: "bravo", Status: state.NodeStatusReady},
				{ID: "node-c", Name: "charlie", Status: state.NodeStatusReady},
			})
			for _, nodeID := range []string{"node-a", "node-b", "node-c"} {
				store.SetSystemFirewall(nodeID, testFirewall(true, nodeID+"-output"))
			}
			model := New(store, theme.New(theme.Options{}), nil).(*Model)

			for range tt.selectNext {
				model.Update(tea.KeyMsg{Type: tea.KeyRight})
			}
			store.RemoveSystemFirewall(tt.removeID)
			model.View()

			if model.selectedID != tt.wantID || model.nodeIdx != tt.wantIdx {
				t.Fatalf(
					"expected fallback %s at index %d, got %s at index %d",
					tt.wantID,
					tt.wantIdx,
					model.selectedID,
					model.nodeIdx,
				)
			}
		})
	}
}

func TestFirewallViewKeyActionsAndAsyncResults(t *testing.T) {
	tests := []struct {
		key     rune
		running bool
		action  string
	}{
		{key: 'e', running: false, action: "enable"},
		{key: 'd', running: true, action: "disable"},
		{key: 'r', running: true, action: "reload"},
	}

	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			store := state.NewStore()
			store.SetNodes([]state.Node{{ID: "node-1", Name: "alpha", Status: state.NodeStatusReady}})
			store.SetSystemFirewall("node-1", testFirewall(tt.running, "output"))
			ctrl := &fakeFirewallController{}
			model := New(store, theme.New(theme.Options{}), ctrl).(*Model)
			model.SetSize(100, 30)

			_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tt.key}})
			if cmd == nil {
				t.Fatalf("expected %s command", tt.action)
			}
			if model.inProgress != action(tt.action) || !strings.Contains(model.View(), "in progress") {
				t.Fatalf("expected visible %s progress state, got %q", tt.action, model.View())
			}

			result := cmd()
			if ctrl.action != tt.action || ctrl.nodeID != "node-1" {
				t.Fatalf("unexpected controller request: %+v", ctrl)
			}
			if !ctrl.hasDeadline {
				t.Fatal("expected UI command context to have a deadline")
			}
			model.Update(result)
			if model.inProgress != "" {
				t.Fatalf("expected %s progress to clear", tt.action)
			}
			if output := model.View(); !strings.Contains(output, "acknowledged") {
				t.Fatalf("expected success feedback, got %q", output)
			}
		})
	}
}

func TestFirewallViewResultNamesOriginalActionNode(t *testing.T) {
	store := state.NewStore()
	store.SetNodes([]state.Node{
		{ID: "node-a", Name: "alpha", Status: state.NodeStatusReady},
		{ID: "node-c", Name: "charlie", Status: state.NodeStatusReady},
	})
	store.SetSystemFirewall("node-a", testFirewall(true, "alpha-output"))
	store.SetSystemFirewall("node-c", testFirewall(true, "charlie-output"))
	ctrl := &fakeFirewallController{}
	model := New(store, theme.New(theme.Options{}), ctrl).(*Model)

	model.Update(tea.KeyMsg{Type: tea.KeyRight})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model.Update(cmd())

	if output := model.View(); !strings.Contains(output, "acknowledged for charlie") {
		t.Fatalf("expected feedback for original action node, got %q", output)
	}
}

func TestFirewallViewIgnoresStaleResults(t *testing.T) {
	store := state.NewStore()
	store.SetNodes([]state.Node{
		{ID: "node-a", Name: "alpha", Status: state.NodeStatusReady},
		{ID: "node-c", Name: "charlie", Status: state.NodeStatusReady},
	})
	store.SetSystemFirewall("node-a", testFirewall(true, "alpha-output"))
	store.SetSystemFirewall("node-c", testFirewall(true, "charlie-output"))
	ctrl := &fakeFirewallController{}
	model := New(store, theme.New(theme.Options{}), ctrl).(*Model)

	_, firstCmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	firstResult := firstCmd()
	model.Update(firstResult)

	_, secondCmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	secondGeneration := model.inProgressGeneration
	model.Update(firstResult)
	if model.inProgress != actionReload || model.inProgressGeneration != secondGeneration {
		t.Fatal("stale generation cleared the current action")
	}
	model.Update(firewallResultMsg{
		action:     actionReload,
		nodeID:     "node-c",
		nodeName:   "charlie",
		generation: secondGeneration,
	})
	if model.inProgress != actionReload {
		t.Fatal("stale node result cleared the current action")
	}

	model.Update(secondCmd())
	if model.inProgress != "" {
		t.Fatal("current result did not clear the action")
	}
}

func TestFirewallViewAsyncErrorFeedback(t *testing.T) {
	store := state.NewStore()
	store.SetNodes([]state.Node{{ID: "node-1", Name: "alpha", Status: state.NodeStatusReady}})
	store.SetSystemFirewall("node-1", testFirewall(true, "output"))
	ctrl := &fakeFirewallController{err: errors.New("permission denied")}
	model := New(store, theme.New(theme.Options{}), ctrl).(*Model)
	model.SetSize(100, 30)

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	model.Update(cmd())

	if output := model.View(); !strings.Contains(output, "permission denied") || !model.statusError {
		t.Fatalf("expected visible error feedback, got %q", output)
	}
}

func TestFirewallViewPreventsInvalidActions(t *testing.T) {
	tests := []struct {
		name    string
		status  state.NodeStatus
		running bool
		key     rune
		message string
	}{
		{name: "disconnected", status: state.NodeStatusDisconnected, running: false, key: 'e', message: "not connected"},
		{name: "already running", status: state.NodeStatusReady, running: true, key: 'e', message: "already running"},
		{name: "already stopped", status: state.NodeStatusReady, running: false, key: 'd', message: "already stopped"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := state.NewStore()
			store.SetNodes([]state.Node{{ID: "node-1", Name: "alpha", Status: tt.status}})
			store.SetSystemFirewall("node-1", testFirewall(tt.running, "output"))
			ctrl := &fakeFirewallController{}
			model := New(store, theme.New(theme.Options{}), ctrl).(*Model)
			model.SetSize(80, 20)

			_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tt.key}})
			if cmd != nil {
				t.Fatal("expected invalid action not to return a command")
			}
			if ctrl.action != "" {
				t.Fatalf("expected controller not to be called, got %s", ctrl.action)
			}
			if output := model.View(); !strings.Contains(output, tt.message) {
				t.Fatalf("expected %q feedback, got %q", tt.message, output)
			}
		})
	}
}

func TestFirewallViewFitsTerminalBounds(t *testing.T) {
	for _, size := range []struct {
		width  int
		height int
	}{
		{width: 120, height: 40},
		{width: 80, height: 40},
	} {
		store := state.NewStore()
		store.SetNodes([]state.Node{{
			ID:      "node-1",
			Name:    "a-node-with-a-long-display-name",
			Version: "1.6.9",
			Status:  state.NodeStatusReady,
		}})
		firewall := testFirewall(true,
			"output-with-a-long-chain-name",
			"input-with-a-long-chain-name",
			"forward-with-a-long-chain-name",
		)
		store.SetSystemFirewall("node-1", firewall)
		model := New(store, theme.New(theme.Options{}), nil)
		model.SetSize(size.width, size.height)

		output := model.View()
		if got := lipgloss.Width(output); got > size.width {
			t.Fatalf("rendered width %d exceeds terminal width %d", got, size.width)
		}
		if got := lipgloss.Height(output); got > size.height {
			t.Fatalf("rendered height %d exceeds terminal height %d", got, size.height)
		}
	}
}

func testFirewall(running bool, chainNames ...string) state.SystemFirewall {
	chains := make([]state.FirewallChain, 0, len(chainNames))
	for idx, name := range chainNames {
		chains = append(chains, state.FirewallChain{
			Name:     name,
			Table:    "opensnitch",
			Family:   "inet",
			Priority: "0",
			Type:     "filter",
			Hook:     "output",
			Policy:   "accept",
			Rules: []state.FirewallRule{{
				UUID:        "rule-" + time.Duration(idx).String(),
				Description: "allow traffic to a long destination description",
				Target:      "accept",
			}},
		})
	}
	return state.SystemFirewall{
		Enabled: running,
		Running: running,
		Version: 2,
		SystemRules: []state.FirewallRuleGroup{{
			Chains: chains,
		}},
	}
}
