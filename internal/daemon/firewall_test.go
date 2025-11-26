package daemon

import (
	"testing"

	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
)

func TestConvertFirewallNil(t *testing.T) {
	fw := convertFirewall(nil)
	if fw.Enabled || fw.Version != 0 || len(fw.Chains) != 0 {
		t.Fatalf("expected zero firewall state, got %+v", fw)
	}
}

func TestConvertFirewallChains(t *testing.T) {
	fw := convertFirewall(&pb.SysFirewall{
		Enabled: true,
		Version: 3,
		SystemRules: []*pb.FwChains{
			{
				Chains: []*pb.FwChain{
					{
						Table:  "filter",
						Name:   "output",
						Family: "ip",
						Policy: "accept",
						Hook:   "output",
						Rules: []*pb.FwRule{{
							UUID:        "123",
							Description: "allow",
							Enabled:     true,
							Target:      "accept",
						}},
					},
				},
			},
		},
	})

	if !fw.Enabled || fw.Version != 3 {
		t.Fatalf("expected firewall metadata to copy, got %+v", fw)
	}
	if len(fw.Chains) != 1 {
		t.Fatalf("expected one chain, got %d", len(fw.Chains))
	}
	chain := fw.Chains[0]
	if chain.Name != "output" || len(chain.Rules) != 1 {
		t.Fatalf("unexpected chain conversion: %+v", chain)
	}
	if chain.Rules[0].UUID != "123" || chain.Rules[0].Description != "allow" {
		t.Fatalf("unexpected rule conversion: %+v", chain.Rules[0])
	}
}
