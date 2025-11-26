package daemon

import (
	"context"
	"testing"

	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"google.golang.org/grpc/peer"
)

func TestParseListenAddr(t *testing.T) {
	tests := []struct {
		input   string
		network string
		address string
		wantErr bool
	}{
		{"127.0.0.1:50051", "tcp", "127.0.0.1:50051", false},
		{"unix:///tmp/osui.sock", "unix", "/tmp/osui.sock", false},
		{"unix://", "", "", true},
		{"", "", "", true},
	}

	for _, tt := range tests {
		target, err := parseListenAddr(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("expected error for input %q", tt.input)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tt.input, err)
		}
		if target.network != tt.network || target.address != tt.address {
			t.Fatalf("unexpected target for %q: %+v", tt.input, target)
		}
	}
}

func TestServerPostAlertStoresAlert(t *testing.T) {
	store := state.NewStore()
	srv := New(store, Options{})
	ctx := peer.NewContext(context.Background(), &peer.Peer{Addr: &testAddr{network: "tcp", value: "1.2.3.4:1000"}})

	alert := &pb.Alert{Id: 7, Priority: pb.Alert_MEDIUM, Type: pb.Alert_WARNING, Action: pb.Alert_SHOW_ALERT, Data: &pb.Alert_Text{Text: "disk"}}
	resp, err := srv.PostAlert(ctx, alert)
	if err != nil {
		t.Fatalf("PostAlert returned error: %v", err)
	}
	if resp.GetId() != 7 {
		t.Fatalf("expected response id 7, got %d", resp.GetId())
	}

	snap := store.Snapshot()
	if len(snap.Alerts) != 1 {
		t.Fatalf("expected 1 alert stored, got %d", len(snap.Alerts))
	}
	stored := snap.Alerts[0]
	if stored.Text != "disk" {
		t.Fatalf("expected alert text disk, got %q", stored.Text)
	}
	if stored.NodeID == "" {
		t.Fatalf("expected node id to be populated")
	}
}

func TestServerSubscribeStoresRules(t *testing.T) {
	store := state.NewStore()
	srv := New(store, Options{})
	ctx := peer.NewContext(context.Background(), &peer.Peer{Addr: &testAddr{network: "tcp", value: "1.2.3.4:5000"}})
	cfg := &pb.ClientConfig{
		Name:              "daemon",
		Version:           "1",
		IsFirewallRunning: true,
		Rules: []*pb.Rule{{
			Name: "ssh",
			Operator: &pb.Operator{
				Type:    "process",
				Operand: "eq",
				Data:    "/usr/bin/ssh",
			},
		}},
	}
	if _, err := srv.Subscribe(ctx, cfg); err != nil {
		t.Fatalf("Subscribe error: %v", err)
	}
	snap := store.Snapshot()
	if len(snap.Rules["tcp://1.2.3.4:5000"]) != 1 {
		t.Fatalf("expected rules stored for node, got %+v", snap.Rules)
	}
}

func TestServerEnableRuleSendsNotification(t *testing.T) {
	store := state.NewStore()
	srv := New(store, Options{})
	sess := &session{nodeID: "node-1", send: make(chan *pb.Notification, 1)}
	srv.sessions["node-1"] = sess
	store.SetRules("node-1", []state.Rule{{
		Name:     "ssh",
		Operator: state.RuleOperator{Type: "process", Operand: "eq", Data: "/usr/bin/ssh"},
	}})
	if err := srv.EnableRule("node-1", "ssh"); err != nil {
		t.Fatalf("EnableRule error: %v", err)
	}
	notif := <-sess.send
	if notif.Type != pb.Action_ENABLE_RULE {
		t.Fatalf("expected enable rule action, got %v", notif.Type)
	}
	if len(notif.Rules) != 1 || notif.Rules[0].GetName() != "ssh" {
		t.Fatalf("unexpected rule payload: %+v", notif.Rules)
	}
	if !store.Snapshot().Rules["node-1"][0].Enabled {
		t.Fatalf("expected rule to be marked enabled in store")
	}
}

func TestServerDeleteRuleRemovesState(t *testing.T) {
	store := state.NewStore()
	srv := New(store, Options{})
	sess := &session{nodeID: "node-1", send: make(chan *pb.Notification, 1)}
	srv.sessions["node-1"] = sess
	store.SetRules("node-1", []state.Rule{{
		Name:     "ssh",
		Operator: state.RuleOperator{Type: "process"},
	}})
	if err := srv.DeleteRule("node-1", "ssh"); err != nil {
		t.Fatalf("DeleteRule error: %v", err)
	}
	if _, ok := store.Snapshot().Rules["node-1"]; ok {
		t.Fatalf("expected rules to be removed from store")
	}
}

func TestServerResolvePromptAddsRule(t *testing.T) {
	store := state.NewStore()
	store.SetStats(state.Stats{NodeID: "node-1"})
	srv := New(store, Options{})
	req := &promptRequest{
		id: "prompt-1",
		prompt: state.Prompt{
			ID:     "prompt-1",
			NodeID: "node-1",
			Connection: state.Connection{
				ProcessPath: "/usr/bin/curl",
			},
		},
		response: make(chan promptResponse, 1),
	}
	srv.registerPrompt(req)
	decision := controller.PromptDecision{
		PromptID: "prompt-1",
		Action:   controller.PromptActionAllow,
		Duration: controller.PromptDurationAlways,
		Target:   controller.PromptTargetProcessPath,
	}
	if err := srv.ResolvePrompt(decision); err != nil {
		t.Fatalf("ResolvePrompt error: %v", err)
	}
	rules := store.Snapshot().Rules["node-1"]
	if len(rules) != 1 {
		t.Fatalf("expected rule to be added to store, got %d", len(rules))
	}
	if store.Snapshot().Stats.Rules != 1 {
		t.Fatalf("expected stats count to update, got %d", store.Snapshot().Stats.Rules)
	}
}

type testAddr struct {
	network string
	value   string
}

func (a *testAddr) Network() string { return a.network }
func (a *testAddr) String() string  { return a.value }
