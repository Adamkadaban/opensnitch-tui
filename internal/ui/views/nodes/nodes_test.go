package nodes

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
)

type configManagerStub struct {
	calls  int
	nodeID string
	config state.NodeDaemonConfig
	err    error
}

func (m *configManagerStub) ApplyNodeConfig(
	_ context.Context,
	nodeID string,
	config state.NodeDaemonConfig,
) error {
	m.calls++
	m.nodeID = nodeID
	m.config = config
	return m.err
}

func TestNodesSelectionDetailEditSaveAndSuccess(t *testing.T) {
	store := configuredNodeStore()
	manager := &configManagerStub{}
	model := newTestModel(store, manager)

	model.Update(key("down"))
	if model.selectedID != "node-2" {
		t.Fatalf("expected node-2 selected, got %q", model.selectedID)
	}
	model.Update(key("enter"))
	model.Update(key("e"))
	model.Update(key("right"))
	if !model.editing || model.draft.DefaultAction == model.baseline.DefaultAction {
		t.Fatalf("expected edited draft, got %+v", model.draft)
	}

	_, cmd := model.Update(key("s"))
	if cmd == nil || !model.applying {
		t.Fatal("expected asynchronous apply command")
	}
	result := cmd()
	if manager.calls != 1 || manager.nodeID != "node-2" {
		t.Fatalf("unexpected apply call: %+v", manager)
	}
	model.Update(result)
	if model.applying || model.editing {
		t.Fatal("expected successful apply to leave idle read-only mode")
	}
	if !strings.Contains(model.statusLine, "successfully") {
		t.Fatalf("expected success status, got %q", model.statusLine)
	}
}

func TestNodesCancelRestoresDraftAndBacksOut(t *testing.T) {
	model := newTestModel(configuredNodeStore(), &configManagerStub{})
	model.Update(key("enter"))
	model.Update(key("e"))
	model.Update(key("right"))
	model.Update(key("esc"))
	if model.editing || model.draft != model.baseline {
		t.Fatal("expected escape to cancel edits")
	}
	model.Update(key("esc"))
	if model.detail {
		t.Fatal("expected escape outside edit mode to return to node list")
	}
}

func TestNodesAsyncErrorAndStaleResultProtection(t *testing.T) {
	manager := &configManagerStub{err: errors.New("daemon rejected config")}
	model := newTestModel(configuredNodeStore(), manager)
	model.Update(key("enter"))
	model.Update(key("e"))
	model.Update(key("right"))
	_, cmd := model.Update(key("s"))
	result := cmd().(configResultMsg)

	model.generation++
	model.Update(result)
	if !model.applying {
		t.Fatal("stale result should not alter current apply state")
	}

	result.generation = model.generation
	model.Update(result)
	if model.applying || !model.statusErr || !strings.Contains(model.statusLine, "daemon rejected") {
		t.Fatalf("expected asynchronous error status, got applying=%v status=%q", model.applying, model.statusLine)
	}
}

func TestNodesRejectInvalidAndDisconnectedConfig(t *testing.T) {
	store := configuredNodeStore()
	manager := &configManagerStub{}
	model := newTestModel(store, manager)
	model.Update(key("enter"))
	model.Update(key("e"))
	model.draft.DefaultAction = "invalid"
	_, cmd := model.Update(key("s"))
	if cmd != nil || manager.calls != 0 || !strings.Contains(model.statusLine, "Invalid configuration") {
		t.Fatalf("expected invalid enum rejection, got cmd=%v status=%q", cmd != nil, model.statusLine)
	}

	model.draft = model.baseline
	model.draft.LogUTC = !model.draft.LogUTC
	store.UpdateNodeStatus("node-1", state.NodeStatusDisconnected, "offline", state.Node{}.LastSeen)
	_, cmd = model.Update(key("s"))
	if cmd != nil || !strings.Contains(model.statusLine, "not connected") {
		t.Fatalf("expected disconnected rejection, got cmd=%v status=%q", cmd != nil, model.statusLine)
	}
}

func TestNodesControlsClampNumericRanges(t *testing.T) {
	model := newTestModel(configuredNodeStore(), &configManagerStub{})
	model.Update(key("enter"))
	model.Update(key("e"))

	model.fieldIdx = 9
	model.draft.Internal.GCPercent = state.NodeGCPercentMax
	model.adjustField(1)
	if model.draft.Internal.GCPercent != state.NodeGCPercentMax {
		t.Fatalf("GC percent exceeded maximum: %d", model.draft.Internal.GCPercent)
	}
	model.fieldIdx = 12
	model.draft.Stats.MaxEvents = state.NodeMaxEventsMin
	model.adjustField(-1)
	if model.draft.Stats.MaxEvents != state.NodeMaxEventsMin {
		t.Fatalf("max events dropped below minimum: %d", model.draft.Stats.MaxEvents)
	}
	model.fieldIdx = 3
	model.draft.LogLevel = state.NodeLogLevelFatal
	model.adjustField(1)
	if model.draft.LogLevel != state.NodeLogLevelFatal {
		t.Fatalf("log level exceeded maximum: %d", model.draft.LogLevel)
	}
}

func TestNodesMalformedConfigAndSecretsAreNotRendered(t *testing.T) {
	store := state.NewStore()
	store.SetNodes([]state.Node{{
		ID:      "node-1",
		Name:    "alpha",
		Address: "tcp://user:password@example.test:50051?token=QUERY-SECRET#fragment",
		Status:  state.NodeStatusReady,
	}})
	store.SetNodeConfig("node-1", state.NodeConfigState{
		RawJSON:    `{"ClientKey":"TOP-SECRET"`,
		ParseError: "unexpected end of JSON input",
		Metadata: state.NodeConfigMetadata{
			AuthenticationType: "mutual-tls",
			TLSConfigured:      true,
		},
	})
	model := newTestModel(store, &configManagerStub{})
	model.Update(key("enter"))
	output := model.View()
	if !strings.Contains(output, "Configuration parse error") {
		t.Fatalf("expected explicit parse error, got %q", output)
	}
	for _, secret := range []string{"TOP-SECRET", "password", "QUERY-SECRET"} {
		if strings.Contains(output, secret) {
			t.Fatalf("rendered secret %q in %q", secret, output)
		}
	}
	if !strings.Contains(output, "TLS: configured") {
		t.Fatalf("expected redacted TLS metadata, got %q", output)
	}
}

func TestNodesViewFitsSupportedTerminalBounds(t *testing.T) {
	for _, size := range []struct {
		width  int
		height int
	}{
		{width: 120, height: 40},
		{width: 80, height: 40},
	} {
		model := newTestModel(configuredNodeStore(), &configManagerStub{})
		model.SetSize(size.width, size.height)
		model.Update(key("enter"))
		output := model.View()
		if got := lipgloss.Width(output); got > size.width {
			t.Fatalf("%dx%d view width %d exceeds terminal", size.width, size.height, got)
		}
		if got := lipgloss.Height(output); got > size.height {
			t.Fatalf("%dx%d view height %d exceeds terminal", size.width, size.height, got)
		}
	}
}

func configuredNodeStore() *state.Store {
	store := state.NewStore()
	config := validNodeConfig()
	store.SetNodes([]state.Node{
		{ID: "node-2", Name: "beta", Address: "10.0.0.2:50051", Status: state.NodeStatusReady},
		{ID: "node-1", Name: "alpha", Address: "10.0.0.1:50051", Status: state.NodeStatusReady},
	})
	store.SetNodeConfig("node-1", state.NodeConfigState{RawJSON: `{}`, Config: config})
	config.DefaultAction = "allow"
	store.SetNodeConfig("node-2", state.NodeConfigState{RawJSON: `{}`, Config: config})
	return store
}

func validNodeConfig() state.NodeDaemonConfig {
	return state.NodeDaemonConfig{
		DefaultAction:     "deny",
		DefaultDuration:   "once",
		ProcMonitorMethod: "ebpf",
		LogLevel:          state.NodeLogLevelImportant,
		LogUTC:            true,
		Rules: state.NodeRulesConfig{
			Path: "/etc/opensnitchd/rules",
		},
		Internal: state.NodeInternalConfig{
			GCPercent: 100,
		},
		FwOptions: state.NodeFirewallOptions{
			MonitorInterval: "15s",
			QueueBypass:     true,
		},
		Stats: state.NodeStatsConfig{
			MaxEvents: 250,
			MaxStats:  25,
		},
	}
}

func newTestModel(store *state.Store, manager controllerNodeConfigManager) *Model {
	model := New(store, theme.New(theme.Options{}), manager).(*Model)
	model.SetSize(100, 38)
	return model
}

type controllerNodeConfigManager interface {
	ApplyNodeConfig(context.Context, string, state.NodeDaemonConfig) error
}

func key(value string) tea.KeyMsg {
	switch value {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
	}
}
