package state

import "time"

// ViewKind identifies a top-level view inside the TUI router.
type ViewKind string

const (
	ViewDashboard ViewKind = "dashboard"
	ViewAlerts    ViewKind = "alerts"
	ViewRules     ViewKind = "rules"
	ViewNodes     ViewKind = "nodes"
	ViewSettings  ViewKind = "settings"
)

// DefaultViewOrder drives the tab navigation order across the application.
var DefaultViewOrder = []ViewKind{
	ViewDashboard,
	ViewAlerts,
	ViewRules,
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
	NodeID         string
	NodeName       string
	DaemonVersion  string
	Rules          uint64
	Connections    uint64
	Accepted       uint64
	Dropped        uint64
	Ignored        uint64
	RuleHits       uint64
	RuleMisses     uint64
	TopDestHosts   []StatBucket
	TopDestPorts   []StatBucket
	TopExecutables []StatBucket
	UpdatedAt      time.Time
}

// StatBucket captures a label/value pair for breakdown charts.
type StatBucket struct {
	Label string
	Value uint64
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

// Settings captures user preferences affecting UI behavior.
type Settings struct {
	ThemeName             string
	DefaultPromptAction   string
	DefaultPromptDuration string
	DefaultPromptTarget   string
	PromptTimeout         time.Duration
	AlertsInterrupt       bool
	PausePromptOnInspect  bool
	YaraRuleDir           string
	YaraEnabled           bool
}

// Connection stores the details of an outbound connection awaiting operator input.
type Connection struct {
	Protocol         string
	SrcIP            string
	SrcPort          uint32
	DstIP            string
	DstHost          string
	DstPort          uint32
	UserID           uint32
	ProcessID        uint32
	ProcessPath      string
	ProcessCWD       string
	ProcessArgs      []string
	ProcessChecksums map[string]string
}

// Prompt captures a pending AskRule request from a daemon node.
type Prompt struct {
	ID          string
	NodeID      string
	NodeName    string
	Connection  Connection
	RequestedAt time.Time
	ExpiresAt   time.Time
	Paused      bool
	Remaining   time.Duration
}

// Snapshot is a threadsafe copy of the application's state tree.
type Snapshot struct {
	ActiveView  ViewKind
	Nodes       []Node
	Stats       Stats
	Alerts      []Alert
	Rules       map[string][]Rule
	Settings    Settings
	Prompts     []Prompt
	LastError   string
	LastErrorAt time.Time
}
