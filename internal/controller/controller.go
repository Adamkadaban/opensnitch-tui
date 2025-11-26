package controller

// Firewall exposes actions to manage daemon firewalls.
type Firewall interface {
	EnableFirewall(nodeID string) error
	DisableFirewall(nodeID string) error
	ReloadFirewall(nodeID string) error
}

// RuleManager exposes CRUD operations for daemon rules.
type RuleManager interface {
	EnableRule(nodeID, ruleName string) error
	DisableRule(nodeID, ruleName string) error
	DeleteRule(nodeID, ruleName string) error
}
