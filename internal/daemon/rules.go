package daemon

import (
	"time"

	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
)

func convertRules(list []*pb.Rule, nodeID string) []state.Rule {
	if len(list) == 0 {
		return nil
	}
	rules := make([]state.Rule, 0, len(list))
	for _, rule := range list {
		rules = append(rules, convertRule(rule, nodeID))
	}
	return rules
}

func convertRule(rule *pb.Rule, nodeID string) state.Rule {
	if rule == nil {
		return state.Rule{}
	}
	converted := state.Rule{
		NodeID:      nodeID,
		Name:        rule.GetName(),
		Description: rule.GetDescription(),
		Action:      rule.GetAction(),
		Duration:    rule.GetDuration(),
		Enabled:     rule.GetEnabled(),
		Precedence:  rule.GetPrecedence(),
		NoLog:       rule.GetNolog(),
		Operator:    convertRuleOperator(rule.GetOperator()),
	}
	if created := rule.GetCreated(); created > 0 {
		converted.CreatedAt = time.Unix(created, 0)
	}
	return converted
}

func convertRuleOperator(op *pb.Operator) state.RuleOperator {
	if op == nil {
		return state.RuleOperator{}
	}
	converted := state.RuleOperator{
		Type:      op.GetType(),
		Operand:   op.GetOperand(),
		Data:      op.GetData(),
		Sensitive: op.GetSensitive(),
	}
	list := op.GetList()
	if len(list) == 0 {
		return converted
	}
	children := make([]state.RuleOperator, len(list))
	for i, child := range list {
		children[i] = convertRuleOperator(child)
	}
	converted.Children = children
	return converted
}

func serializeRule(rule state.Rule) *pb.Rule {
	proto := &pb.Rule{
		Name:        rule.Name,
		Description: rule.Description,
		Enabled:     rule.Enabled,
		Precedence:  rule.Precedence,
		Nolog:       rule.NoLog,
		Action:      rule.Action,
		Duration:    rule.Duration,
		Operator:    serializeRuleOperator(rule.Operator),
	}
	if !rule.CreatedAt.IsZero() {
		proto.Created = rule.CreatedAt.Unix()
	}
	return proto
}

func serializeRuleOperator(op state.RuleOperator) *pb.Operator {
	operator := &pb.Operator{
		Type:      op.Type,
		Operand:   op.Operand,
		Data:      op.Data,
		Sensitive: op.Sensitive,
	}
	if len(op.Children) == 0 {
		return operator
	}
	operator.List = make([]*pb.Operator, len(op.Children))
	for i, child := range op.Children {
		operator.List[i] = serializeRuleOperator(child)
	}
	return operator
}
