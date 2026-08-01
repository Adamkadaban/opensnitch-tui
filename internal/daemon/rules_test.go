package daemon

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"google.golang.org/protobuf/proto"
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

func TestApplyRulesSequentialAcknowledgementsPreserveOrderPayloadAndStore(t *testing.T) {
	srv, stream, stop := startNotificationTestStream(t, "node-1")
	defer stop()
	original := []state.Rule{
		{NodeID: stream.nodeID, Name: "replace", Description: "old", Action: "deny", Duration: "once", Operator: state.RuleOperator{Type: "simple"}},
		{NodeID: stream.nodeID, Name: "untouched", Action: "allow", Duration: "always", Operator: state.RuleOperator{Type: "simple"}},
	}
	srv.store.SetRules(stream.nodeID, original)
	created := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)
	updated := created.Add(5 * time.Minute)
	batch := []state.Rule{
		{
			Name: "replace", Description: "new", Action: "allow", Duration: "always", Enabled: true,
			Precedence: true, NoLog: true, CreatedAt: created, UpdatedAt: updated,
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

	first := <-stream.sent
	assertSingleRuleNotification(t, first, batch[0])
	if got := srv.store.Snapshot().Rules[stream.nodeID]; !reflect.DeepEqual(got, original) {
		t.Fatalf("store changed before first acknowledgement\nwant: %+v\ngot:  %+v", original, got)
	}
	stream.replies <- &pb.NotificationReply{Id: first.GetId(), Code: pb.NotificationReplyCode_OK}

	second := <-stream.sent
	assertSingleRuleNotification(t, second, batch[1])
	if second.GetId() <= first.GetId() {
		t.Fatalf("notification IDs are not increasing: first=%d second=%d", first.GetId(), second.GetId())
	}
	firstApplied := batch[0]
	firstApplied.NodeID = stream.nodeID
	wantAfterFirst := []state.Rule{firstApplied, original[1]}
	if got := srv.store.Snapshot().Rules[stream.nodeID]; !reflect.DeepEqual(got, wantAfterFirst) {
		t.Fatalf("unexpected store after first acknowledgement\nwant: %+v\ngot:  %+v", wantAfterFirst, got)
	}
	stream.replies <- &pb.NotificationReply{Id: second.GetId(), Code: pb.NotificationReplyCode_OK}

	if err := <-result; err != nil {
		t.Fatalf("ApplyRules error: %v", err)
	}
	secondApplied := batch[1]
	secondApplied.NodeID = stream.nodeID
	want := []state.Rule{firstApplied, original[1], secondApplied}
	if got := srv.store.Snapshot().Rules[stream.nodeID]; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected final store\nwant: %+v\ngot:  %+v", want, got)
	}
}

func TestApplyRulesFirstOKSecondErrorPreservesAcknowledgedState(t *testing.T) {
	srv, stream, stop := startNotificationTestStream(t, "node-1")
	defer stop()
	original := []state.Rule{
		{NodeID: stream.nodeID, Name: "first", Description: "old first", Action: "deny", Duration: "always", Operator: state.RuleOperator{Type: "simple"}},
		{NodeID: stream.nodeID, Name: "second", Description: "old second", Action: "deny", Duration: "always", Operator: state.RuleOperator{Type: "simple"}},
		{NodeID: stream.nodeID, Name: "untouched", Action: "allow", Duration: "always", Operator: state.RuleOperator{Type: "simple"}},
	}
	srv.store.SetRules(stream.nodeID, original)
	batch := []state.Rule{
		{Name: "first", Description: "new first", Action: "allow", Duration: "always", Enabled: true, Operator: state.RuleOperator{Type: "simple", Operand: "process.path", Data: "/bin/first"}},
		{Name: "second", Description: "new second", Action: "allow", Duration: "always", Enabled: true, Operator: state.RuleOperator{Type: "simple", Operand: "process.path", Data: "/bin/second"}},
		{Name: "later", Action: "deny", Duration: "once", Operator: state.RuleOperator{Type: "simple", Operand: "dest.host", Data: "later.example"}},
	}

	result := make(chan error, 1)
	go func() {
		result <- srv.ApplyRules(context.Background(), stream.nodeID, batch)
	}()

	first := <-stream.sent
	assertSingleRuleNotification(t, first, batch[0])
	stream.replies <- &pb.NotificationReply{Id: first.GetId(), Code: pb.NotificationReplyCode_OK}

	second := <-stream.sent
	assertSingleRuleNotification(t, second, batch[1])
	firstApplied := batch[0]
	firstApplied.NodeID = stream.nodeID
	want := []state.Rule{firstApplied, original[1], original[2]}
	if got := srv.store.Snapshot().Rules[stream.nodeID]; !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected store before second reply\nwant: %+v\ngot:  %+v", want, got)
	}
	stream.replies <- &pb.NotificationReply{
		Id: second.GetId(), Code: pb.NotificationReplyCode_ERROR, Data: "rule rejected",
	}
	err := <-result
	var applyErr *RuleApplyError
	if !errors.As(err, &applyErr) {
		t.Fatalf("expected RuleApplyError, got %v", err)
	}
	if applyErr.Index != 2 || applyErr.Name != "second" || applyErr.Applied != 1 || applyErr.Total != 3 {
		t.Fatalf("unexpected apply error details: %+v", applyErr)
	}
	if got := err.Error(); got != `rule 2 "second" failed after 1 of 3 rules applied: notification 10002 failed for test://node-1: rule rejected` {
		t.Fatalf("unexpected apply error text: %q", got)
	}
	var replyErr *NotificationReplyError
	if !errors.As(err, &replyErr) {
		t.Fatalf("expected NotificationReplyError, got %v", err)
	}
	if got := srv.store.Snapshot().Rules[stream.nodeID]; !reflect.DeepEqual(got, want) {
		t.Fatalf("store diverged after daemon error\nwant: %+v\ngot:  %+v", want, got)
	}
	assertNoNotification(t, stream.sent)
}

func TestApplyRulesCancellationAfterFirstStopsFurtherSends(t *testing.T) {
	srv, stream, stop := startNotificationTestStream(t, "node-1")
	defer stop()
	batch := []state.Rule{
		{Name: "first", Action: "allow", Duration: "always", Operator: state.RuleOperator{Type: "simple"}},
		{Name: "second", Action: "deny", Duration: "always", Operator: state.RuleOperator{Type: "simple"}},
	}
	ctx := newCancelBetweenRulesContext()
	result := make(chan error, 1)
	go func() {
		result <- srv.ApplyRules(ctx, stream.nodeID, batch)
	}()

	first := <-stream.sent
	assertSingleRuleNotification(t, first, batch[0])
	stream.replies <- &pb.NotificationReply{Id: first.GetId(), Code: pb.NotificationReplyCode_OK}

	err := <-result
	var applyErr *RuleApplyError
	if !errors.As(err, &applyErr) {
		t.Fatalf("expected RuleApplyError, got %v", err)
	}
	if !errors.Is(err, context.Canceled) || applyErr.Index != 2 || applyErr.Applied != 1 {
		t.Fatalf("unexpected cancellation error: %+v", applyErr)
	}
	firstApplied := batch[0]
	firstApplied.NodeID = stream.nodeID
	if got := srv.store.Snapshot().Rules[stream.nodeID]; !reflect.DeepEqual(got, []state.Rule{firstApplied}) {
		t.Fatalf("unexpected store after cancellation: %+v", got)
	}
	assertNoNotification(t, stream.sent)
}

func TestApplyRulesValidatesCompleteBatchBeforeSending(t *testing.T) {
	srv, stream, stop := startNotificationTestStream(t, "node-1")
	defer stop()
	valid := state.Rule{
		Name: "valid", Action: "allow", Duration: "always",
		Operator: state.RuleOperator{Type: "simple"},
	}
	tests := []struct {
		name   string
		nodeID string
		rules  []state.Rule
		want   string
	}{
		{name: "node ID", rules: []state.Rule{valid}, want: "node id required"},
		{name: "empty name", nodeID: stream.nodeID, rules: []state.Rule{valid, {Action: "deny", Duration: "always", Operator: state.RuleOperator{Type: "simple"}}}, want: "rule 2 name required"},
		{name: "duplicate", nodeID: stream.nodeID, rules: []state.Rule{valid, valid}, want: `duplicate rule name "valid"`},
		{name: "operator", nodeID: stream.nodeID, rules: []state.Rule{valid, {Name: "bad", Action: "deny", Duration: "always"}}, want: `rule 2 "bad" operator: type is empty`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := srv.ApplyRules(context.Background(), test.nodeID, test.rules)
			if err == nil || err.Error() != test.want {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
			assertNoNotification(t, stream.sent)
			if got := srv.store.Snapshot().Rules[stream.nodeID]; len(got) != 0 {
				t.Fatalf("validation changed store: %+v", got)
			}
		})
	}
}

func assertSingleRuleNotification(t *testing.T, notification *pb.Notification, rule state.Rule) {
	t.Helper()
	if notification.GetType() != pb.Action_CHANGE_RULE || len(notification.GetRules()) != 1 {
		t.Fatalf("unexpected notification: %+v", notification)
	}
	if want := serializeRule(rule); !proto.Equal(notification.GetRules()[0], want) {
		t.Fatalf("unexpected daemon payload\nwant: %v\ngot:  %v", want, notification.GetRules()[0])
	}
}

func assertNoNotification(t *testing.T, sent <-chan *pb.Notification) {
	t.Helper()
	select {
	case notification := <-sent:
		t.Fatalf("unexpected notification: %+v", notification)
	default:
	}
}

type cancelBetweenRulesContext struct {
	context.Context
	checks atomic.Int32
	done   chan struct{}
	once   sync.Once
}

func newCancelBetweenRulesContext() *cancelBetweenRulesContext {
	return &cancelBetweenRulesContext{
		Context: context.Background(),
		done:    make(chan struct{}),
	}
}

func (c *cancelBetweenRulesContext) Done() <-chan struct{} {
	return c.done
}

func (c *cancelBetweenRulesContext) Err() error {
	if c.checks.Add(1) < 3 {
		return nil
	}
	c.once.Do(func() { close(c.done) })
	return context.Canceled
}
