package state

import (
	"fmt"
	"time"
)

const (
	NodeLogLevelDebug = iota
	NodeLogLevelInfo
	NodeLogLevelImportant
	NodeLogLevelWarning
	NodeLogLevelError
	NodeLogLevelFatal
)

const (
	NodeGCPercentMin = 0
	NodeGCPercentMax = 100
	NodeMaxEventsMin = 1
	NodeMaxEventsMax = 100_000
	NodeMaxStatsMin  = 1
	NodeMaxStatsMax  = 10_000
)

const NodeMonitorIntervalMax = time.Hour

var (
	NodeDefaultActions     = []string{"deny", "allow", "reject"}
	NodeDefaultDurations   = []string{"once", "until restart", "always"}
	NodeProcMonitorMethods = []string{
		"proc",
		"ebpf",
		"audit",
	}
)

// ValidateNodeDaemonConfig rejects values outside the safe editor's v1.8 bounds.
func ValidateNodeDaemonConfig(config NodeDaemonConfig) error {
	if !containsNodeOption(NodeDefaultActions, config.DefaultAction) {
		return fmt.Errorf("invalid default action %q", config.DefaultAction)
	}
	if !containsNodeOption(NodeDefaultDurations, config.DefaultDuration) {
		return fmt.Errorf("invalid default duration %q", config.DefaultDuration)
	}
	if !containsNodeOption(NodeProcMonitorMethods, config.ProcMonitorMethod) {
		return fmt.Errorf("invalid process monitor method %q", config.ProcMonitorMethod)
	}
	if config.LogLevel < NodeLogLevelDebug || config.LogLevel > NodeLogLevelFatal {
		return fmt.Errorf("log level must be between %d and %d", NodeLogLevelDebug, NodeLogLevelFatal)
	}
	if config.Internal.GCPercent < NodeGCPercentMin || config.Internal.GCPercent > NodeGCPercentMax {
		return fmt.Errorf("GC percent must be between %d and %d", NodeGCPercentMin, NodeGCPercentMax)
	}
	interval, err := time.ParseDuration(config.FwOptions.MonitorInterval)
	if err != nil {
		return fmt.Errorf("invalid firewall monitor interval: %w", err)
	}
	if interval < 0 || interval > NodeMonitorIntervalMax {
		return fmt.Errorf("firewall monitor interval must be between 0s and %s", NodeMonitorIntervalMax)
	}
	if config.Stats.MaxEvents < NodeMaxEventsMin || config.Stats.MaxEvents > NodeMaxEventsMax {
		return fmt.Errorf("max events must be between %d and %d", NodeMaxEventsMin, NodeMaxEventsMax)
	}
	if config.Stats.MaxStats < NodeMaxStatsMin || config.Stats.MaxStats > NodeMaxStatsMax {
		return fmt.Errorf("max stats must be between %d and %d", NodeMaxStatsMin, NodeMaxStatsMax)
	}
	return nil
}

func containsNodeOption(options []string, value string) bool {
	for _, option := range options {
		if option == value {
			return true
		}
	}
	return false
}
