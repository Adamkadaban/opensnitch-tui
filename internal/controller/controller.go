package controller

import (
	"context"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
)

// RuleManager exposes CRUD operations for daemon rules.
type RuleManager interface {
	EnableRule(nodeID, ruleName string) error
	DisableRule(nodeID, ruleName string) error
	DeleteRule(nodeID, ruleName string) error
	ChangeRule(nodeID string, rule state.Rule) error
}

// RuleBatchManager applies new and replacement rules after daemon acknowledgement.
type RuleBatchManager interface {
	ApplyRules(ctx context.Context, nodeID string, rules []state.Rule) error
}

// RuleArchive stores canonical rule JSON under a fixed node-scoped directory.
type RuleArchive interface {
	Directory(node state.Node) string
	Export(ctx context.Context, node state.Node, rules []state.Rule) (string, error)
	Import(ctx context.Context, node state.Node) ([]state.Rule, string, error)
}

// AlertArchive stores one sanitized structured alert under a fixed private directory.
type AlertArchive interface {
	Directory() string
	Export(ctx context.Context, alert state.Alert) (string, error)
}

// FirewallManager controls the system firewall on one daemon node.
type FirewallManager interface {
	EnableFirewall(ctx context.Context, nodeID string) error
	DisableFirewall(ctx context.Context, nodeID string) error
	ReloadFirewall(ctx context.Context, nodeID string) error
}

// NodeConfigManager applies a validated safe daemon configuration subset.
type NodeConfigManager interface {
	ApplyNodeConfig(ctx context.Context, nodeID string, config state.NodeDaemonConfig) error
}

// PromptManager resolves interactive connection prompts surfaced by the daemon.
type PromptManager interface {
	ResolvePrompt(decision PromptDecision) error
	PausePrompt(promptID string) error
	ResumePrompt(promptID string) error
}

// SettingsManager persists UI configuration choices.
type SettingsManager interface {
	SetTheme(name string) (string, error)
	SetDefaultPromptAction(action string) (string, error)
	SetDefaultPromptDuration(duration string) (string, error)
	SetDefaultPromptTarget(target string) (string, error)
	SetAlertsInterrupt(enabled bool) (bool, error)
	SetPromptTimeout(seconds int) (int, error)
	SetPausePromptOnInspect(enabled bool) (bool, error)
	SetYaraRuleDir(path string) (string, error)
	SetYaraEnabled(enabled bool) (bool, error)
}

// PromptDecision captures an operator's selection for a pending prompt.
type PromptDecision struct {
	PromptID   string
	Action     PromptAction
	Duration   PromptDuration
	Target     PromptTarget
	Conditions []PromptCondition
}

// PromptCondition adds an exact match from the pending connection to a rule.
type PromptCondition struct {
	Target PromptTarget
}

type PromptAction string

const (
	PromptActionAllow  PromptAction = "allow"
	PromptActionDeny   PromptAction = "deny"
	PromptActionReject PromptAction = "reject"
)

type PromptDuration string

const (
	PromptDurationOnce         PromptDuration = "once"
	PromptDuration30Seconds    PromptDuration = "30s"
	PromptDuration5Minutes     PromptDuration = "5m"
	PromptDuration15Minutes    PromptDuration = "15m"
	PromptDuration30Minutes    PromptDuration = "30m"
	PromptDuration1Hour        PromptDuration = "1h"
	PromptDuration12Hours      PromptDuration = "12h"
	PromptDurationUntilRestart PromptDuration = "until restart"
	PromptDurationAlways       PromptDuration = "always"
)

type PromptTarget string

const (
	PromptTargetProcessPath     PromptTarget = "process.path"
	PromptTargetProcessCmd      PromptTarget = "process.command"
	PromptTargetProcessID       PromptTarget = "process.id"
	PromptTargetUserID          PromptTarget = "user.id"
	PromptTargetDestinationIP   PromptTarget = "dest.ip"
	PromptTargetDestinationHost PromptTarget = "dest.host"
	PromptTargetDestinationPort PromptTarget = "dest.port"
	PromptTargetChecksumMD5     PromptTarget = "process.hash.md5"
)
