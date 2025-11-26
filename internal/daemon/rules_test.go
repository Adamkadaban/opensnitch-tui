package daemon

import (
	"testing"

	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
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
