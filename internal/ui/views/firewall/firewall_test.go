package firewall

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
)

type fakeController struct {
	action string
	nodeID string
	err    error
}

func (f *fakeController) EnableFirewall(id string) error {
	f.action = "enable"
	f.nodeID = id
	return f.err
}

func (f *fakeController) DisableFirewall(id string) error {
	f.action = "disable"
	f.nodeID = id
	return f.err
}

func (f *fakeController) ReloadFirewall(id string) error {
	f.action = "reload"
	f.nodeID = id
	return f.err
}

var _ controller.Firewall = (*fakeController)(nil)

func TestFirewallViewEmpty(t *testing.T) {
	store := state.NewStore()
	th := theme.New(theme.Options{})
	view := New(store, th, nil)
	view.SetSize(80, 20)

	if out := view.View(); !strings.Contains(out, "No nodes connected") {
		t.Fatalf("expected empty-state copy, got %q", out)
	}
}

func TestFirewallActionDispatch(t *testing.T) {
	store := state.NewStore()
	store.SetNodes([]state.Node{{ID: "node-1", Name: "alpha", Address: "10.0.0.2", Status: state.NodeStatusReady}})
	store.SetFirewall("node-1", state.Firewall{Enabled: true, Chains: []state.FirewallChain{{Name: "output"}}})
	th := theme.New(theme.Options{})
	ctrl := &fakeController{}
	view := New(store, th, ctrl)
	view.SetSize(80, 20)

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}}
	view.Update(msg)

	if ctrl.action != "enable" || ctrl.nodeID != "node-1" {
		t.Fatalf("expected enable action for node-1, got action=%s node=%s", ctrl.action, ctrl.nodeID)
	}

	out := view.View()
	if !strings.Contains(strings.ToLower(out), "requested enable") {
		t.Fatalf("expected status line in view, got %q", out)
	}
}

func TestFirewallSelectionMoves(t *testing.T) {
	store := state.NewStore()
	store.SetNodes([]state.Node{
		{ID: "node-1", Name: "alpha", Address: "10.0.0.2", Status: state.NodeStatusReady},
		{ID: "node-2", Name: "beta", Address: "10.0.0.3", Status: state.NodeStatusReady},
	})
	store.SetFirewall("node-1", state.Firewall{Enabled: true})
	store.SetFirewall("node-2", state.Firewall{Enabled: false})
	th := theme.New(theme.Options{})
	view := New(store, th, &fakeController{})
	view.SetSize(80, 20)

	down := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	view.Update(down)

	out := view.View()
	if !strings.Contains(out, "beta") {
		t.Fatalf("expected selection to move to beta, got %q", out)
	}
}

func TestFirewallViewportSizing(t *testing.T) {
	store := state.NewStore()
	store.SetNodes([]state.Node{{ID: "node-1", Name: "alpha", Address: "10.0.0.2"}})
	store.SetFirewall("node-1", state.Firewall{})
	th := theme.New(theme.Options{})
	model, ok := New(store, th, nil).(*Model)
	if !ok {
		t.Fatal("expected *Model type")
	}
	model.SetSize(100, 15)
	if model.viewport.Height != 15 {
		t.Fatalf("expected viewport height 15, got %d", model.viewport.Height)
	}
	if model.viewport.Width != 100 {
		t.Fatalf("expected viewport width 100, got %d", model.viewport.Width)
	}
}
