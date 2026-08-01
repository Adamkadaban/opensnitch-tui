package daemon

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
)

func TestConvertRules(t *testing.T) {
	protoRules := []*pb.Rule{{
		Created:     100,
		Name:        "ssh",
		Description: "allow ssh",
		Enabled:     true,
		Precedence:  true,
		Nolog:       true,
		Action:      "allow",
		Duration:    "always",
		Operator: &pb.Operator{
			Type:    "process",
			Operand: "eq",
			Data:    "/usr/bin/ssh",
			List: []*pb.Operator{{
				Type:    "list",
				Operand: "contains",
				Data:    "10.0.0.1",
			}},
		},
	}}

	rules := convertRules(protoRules, "node-1")
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	rule := rules[0]
	if rule.Name != "ssh" {
		t.Fatalf("unexpected rule name %q", rule.Name)
	}
	if rule.CreatedAt.IsZero() {
		t.Fatalf("expected created at to be set")
	}
	if len(rule.Operator.Children) != 1 {
		t.Fatalf("expected nested operators to be copied")
	}
}

func TestSerializeRule(t *testing.T) {
	rule := convertRule(&pb.Rule{
		Created: 50,
		Name:    "web",
		Operator: &pb.Operator{
			Type:    "process",
			Operand: "eq",
			Data:    "/usr/bin/web",
			List: []*pb.Operator{{
				Type:    "list",
				Operand: "contains",
				Data:    "example.com",
			}},
		},
	}, "node-1")
	proto := serializeRule(rule)
	if proto.GetName() != "web" {
		t.Fatalf("expected serialized rule name web, got %q", proto.GetName())
	}
	if proto.GetCreated() == 0 {
		t.Fatalf("expected created timestamp to be preserved")
	}
	if proto.GetOperator() == nil || len(proto.GetOperator().GetList()) != 1 {
		t.Fatalf("expected operator children to be serialized")
	}
	roundTrip := convertRule(proto, "node-1")
	if roundTrip.Operator.Children[0].Data != "example.com" {
		t.Fatalf("expected operator payload to survive round trip")
	}
}

func TestApplyRulesSendsBatchAndUpdatesAfterAcknowledgement(t *testing.T) {
	srv, stream, stop := startNotificationTestStream(t, "node-1")
	defer stop()
	srv.store.SetRules(stream.nodeID, []state.Rule{
		{Name: "replace", Description: "old", Action: "deny", Duration: "once", Operator: state.RuleOperator{Type: "simple"}},
		{Name: "untouched", Action: "allow", Duration: "always", Operator: state.RuleOperator{Type: "simple"}},
	})
	created := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)
	batch := []state.Rule{
		{
			Name: "replace", Description: "new", Action: "allow", Duration: "always", Enabled: true,
			CreatedAt: created,
			Operator: state.RuleOperator{
				Type: "list",
				Children: []state.RuleOperator{{
					Type: "simple", Operand: "dest.host", Data: "example.com", Sensitive: true,
				}},
			},
		},
		{Name: "added", Action: "deny", Duration: "always", Operator: state.RuleOperator{Type: "simple", Operand: "process.path", Data: "/bin/curl"}},
	}

	result := make(chan error, 1)
	go func() {
		result <- srv.ApplyRules(context.Background(), stream.nodeID, batch)
	}()
	sent := <-stream.sent
	if sent.GetType() != pb.Action_CHANGE_RULE || len(sent.GetRules()) != 2 {
		t.Fatalf("unexpected batch notification: %+v", sent)
	}
	if sent.GetRules()[0].GetName() != "replace" || sent.GetRules()[1].GetName() != "added" {
		t.Fatalf("unexpected rule order: %+v", sent.GetRules())
	}
	if got := sent.GetRules()[0].GetOperator().GetList(); len(got) != 1 || got[0].GetData() != "example.com" || !got[0].GetSensitive() {
		t.Fatalf("nested operator payload was not preserved: %+v", got)
	}
	beforeAck := srv.store.Snapshot().Rules[stream.nodeID]
	if beforeAck[0].Description != "old" || len(beforeAck) != 2 {
		t.Fatalf("store changed before acknowledgement: %+v", beforeAck)
	}

	stream.replies <- &pb.NotificationReply{Id: sent.GetId(), Code: pb.NotificationReplyCode_OK}
	if err := <-result; err != nil {
		t.Fatalf("ApplyRules error: %v", err)
	}
	got := srv.store.Snapshot().Rules[stream.nodeID]
	if len(got) != 3 || got[0].Name != "replace" || got[0].Description != "new" ||
		got[1].Name != "untouched" || got[2].Name != "added" {
		t.Fatalf("unexpected acknowledged merge: %+v", got)
	}
	if !got[0].CreatedAt.Equal(created) || !reflect.DeepEqual(got[0].Operator, batch[0].Operator) {
		t.Fatalf("replacement lost rule fields: %+v", got[0])
	}
}

func TestApplyRulesDaemonErrorLeavesStoreUnchanged(t *testing.T) {
	srv, stream, stop := startNotificationTestStream(t, "node-1")
	defer stop()
	original := []state.Rule{{
		Name: "same", Description: "original", Action: "deny", Duration: "always",
		Operator: state.RuleOperator{Type: "simple"},
	}}
	srv.store.SetRules(stream.nodeID, original)

	result := make(chan error, 1)
	go func() {
		result <- srv.ApplyRules(context.Background(), stream.nodeID, []state.Rule{{
			Name: "same", Description: "replacement", Action: "allow", Duration: "always",
			Operator: state.RuleOperator{Type: "simple"},
		}})
	}()
	sent := <-stream.sent
	stream.replies <- &pb.NotificationReply{
		Id: sent.GetId(), Code: pb.NotificationReplyCode_ERROR, Data: "rule rejected",
	}
	err := <-result
	var replyErr *NotificationReplyError
	if !errors.As(err, &replyErr) {
		t.Fatalf("expected NotificationReplyError, got %v", err)
	}
	if got := srv.store.Snapshot().Rules[stream.nodeID]; !reflect.DeepEqual(got, original) {
		t.Fatalf("store changed after daemon error\nwant: %+v\ngot:  %+v", original, got)
	}
}

func TestApplyRulesRejectsDuplicateNames(t *testing.T) {
	srv := New(state.NewStore(), Options{})
	err := srv.ApplyRules(context.Background(), "node-1", []state.Rule{
		{Name: "same"},
		{Name: "same"},
	})
	if err == nil || err.Error() != `duplicate rule name "same"` {
		t.Fatalf("expected duplicate name error, got %v", err)
	}
}
