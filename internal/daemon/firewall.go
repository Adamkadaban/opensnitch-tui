package daemon

import (
	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
)

func convertFirewall(sys *pb.SysFirewall) state.Firewall {
	if sys == nil {
		return state.Firewall{}
	}
	fw := state.Firewall{
		Enabled: sys.GetEnabled(),
		Version: sys.GetVersion(),
	}
	chains := sys.GetSystemRules()
	for _, container := range chains {
		for _, chain := range container.GetChains() {
			fwChain := state.FirewallChain{
				Table:    chain.GetTable(),
				Name:     chain.GetName(),
				Family:   chain.GetFamily(),
				Hook:     chain.GetHook(),
				Priority: chain.GetPriority(),
				Policy:   chain.GetPolicy(),
			}
			for _, rule := range chain.GetRules() {
				fwChain.Rules = append(fwChain.Rules, state.FirewallRule{
					UUID:        rule.GetUUID(),
					Enabled:     rule.GetEnabled(),
					Description: rule.GetDescription(),
					Target:      rule.GetTarget(),
					Parameters:  rule.GetTargetParameters(),
				})
			}
			fw.Chains = append(fw.Chains, fwChain)
		}
		if container.GetRule().GetDescription() != "" {
			legacy := container.GetRule()
			fw.Chains = append(fw.Chains, state.FirewallChain{
				Table: legacy.GetTable(),
				Name:  legacy.GetChain(),
				Rules: []state.FirewallRule{{
					Description: legacy.GetDescription(),
					Target:      legacy.GetTarget(),
					Parameters:  legacy.GetTargetParameters(),
				}},
			})
		}
	}
	return fw
}
