package state

import "time"

// ViewKind identifies a top-level view inside the TUI router.
type ViewKind string

const (
	ViewDashboard ViewKind = "dashboard"
	ViewAlerts    ViewKind = "alerts"
	ViewEvents    ViewKind = "events"
	ViewRules     ViewKind = "rules"
	ViewFirewall  ViewKind = "firewall"
	ViewTasks     ViewKind = "tasks"
	ViewNodes     ViewKind = "nodes"
	ViewSettings  ViewKind = "settings"
)

// DefaultViewOrder drives the tab navigation order across the application.
var DefaultViewOrder = []ViewKind{
	ViewDashboard,
	ViewEvents,
	ViewAlerts,
	ViewRules,
	ViewFirewall,
	ViewTasks,
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

// NodeDaemonConfig is the safe, editable subset of an OpenSnitch v1.8 daemon configuration.
type NodeDaemonConfig struct {
	DefaultAction     string
	DefaultDuration   string
	ProcMonitorMethod string
	LogLevel          int
	LogUTC            bool
	LogMicro          bool
	InterceptUnknown  bool
	Rules             NodeRulesConfig
	Internal          NodeInternalConfig
	FwOptions         NodeFirewallOptions
	Stats             NodeStatsConfig
}

type NodeRulesConfig struct {
	Path            string
	EnableChecksums bool
}

type NodeInternalConfig struct {
	FlushConnsOnStart bool
	GCPercent         int
}

type NodeFirewallOptions struct {
	MonitorInterval string
	QueueBypass     bool
}

type NodeStatsConfig struct {
	MaxEvents int
	MaxStats  int
}

// NodeConfigMetadata contains non-secret, read-only configuration metadata.
type NodeConfigMetadata struct {
	AuthenticationType string
	TLSConfigured      bool
}

// NodeConfigState preserves the full daemon document while exposing only safe fields.
type NodeConfigState struct {
	RawJSON    string
	Config     NodeDaemonConfig
	Metadata   NodeConfigMetadata
	ParseError string
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
	TopUsers       []StatBucket
	Events         []Event
	UpdatedAt      time.Time
}

// Event represents a daemon event entry.
type Event struct {
	NodeID     string
	Time       string
	UnixNano   int64
	Connection Connection
	Rule       Rule
}

// StatBucket captures a label/value pair for breakdown charts.
type StatBucket struct {
	Label string
	Value uint64
}

type AlertPriority string

const (
	AlertPriorityLow    AlertPriority = "LOW"
	AlertPriorityMedium AlertPriority = "MEDIUM"
	AlertPriorityHigh   AlertPriority = "HIGH"
)

type AlertType string

const (
	AlertTypeError   AlertType = "ERROR"
	AlertTypeWarning AlertType = "WARNING"
	AlertTypeInfo    AlertType = "INFO"
)

type AlertAction string

const (
	AlertActionNone      AlertAction = "NONE"
	AlertActionShowAlert AlertAction = "SHOW_ALERT"
	AlertActionSaveToDB  AlertAction = "SAVE_TO_DB"
)

type AlertWhat string

const (
	AlertWhatGeneric     AlertWhat = "GENERIC"
	AlertWhatProcMonitor AlertWhat = "PROC_MONITOR"
	AlertWhatFirewall    AlertWhat = "FIREWALL"
	AlertWhatConnection  AlertWhat = "CONNECTION"
	AlertWhatRule        AlertWhat = "RULE"
	AlertWhatNetlink     AlertWhat = "NETLINK"
	AlertWhatKernelEvent AlertWhat = "KERNEL_EVENT"
)

type AlertPayloadKind string

const (
	AlertPayloadNone       AlertPayloadKind = ""
	AlertPayloadText       AlertPayloadKind = "text"
	AlertPayloadProcess    AlertPayloadKind = "process"
	AlertPayloadConnection AlertPayloadKind = "connection"
	AlertPayloadRule       AlertPayloadKind = "rule"
	AlertPayloadFirewall   AlertPayloadKind = "firewall_rule"
)

// Alert represents a daemon alert entry shown in the UI.
type Alert struct {
	ID           string
	NodeID       string
	Text         string
	Priority     AlertPriority
	Type         AlertType
	Action       AlertAction
	What         AlertWhat
	PayloadKind  AlertPayloadKind
	Process      *Process
	Connection   *Connection
	Rule         *Rule
	FirewallRule *FirewallRule
	CreatedAt    time.Time
}

type Process struct {
	PID         uint64
	PPID        uint64
	UID         uint64
	Comm        string
	Path        string
	Args        []string
	Env         map[string]string
	CWD         string
	Checksums   map[string]string
	IOReads     uint64
	IOWrites    uint64
	NetReads    uint64
	NetWrites   uint64
	ProcessTree []ProcessTreeEntry
}

type ProcessTreeEntry struct {
	Path string
	PID  uint32
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
	UpdatedAt   time.Time
	Operator    RuleOperator
}

type RuleOperator struct {
	Type      string
	Operand   string
	Data      string
	Sensitive bool
	Children  []RuleOperator
}

// SystemFirewall is the firewall state reported by one daemon node.
type SystemFirewall struct {
	NodeID      string
	Enabled     bool
	Running     bool
	Version     uint32
	SystemRules []FirewallRuleGroup
}

// FirewallRuleGroup preserves one protocol FwChains entry.
type FirewallRuleGroup struct {
	Rule   *FirewallRule
	Chains []FirewallChain
}

// FirewallChain describes a system firewall chain and its rules.
type FirewallChain struct {
	Name     string
	Table    string
	Family   string
	Priority string
	Type     string
	Hook     string
	Policy   string
	Rules    []FirewallRule
}

// FirewallRule describes one system firewall rule.
type FirewallRule struct {
	Table            string
	Chain            string
	UUID             string
	Enabled          bool
	Position         uint64
	Description      string
	Parameters       string
	Expressions      []FirewallExpression
	Target           string
	TargetParameters string
}

// FirewallExpression preserves an optional protocol expression statement.
type FirewallExpression struct {
	Statement *FirewallStatement
}

// FirewallStatement describes a firewall expression operation.
type FirewallStatement struct {
	Op     string
	Name   string
	Values []FirewallStatementValue
}

// FirewallStatementValue stores a statement key/value pair.
type FirewallStatementValue struct {
	Key   string
	Value string
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
	ProcessEnv       map[string]string
	ProcessChecksums map[string]string
	ProcessTree      []ProcessTreeEntry
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
	ActiveView      ViewKind
	Nodes           []Node
	Stats           Stats
	Alerts          []Alert
	Rules           map[string][]Rule
	SystemFirewalls map[string]SystemFirewall
	NodeConfigs     map[string]NodeConfigState
	Settings        Settings
	Prompts         []Prompt
	LastError       string
	LastErrorAt     time.Time
}
