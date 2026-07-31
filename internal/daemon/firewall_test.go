package daemon

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/proto"

	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
)

func TestConvertSystemFirewall(t *testing.T) {
	protoFirewall := &pb.SysFirewall{
		Enabled: true,
		Version: 2,
		SystemRules: []*pb.FwChains{{
			Rule: &pb.FwRule{
				Table:       "filter",
				Chain:       "OUTPUT",
				UUID:        "legacy-rule",
				Enabled:     true,
				Position:    1,
				Description: "legacy",
				Parameters:  "-p tcp",
				Target:      "ACCEPT",
			},
			Chains: []*pb.FwChain{{
				Name:     "output",
				Table:    "opensnitch",
				Family:   "inet",
				Priority: "0",
				Type:     "filter",
				Hook:     "output",
				Policy:   "accept",
				Rules: []*pb.FwRule{{
					Table:            "opensnitch",
					Chain:            "output",
					UUID:             "rule-1",
					Enabled:          true,
					Position:         4,
					Description:      "allow dns",
					Parameters:       "udp dport 53",
					Target:           "accept",
					TargetParameters: "counter",
					Expressions: []*pb.Expressions{{
						Statement: &pb.Statement{
							Op:   "match",
							Name: "udp",
							Values: []*pb.StatementValues{{
								Key:   "dport",
								Value: "53",
							}},
						},
					}},
				}},
			}},
		}},
	}

	got, ok := convertSystemFirewall(protoFirewall, "node-1", true)
	if !ok {
		t.Fatal("expected firewall state to be present")
	}
	want := state.SystemFirewall{
		NodeID:  "node-1",
		Enabled: true,
		Running: true,
		Version: 2,
		SystemRules: []state.FirewallRuleGroup{{
			Rule: &state.FirewallRule{
				Table:       "filter",
				Chain:       "OUTPUT",
				UUID:        "legacy-rule",
				Enabled:     true,
				Position:    1,
				Description: "legacy",
				Parameters:  "-p tcp",
				Target:      "ACCEPT",
			},
			Chains: []state.FirewallChain{{
				Name:     "output",
				Table:    "opensnitch",
				Family:   "inet",
				Priority: "0",
				Type:     "filter",
				Hook:     "output",
				Policy:   "accept",
				Rules: []state.FirewallRule{{
					Table:            "opensnitch",
					Chain:            "output",
					UUID:             "rule-1",
					Enabled:          true,
					Position:         4,
					Description:      "allow dns",
					Parameters:       "udp dport 53",
					Target:           "accept",
					TargetParameters: "counter",
					Expressions: []state.FirewallExpression{{
						Statement: &state.FirewallStatement{
							Op:   "match",
							Name: "udp",
							Values: []state.FirewallStatementValue{{
								Key:   "dport",
								Value: "53",
							}},
						},
					}},
				}},
			}},
		}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("converted firewall mismatch (-want +got):\n%s", diff)
	}
}

func TestConvertSystemFirewallNil(t *testing.T) {
	got, ok := convertSystemFirewall(nil, "node-1", true)
	if ok {
		t.Fatal("expected nil firewall state to be absent")
	}
	if diff := cmp.Diff(state.SystemFirewall{}, got); diff != "" {
		t.Fatalf("expected zero firewall state (-want +got):\n%s", diff)
	}
}

func TestSerializeSystemFirewallRoundTrip(t *testing.T) {
	original := state.SystemFirewall{
		NodeID:  "node-1",
		Enabled: true,
		Running: true,
		Version: 7,
		SystemRules: []state.FirewallRuleGroup{{
			Rule: &state.FirewallRule{
				Table:            "filter",
				Chain:            "OUTPUT",
				UUID:             "legacy",
				Enabled:          true,
				Position:         2,
				Description:      "legacy rule",
				Parameters:       "-p udp",
				Target:           "ACCEPT",
				TargetParameters: "counter",
			},
			Chains: []state.FirewallChain{{
				Name:     "output",
				Table:    "opensnitch",
				Family:   "inet",
				Priority: "-5",
				Type:     "filter",
				Hook:     "output",
				Policy:   "drop",
				Rules: []state.FirewallRule{{
					Table:            "opensnitch",
					Chain:            "output",
					UUID:             "rule-1",
					Enabled:          true,
					Position:         4,
					Description:      "allow dns",
					Parameters:       "udp dport 53",
					Target:           "accept",
					TargetParameters: "counter",
					Expressions: []state.FirewallExpression{
						{},
						{Statement: &state.FirewallStatement{
							Op:   "match",
							Name: "udp",
							Values: []state.FirewallStatementValue{{
								Key:   "dport",
								Value: "53",
							}},
						}},
					},
				}},
			}},
		}},
	}

	serialized := serializeSystemFirewall(original)
	roundTrip, ok := convertSystemFirewall(serialized, original.NodeID, original.Running)
	if !ok {
		t.Fatal("expected serialized firewall to convert")
	}
	if diff := cmp.Diff(original, roundTrip); diff != "" {
		t.Fatalf("round-trip firewall mismatch (-want +got):\n%s", diff)
	}

	reserialized := serializeSystemFirewall(roundTrip)
	if !proto.Equal(serialized, reserialized) {
		t.Fatalf("serialized firewall changed after round trip:\nfirst: %s\nsecond: %s", serialized, reserialized)
	}
}
