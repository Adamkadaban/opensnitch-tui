package daemon

import (
	"context"
	"fmt"

	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
)

// EnableFirewall starts and enables the system firewall after daemon acknowledgement.
func (s *Server) EnableFirewall(ctx context.Context, nodeID string) error {
	return s.updateFirewallState(ctx, nodeID, func(firewall *state.SystemFirewall) {
		firewall.Enabled = true
		firewall.Running = true
	})
}

// DisableFirewall stops and disables the system firewall after daemon acknowledgement.
func (s *Server) DisableFirewall(ctx context.Context, nodeID string) error {
	return s.updateFirewallState(ctx, nodeID, func(firewall *state.SystemFirewall) {
		firewall.Enabled = false
		firewall.Running = false
	})
}

// ReloadFirewall sends the selected node's normalized firewall definition to the daemon.
func (s *Server) ReloadFirewall(ctx context.Context, nodeID string) error {
	firewall, ok := s.store.SystemFirewall(nodeID)
	if !ok {
		return fmt.Errorf("system firewall not found for %s", nodeID)
	}
	_, err := s.SendNotification(ctx, nodeID, &pb.Notification{
		Type:        pb.Action_RELOAD_FW_RULES,
		SysFirewall: serializeSystemFirewall(firewall),
	})
	return err
}

func (s *Server) updateFirewallState(
	ctx context.Context,
	nodeID string,
	mutate func(*state.SystemFirewall),
) error {
	firewall, ok := s.store.SystemFirewall(nodeID)
	if !ok {
		return fmt.Errorf("system firewall not found for %s", nodeID)
	}
	mutate(&firewall)
	if _, err := s.SendNotification(ctx, nodeID, &pb.Notification{
		Type:        pb.Action_RELOAD_FW_RULES,
		SysFirewall: serializeSystemFirewall(firewall),
	}); err != nil {
		return err
	}
	s.store.SetSystemFirewall(nodeID, firewall)
	return nil
}

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

func serializeSystemFirewall(firewall state.SystemFirewall) *pb.SysFirewall {
	converted := &pb.SysFirewall{
		Enabled: firewall.Enabled,
		Version: firewall.Version,
	}
	if len(firewall.SystemRules) == 0 {
		return converted
	}
	converted.SystemRules = make([]*pb.FwChains, len(firewall.SystemRules))
	for i, group := range firewall.SystemRules {
		converted.SystemRules[i] = serializeFirewallRuleGroup(group)
	}
	return converted
}

func serializeFirewallRuleGroup(group state.FirewallRuleGroup) *pb.FwChains {
	converted := &pb.FwChains{}
	if group.Rule != nil {
		converted.Rule = serializeFirewallRule(*group.Rule)
	}
	if len(group.Chains) == 0 {
		return converted
	}
	converted.Chains = make([]*pb.FwChain, len(group.Chains))
	for i, chain := range group.Chains {
		converted.Chains[i] = serializeFirewallChain(chain)
	}
	return converted
}

func serializeFirewallChain(chain state.FirewallChain) *pb.FwChain {
	converted := &pb.FwChain{
		Name:     chain.Name,
		Table:    chain.Table,
		Family:   chain.Family,
		Priority: chain.Priority,
		Type:     chain.Type,
		Hook:     chain.Hook,
		Policy:   chain.Policy,
	}
	if len(chain.Rules) == 0 {
		return converted
	}
	converted.Rules = make([]*pb.FwRule, len(chain.Rules))
	for i, rule := range chain.Rules {
		converted.Rules[i] = serializeFirewallRule(rule)
	}
	return converted
}

func serializeFirewallRule(rule state.FirewallRule) *pb.FwRule {
	converted := &pb.FwRule{
		Table:            rule.Table,
		Chain:            rule.Chain,
		UUID:             rule.UUID,
		Enabled:          rule.Enabled,
		Position:         rule.Position,
		Description:      rule.Description,
		Parameters:       rule.Parameters,
		Target:           rule.Target,
		TargetParameters: rule.TargetParameters,
	}
	if len(rule.Expressions) == 0 {
		return converted
	}
	converted.Expressions = make([]*pb.Expressions, len(rule.Expressions))
	for i, expression := range rule.Expressions {
		converted.Expressions[i] = serializeFirewallExpression(expression)
	}
	return converted
}

func serializeFirewallExpression(expression state.FirewallExpression) *pb.Expressions {
	converted := &pb.Expressions{}
	if expression.Statement == nil {
		return converted
	}
	statement := expression.Statement
	converted.Statement = &pb.Statement{
		Op:   statement.Op,
		Name: statement.Name,
	}
	if len(statement.Values) == 0 {
		return converted
	}
	converted.Statement.Values = make([]*pb.StatementValues, len(statement.Values))
	for i, value := range statement.Values {
		converted.Statement.Values[i] = &pb.StatementValues{
			Key:   value.Key,
			Value: value.Value,
		}
	}
	return converted
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
