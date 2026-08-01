package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/proto"

	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
)

func TestFirewallControlsSendActionsAndUpdateAfterAcknowledgement(t *testing.T) {
	tests := []struct {
		name        string
		initial     state.SystemFirewall
		call        func(context.Context, *Server, string) error
		wantEnabled bool
		wantRunning bool
	}{
		{
			name:    "enable",
			initial: testFirewallControlState(false, false),
			call: func(ctx context.Context, server *Server, nodeID string) error {
				return server.EnableFirewall(ctx, nodeID)
			},
			wantEnabled: true,
			wantRunning: true,
		},
		{
			name:    "disable",
			initial: testFirewallControlState(true, true),
			call: func(ctx context.Context, server *Server, nodeID string) error {
				return server.DisableFirewall(ctx, nodeID)
			},
			wantEnabled: false,
			wantRunning: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, stream, stop := startNotificationTestStream(t, "node-1")
			defer stop()
			tt.initial.NodeID = stream.nodeID
			server.store.SetSystemFirewall(stream.nodeID, tt.initial)

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			result := make(chan error, 1)
			go func() {
				result <- tt.call(ctx, server, stream.nodeID)
			}()

			sent := <-stream.sent
			if sent.GetType() != pb.Action_RELOAD_FW_RULES {
				t.Fatalf("expected reload action, got %s", sent.GetType())
			}
			expected := tt.initial
			expected.Enabled = tt.wantEnabled
			expected.Running = tt.wantRunning
			if !proto.Equal(sent.GetSysFirewall(), serializeSystemFirewall(expected)) {
				t.Fatalf("unexpected firewall payload: %s", sent.GetSysFirewall())
			}
			beforeReply, _ := server.store.SystemFirewall(stream.nodeID)
			if diff := cmp.Diff(tt.initial, beforeReply); diff != "" {
				t.Fatalf("store changed before acknowledgement (-want +got):\n%s", diff)
			}

			stream.replies <- &pb.NotificationReply{Id: sent.GetId(), Code: pb.NotificationReplyCode_OK}
			if err := <-result; err != nil {
				t.Fatalf("%s firewall: %v", tt.name, err)
			}

			updated, _ := server.store.SystemFirewall(stream.nodeID)
			if updated.Enabled != tt.wantEnabled || updated.Running != tt.wantRunning {
				t.Fatalf("unexpected state after %s: enabled=%t running=%t", tt.name, updated.Enabled, updated.Running)
			}
			if diff := cmp.Diff(tt.initial.SystemRules, updated.SystemRules); diff != "" {
				t.Fatalf("firewall rules changed after %s (-want +got):\n%s", tt.name, diff)
			}
		})
	}
}

func TestReloadFirewallSendsNormalizedPayload(t *testing.T) {
	server, stream, stop := startNotificationTestStream(t, "node-1")
	defer stop()
	firewall := testFirewallControlState(true, true)
	firewall.NodeID = stream.nodeID
	server.store.SetSystemFirewall(stream.nodeID, firewall)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- server.ReloadFirewall(ctx, stream.nodeID)
	}()

	sent := <-stream.sent
	if sent.GetType() != pb.Action_RELOAD_FW_RULES {
		t.Fatalf("expected reload action, got %s", sent.GetType())
	}
	if !proto.Equal(sent.GetSysFirewall(), serializeSystemFirewall(firewall)) {
		t.Fatalf("unexpected reload payload: %s", sent.GetSysFirewall())
	}

	stream.replies <- &pb.NotificationReply{Id: sent.GetId(), Code: pb.NotificationReplyCode_OK}
	if err := <-result; err != nil {
		t.Fatalf("reload firewall: %v", err)
	}
	after, _ := server.store.SystemFirewall(stream.nodeID)
	if diff := cmp.Diff(firewall, after); diff != "" {
		t.Fatalf("reload changed store state (-want +got):\n%s", diff)
	}
}

func TestFirewallControlPropagatesDaemonErrorWithoutStoreUpdate(t *testing.T) {
	server, stream, stop := startNotificationTestStream(t, "node-1")
	defer stop()
	initial := testFirewallControlState(false, false)
	initial.NodeID = stream.nodeID
	server.store.SetSystemFirewall(stream.nodeID, initial)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- server.EnableFirewall(ctx, stream.nodeID)
	}()

	sent := <-stream.sent
	stream.replies <- &pb.NotificationReply{
		Id:   sent.GetId(),
		Code: pb.NotificationReplyCode_ERROR,
		Data: "permission denied",
	}
	err := <-result
	var replyErr *NotificationReplyError
	if !errors.As(err, &replyErr) {
		t.Fatalf("expected NotificationReplyError, got %v", err)
	}
	after, _ := server.store.SystemFirewall(stream.nodeID)
	if diff := cmp.Diff(initial, after); diff != "" {
		t.Fatalf("store changed after daemon error (-want +got):\n%s", diff)
	}
}

func testFirewallControlState(enabled, running bool) state.SystemFirewall {
	return state.SystemFirewall{
		NodeID:  "node-1",
		Enabled: enabled,
		Running: running,
		Version: 3,
		SystemRules: []state.FirewallRuleGroup{{
			Chains: []state.FirewallChain{{
				Name:   "output",
				Table:  "opensnitch",
				Family: "inet",
				Hook:   "output",
				Policy: "accept",
				Rules: []state.FirewallRule{{
					UUID:        "rule-1",
					Description: "allow dns",
					Target:      "accept",
				}},
			}},
		}},
	}
}
