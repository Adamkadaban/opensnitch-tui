package daemon

import (
	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
)

func convertSystemFirewall(firewall *pb.SysFirewall, nodeID string, running bool) (state.SystemFirewall, bool) {
	if firewall == nil {
		return state.SystemFirewall{}, false
	}
	converted := state.SystemFirewall{
		NodeID:  nodeID,
		Enabled: firewall.GetEnabled(),
		Running: running,
		Version: firewall.GetVersion(),
	}
	if len(firewall.GetSystemRules()) == 0 {
		return converted, true
	}
	converted.SystemRules = make([]state.FirewallRuleGroup, len(firewall.GetSystemRules()))
	for i, group := range firewall.GetSystemRules() {
		converted.SystemRules[i] = convertFirewallRuleGroup(group)
	}
	return converted, true
}

func convertFirewallRuleGroup(group *pb.FwChains) state.FirewallRuleGroup {
	if group == nil {
		return state.FirewallRuleGroup{}
	}
	converted := state.FirewallRuleGroup{}
	if group.GetRule() != nil {
		rule := convertFirewallRule(group.GetRule())
		converted.Rule = &rule
	}
	if len(group.GetChains()) == 0 {
		return converted
	}
	converted.Chains = make([]state.FirewallChain, len(group.GetChains()))
	for i, chain := range group.GetChains() {
		converted.Chains[i] = convertFirewallChain(chain)
	}
	return converted
}

func convertFirewallChain(chain *pb.FwChain) state.FirewallChain {
	if chain == nil {
		return state.FirewallChain{}
	}
	converted := state.FirewallChain{
		Name:     chain.GetName(),
		Table:    chain.GetTable(),
		Family:   chain.GetFamily(),
		Priority: chain.GetPriority(),
		Type:     chain.GetType(),
		Hook:     chain.GetHook(),
		Policy:   chain.GetPolicy(),
	}
	if len(chain.GetRules()) == 0 {
		return converted
	}
	converted.Rules = make([]state.FirewallRule, len(chain.GetRules()))
	for i, rule := range chain.GetRules() {
		converted.Rules[i] = convertFirewallRule(rule)
	}
	return converted
}

func convertFirewallRule(rule *pb.FwRule) state.FirewallRule {
	if rule == nil {
		return state.FirewallRule{}
	}
	converted := state.FirewallRule{
		Table:            rule.GetTable(),
		Chain:            rule.GetChain(),
		UUID:             rule.GetUUID(),
		Enabled:          rule.GetEnabled(),
		Position:         rule.GetPosition(),
		Description:      rule.GetDescription(),
		Parameters:       rule.GetParameters(),
		Target:           rule.GetTarget(),
		TargetParameters: rule.GetTargetParameters(),
	}
	if len(rule.GetExpressions()) == 0 {
		return converted
	}
	converted.Expressions = make([]state.FirewallExpression, len(rule.GetExpressions()))
	for i, expression := range rule.GetExpressions() {
		converted.Expressions[i] = convertFirewallExpression(expression)
	}
	return converted
}

func convertFirewallExpression(expression *pb.Expressions) state.FirewallExpression {
	if expression == nil || expression.GetStatement() == nil {
		return state.FirewallExpression{}
	}
	statement := expression.GetStatement()
	converted := state.FirewallStatement{
		Op:   statement.GetOp(),
		Name: statement.GetName(),
	}
	if len(statement.GetValues()) > 0 {
		converted.Values = make([]state.FirewallStatementValue, len(statement.GetValues()))
		for i, value := range statement.GetValues() {
			if value == nil {
				continue
			}
			converted.Values[i] = state.FirewallStatementValue{
				Key:   value.GetKey(),
				Value: value.GetValue(),
			}
		}
	}
	return state.FirewallExpression{Statement: &converted}
}
