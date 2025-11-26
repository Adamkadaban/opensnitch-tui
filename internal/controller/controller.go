package controller

// RuleManager exposes CRUD operations for daemon rules.
type RuleManager interface {
	EnableRule(nodeID, ruleName string) error
	DisableRule(nodeID, ruleName string) error
	DeleteRule(nodeID, ruleName string) error
}
