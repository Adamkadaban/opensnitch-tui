package daemon

import (
	"reflect"
	"testing"
	"time"

	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
)

func TestConvertAlertVariants(t *testing.T) {
	tests := []struct {
		name    string
		payload any
		assert  func(*testing.T, state.Alert)
	}{
		{
			name:    "text",
			payload: &pb.Alert_Text{Text: "disk full"},
			assert: func(t *testing.T, alert state.Alert) {
				if alert.PayloadKind != state.AlertPayloadText || alert.Text != "disk full" {
					t.Fatalf("unexpected text alert: %#v", alert)
				}
			},
		},
		{
			name: "process",
			payload: &pb.Alert_Proc{Proc: &pb.Process{
				Pid: 4, Ppid: 3, Uid: 2, Comm: "curl", Path: "/usr/bin/curl",
				Args: []string{"curl", "example.com"}, Env: map[string]string{"TOKEN": "secret"},
				Checksums:   map[string]string{"sha256": "abc"},
				ProcessTree: []*pb.StringInt{{Key: "/sbin/init", Value: 1}, nil},
			}},
			assert: func(t *testing.T, alert state.Alert) {
				if alert.PayloadKind != state.AlertPayloadProcess || alert.Process == nil {
					t.Fatalf("unexpected process alert: %#v", alert)
				}
				if alert.Process.PID != 4 || alert.Process.Env["TOKEN"] != "secret" ||
					len(alert.Process.ProcessTree) != 2 || alert.Process.ProcessTree[0].PID != 1 {
					t.Fatalf("unexpected process payload: %#v", alert.Process)
				}
			},
		},
		{
			name: "connection",
			payload: &pb.Alert_Conn{Conn: &pb.Connection{
				Protocol: "tcp", DstIp: "1.1.1.1", DstHost: "example.com", DstPort: 443,
				ProcessArgs: []string{"curl"}, ProcessEnv: map[string]string{"PASSWORD": "secret"},
				ProcessChecksums: map[string]string{"sha256": "abc"},
				ProcessTree:      []*pb.StringInt{{Key: "/usr/bin/curl", Value: 9}},
			}},
			assert: func(t *testing.T, alert state.Alert) {
				if alert.PayloadKind != state.AlertPayloadConnection || alert.Connection == nil {
					t.Fatalf("unexpected connection alert: %#v", alert)
				}
				if alert.Connection.DstPort != 443 || alert.Connection.ProcessEnv["PASSWORD"] != "secret" ||
					alert.Connection.ProcessTree[0].PID != 9 {
					t.Fatalf("unexpected connection payload: %#v", alert.Connection)
				}
			},
		},
		{
			name: "rule",
			payload: &pb.Alert_Rule{Rule: &pb.Rule{
				Name: "allow-web", Action: "allow",
				Operator: &pb.Operator{Type: "list", List: []*pb.Operator{{
					Type: "simple", Operand: "dest.host", Data: "example.com",
				}}},
			}},
			assert: func(t *testing.T, alert state.Alert) {
				if alert.PayloadKind != state.AlertPayloadRule || alert.Rule == nil ||
					alert.Rule.NodeID != "node-1" || alert.Rule.Operator.Children[0].Data != "example.com" {
					t.Fatalf("unexpected rule alert: %#v", alert)
				}
			},
		},
		{
			name: "firewall",
			payload: &pb.Alert_Fwrule{Fwrule: &pb.FwRule{
				Table: "filter", Chain: "output", Target: "accept",
				Expressions: []*pb.Expressions{{Statement: &pb.Statement{
					Op: "match", Name: "tcp", Values: []*pb.StatementValues{{Key: "dport", Value: "443"}, nil},
				}}},
			}},
			assert: func(t *testing.T, alert state.Alert) {
				if alert.PayloadKind != state.AlertPayloadFirewall || alert.FirewallRule == nil ||
					alert.FirewallRule.Expressions[0].Statement.Values[0].Value != "443" {
					t.Fatalf("unexpected firewall alert: %#v", alert)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now()
			alert := &pb.Alert{
				Id: 42, Priority: pb.Alert_HIGH, Type: pb.Alert_WARNING,
				Action: pb.Alert_SHOW_ALERT, What: pb.Alert_KERNEL_EVENT,
			}
			switch payload := test.payload.(type) {
			case *pb.Alert_Text:
				alert.Data = payload
			case *pb.Alert_Proc:
				alert.Data = payload
			case *pb.Alert_Conn:
				alert.Data = payload
			case *pb.Alert_Rule:
				alert.Data = payload
			case *pb.Alert_Fwrule:
				alert.Data = payload
			}
			converted := convertAlert(alert, "node-1")
			if converted.ID != "42" || converted.NodeID != "node-1" ||
				converted.Priority != state.AlertPriorityHigh || converted.Type != state.AlertTypeWarning ||
				converted.Action != state.AlertActionShowAlert || converted.What != state.AlertWhatKernelEvent {
				t.Fatalf("unexpected alert metadata: %#v", converted)
			}
			if converted.CreatedAt.Before(now) {
				t.Fatal("expected CreatedAt to be set")
			}
			if payloadCount(converted) != 1 {
				t.Fatalf("expected exactly one payload, got %#v", converted)
			}
			test.assert(t, converted)
		})
	}
}

func TestConvertAlertNilAndUnknownValues(t *testing.T) {
	if alert := convertAlert(nil, "node-1"); !reflect.DeepEqual(alert, state.Alert{}) {
		t.Fatalf("expected zero alert, got %#v", alert)
	}

	tests := []*pb.Alert{
		{},
		{Data: &pb.Alert_Proc{}},
		{Data: &pb.Alert_Conn{}},
		{Data: &pb.Alert_Rule{}},
		{Data: &pb.Alert_Fwrule{}},
	}
	for _, input := range tests {
		alert := convertAlert(input, "node-1")
		if alert.PayloadKind != state.AlertPayloadNone || payloadCount(alert) != 0 {
			t.Fatalf("expected no payload for nil value, got %#v", alert)
		}
	}

	alert := convertAlert(&pb.Alert{
		Priority: pb.Alert_Priority(99),
		Type:     pb.Alert_Type(98),
		Action:   pb.Alert_Action(97),
		What:     pb.Alert_What(96),
	}, "node-1")
	if alert.Priority != "UNKNOWN(99)" || alert.Type != "UNKNOWN(98)" ||
		alert.Action != "UNKNOWN(97)" || alert.What != "UNKNOWN(96)" {
		t.Fatalf("unexpected unknown enum values: %#v", alert)
	}
}

func TestConvertAlertDeepCopiesPayloads(t *testing.T) {
	process := &pb.Process{
		Args: []string{"one"}, Env: map[string]string{"A": "one"},
		Checksums:   map[string]string{"sha256": "one"},
		ProcessTree: []*pb.StringInt{{Key: "one", Value: 1}},
	}
	converted := convertAlert(&pb.Alert{Data: &pb.Alert_Proc{Proc: process}}, "node-1")
	process.Args[0] = "two"
	process.Env["A"] = "two"
	process.Checksums["sha256"] = "two"
	process.ProcessTree[0].Key = "two"
	if converted.Process.Args[0] != "one" || converted.Process.Env["A"] != "one" ||
		converted.Process.Checksums["sha256"] != "one" || converted.Process.ProcessTree[0].Path != "one" {
		t.Fatalf("converted process aliases proto input: %#v", converted.Process)
	}

	connection := &pb.Connection{
		ProcessArgs: []string{"one"}, ProcessEnv: map[string]string{"A": "one"},
		ProcessChecksums: map[string]string{"sha256": "one"},
		ProcessTree:      []*pb.StringInt{{Key: "one", Value: 1}},
	}
	converted = convertAlert(&pb.Alert{Data: &pb.Alert_Conn{Conn: connection}}, "node-1")
	connection.ProcessArgs[0] = "two"
	connection.ProcessEnv["A"] = "two"
	connection.ProcessChecksums["sha256"] = "two"
	connection.ProcessTree[0].Key = "two"
	if converted.Connection.ProcessArgs[0] != "one" || converted.Connection.ProcessEnv["A"] != "one" ||
		converted.Connection.ProcessChecksums["sha256"] != "one" || converted.Connection.ProcessTree[0].Path != "one" {
		t.Fatalf("converted connection aliases proto input: %#v", converted.Connection)
	}

	operator := &pb.Operator{Type: "list", List: []*pb.Operator{{Type: "simple", Data: "one"}}}
	converted = convertAlert(&pb.Alert{Data: &pb.Alert_Rule{Rule: &pb.Rule{Operator: operator}}}, "node-1")
	operator.List[0].Data = "two"
	if converted.Rule.Operator.Children[0].Data != "one" {
		t.Fatalf("converted rule aliases proto input: %#v", converted.Rule)
	}

	values := []*pb.StatementValues{{Key: "one", Value: "one"}}
	converted = convertAlert(&pb.Alert{Data: &pb.Alert_Fwrule{Fwrule: &pb.FwRule{
		Expressions: []*pb.Expressions{{Statement: &pb.Statement{Values: values}}},
	}}}, "node-1")
	values[0].Value = "two"
	if converted.FirewallRule.Expressions[0].Statement.Values[0].Value != "one" {
		t.Fatalf("converted firewall rule aliases proto input: %#v", converted.FirewallRule)
	}
}

func payloadCount(alert state.Alert) int {
	count := 0
	if alert.PayloadKind == state.AlertPayloadText {
		count++
	}
	if alert.Process != nil {
		count++
	}
	if alert.Connection != nil {
		count++
	}
	if alert.Rule != nil {
		count++
	}
	if alert.FirewallRule != nil {
		count++
	}
	return count
}
