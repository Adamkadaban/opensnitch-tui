package controller

// RuleManager exposes CRUD operations for daemon rules.
type RuleManager interface {
	EnableRule(nodeID, ruleName string) error
	DisableRule(nodeID, ruleName string) error
	DeleteRule(nodeID, ruleName string) error
}

// PromptManager resolves interactive connection prompts surfaced by the daemon.
type PromptManager interface {
	ResolvePrompt(decision PromptDecision) error
}

// SettingsManager persists UI configuration choices.
type SettingsManager interface {
	SetTheme(name string) (string, error)
	SetDefaultPromptAction(action string) (string, error)
	SetDefaultPromptDuration(duration string) (string, error)
	SetDefaultPromptTarget(target string) (string, error)
	SetAlertsInterrupt(enabled bool) (bool, error)
	SetPromptTimeout(seconds int) (int, error)
}

// PromptDecision captures an operator's selection for a pending prompt.
type PromptDecision struct {
	PromptID string
	Action   PromptAction
	Duration PromptDuration
	Target   PromptTarget
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
)
