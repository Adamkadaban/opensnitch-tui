package state

import "time"

// ViewKind identifies a top-level view inside the TUI router.
type ViewKind string

const (
	ViewDashboard ViewKind = "dashboard"
	ViewAlerts    ViewKind = "alerts"
	ViewRules     ViewKind = "rules"
	ViewFirewall  ViewKind = "firewall"
	ViewNodes     ViewKind = "nodes"
	ViewSettings  ViewKind = "settings"
)

// DefaultViewOrder drives the tab navigation order across the application.
var DefaultViewOrder = []ViewKind{
	ViewDashboard,
	ViewAlerts,
	ViewRules,
	ViewFirewall,
	ViewNodes,
	ViewSettings,
}

// NodeStatus captures the health of a daemon connection.
type NodeStatus string

const (
	NodeStatusUnknown      NodeStatus = "unknown"
	NodeStatusDisconnected NodeStatus = "disconnected"
	NodeStatusConnecting   NodeStatus = "connecting"
	NodeStatusReady        NodeStatus = "ready"
	NodeStatusError        NodeStatus = "error"
)

// Node represents a daemon endpoint tracked by the UI.
type Node struct {
	ID              string
	Name            string
	Address         string
	Version         string
	FirewallEnabled bool
	Status          NodeStatus
	LastSeen        time.Time
	Message         string
}

// Stats aggregates daemon telemetry snapshots rendered in the dashboard.
type Stats struct {
	NodeID        string
	NodeName      string
	DaemonVersion string
	Rules         uint64
	Connections   uint64
	Accepted      uint64
	Dropped       uint64
	Ignored       uint64
	RuleHits      uint64
	RuleMisses    uint64
	UpdatedAt     time.Time
}

// Alert represents a daemon alert entry shown in the UI.
type Alert struct {
	ID        string
	NodeID    string
	Text      string
	Priority  string
	Type      string
	Action    string
	CreatedAt time.Time
}

// Firewall represents nftables information for a node.
type Firewall struct {
	Enabled bool
	Version uint32
	Chains  []FirewallChain
}

// FirewallChain contains rules for a specific nftables chain.
type FirewallChain struct {
	Table    string
	Name     string
	Family   string
	Hook     string
	Priority string
	Policy   string
	Rules    []FirewallRule
}

// FirewallRule mirrors a single nftables rule entry.
type FirewallRule struct {
	UUID        string
	Enabled     bool
	Description string
	Target      string
	Parameters  string
}

// Rule represents a daemon rule entry.
type Rule struct {
	NodeID      string
	Name        string
	Description string
	Action      string
	Duration    string
	Enabled     bool
	Precedence  bool
	NoLog       bool
	CreatedAt   time.Time
	Operator    RuleOperator
}

type RuleOperator struct {
	Type      string
	Operand   string
	Data      string
	Sensitive bool
	Children  []RuleOperator
}

// Snapshot is a threadsafe copy of the application's state tree.
type Snapshot struct {
	ActiveView ViewKind
	Nodes      []Node
	Stats      Stats
	Alerts     []Alert
	Firewalls  map[string]Firewall
	Rules      map[string][]Rule
	LastError  string
}
