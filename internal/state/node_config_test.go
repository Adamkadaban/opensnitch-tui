package state

import "testing"

func TestNodeConfigsAreIsolatedAndSnapshotsAreCopied(t *testing.T) {
	store := NewStore()
	first := NodeConfigState{
		RawJSON: `{"node":1}`,
		Config: NodeDaemonConfig{
			DefaultAction: "allow",
			Rules:         NodeRulesConfig{Path: "/node-1"},
		},
	}
	second := NodeConfigState{
		RawJSON: `{"node":2}`,
		Config: NodeDaemonConfig{
			DefaultAction: "deny",
			Rules:         NodeRulesConfig{Path: "/node-2"},
		},
	}
	store.SetNodeConfig("node-1", first)
	store.SetNodeConfig("node-2", second)

	snapshot := store.Snapshot()
	mutated := snapshot.NodeConfigs["node-1"]
	mutated.RawJSON = "changed"
	mutated.Config.Rules.Path = "changed"
	snapshot.NodeConfigs["node-1"] = mutated
	delete(snapshot.NodeConfigs, "node-2")

	firstStored, ok := store.NodeConfig("node-1")
	if !ok || firstStored.RawJSON != first.RawJSON || firstStored.Config.Rules.Path != "/node-1" {
		t.Fatalf("snapshot mutation leaked into node-1: %+v", firstStored)
	}
	secondStored, ok := store.NodeConfig("node-2")
	if !ok || secondStored.RawJSON != second.RawJSON || secondStored.Config.Rules.Path != "/node-2" {
		t.Fatalf("node-1 mutation affected node-2: %+v", secondStored)
	}
}

func TestValidateNodeDaemonConfig(t *testing.T) {
	config := NodeDaemonConfig{
		DefaultAction:     "deny",
		DefaultDuration:   "once",
		ProcMonitorMethod: "ebpf",
		LogLevel:          NodeLogLevelImportant,
		Internal:          NodeInternalConfig{GCPercent: 100},
		FwOptions:         NodeFirewallOptions{MonitorInterval: "15s"},
		Stats:             NodeStatsConfig{MaxEvents: 250, MaxStats: 25},
	}
	if err := ValidateNodeDaemonConfig(config); err != nil {
		t.Fatalf("valid configuration rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*NodeDaemonConfig)
	}{
		{name: "action", mutate: func(c *NodeDaemonConfig) { c.DefaultAction = "drop" }},
		{name: "duration", mutate: func(c *NodeDaemonConfig) { c.DefaultDuration = "forever" }},
		{name: "monitor", mutate: func(c *NodeDaemonConfig) { c.ProcMonitorMethod = "fanotify" }},
		{name: "log level", mutate: func(c *NodeDaemonConfig) { c.LogLevel = 6 }},
		{name: "GC", mutate: func(c *NodeDaemonConfig) { c.Internal.GCPercent = 101 }},
		{name: "interval", mutate: func(c *NodeDaemonConfig) { c.FwOptions.MonitorInterval = "invalid" }},
		{name: "events", mutate: func(c *NodeDaemonConfig) { c.Stats.MaxEvents = 0 }},
		{name: "stats", mutate: func(c *NodeDaemonConfig) { c.Stats.MaxStats = NodeMaxStatsMax + 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invalid := config
			test.mutate(&invalid)
			if err := ValidateNodeDaemonConfig(invalid); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
