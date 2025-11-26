package state

import "time"

// ViewKind identifies a top-level view inside the TUI router.
type ViewKind string

const (
	ViewDashboard ViewKind = "dashboard"
	ViewRules     ViewKind = "rules"
	ViewFirewall  ViewKind = "firewall"
	ViewNodes     ViewKind = "nodes"
	ViewSettings  ViewKind = "settings"
)

// DefaultViewOrder drives the tab navigation order across the application.
var DefaultViewOrder = []ViewKind{
	ViewDashboard,
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

// Snapshot is a threadsafe copy of the application's state tree.
type Snapshot struct {
	ActiveView ViewKind
	Nodes      []Node
	Stats      Stats
	LastError  string
}
