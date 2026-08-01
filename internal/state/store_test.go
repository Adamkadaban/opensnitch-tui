package state

import (
	"fmt"
	"testing"
	"time"
)

func TestStoreUpsertNodeMergesExisting(t *testing.T) {
	store := NewStore()

	original := Node{ID: "node-1", Name: "alpha", Address: "10.0.0.1", FirewallEnabled: true}
	store.UpsertNode(original)

	updated := Node{ID: "node-1", Message: "ready", Version: "1.0.0"}
	store.UpsertNode(updated)

	snapshot := store.Snapshot()
	if len(snapshot.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(snapshot.Nodes))
	}
	node := snapshot.Nodes[0]
	if node.Name != original.Name {
		t.Fatalf("expected name %q to persist, got %q", original.Name, node.Name)
	}
	if node.Version != updated.Version {
		t.Fatalf("expected version %q, got %q", updated.Version, node.Version)
	}
	if node.Message != updated.Message {
		t.Fatalf("expected message %q, got %q", updated.Message, node.Message)
	}
	if !node.FirewallEnabled {
		t.Fatalf("expected firewall flag to remain true")
	}
}

func TestStoreUpdateNodeStatusCreatesNewEntry(t *testing.T) {
	store := NewStore()
	timestamp := time.Now().Truncate(time.Millisecond)

	store.UpdateNodeStatus("node-2", NodeStatusReady, "connected", timestamp)

	snapshot := store.Snapshot()
	if len(snapshot.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(snapshot.Nodes))
	}
	node := snapshot.Nodes[0]
	if node.Status != NodeStatusReady {
		t.Fatalf("expected status %q, got %q", NodeStatusReady, node.Status)
	}
	if node.Message != "connected" {
		t.Fatalf("expected message connected, got %q", node.Message)
	}
	if !node.LastSeen.Equal(timestamp) {
		t.Fatalf("expected last seen %s, got %s", timestamp, node.LastSeen)
	}
}

func TestStoreUpdateNodeStatusNotifiesSubscribersOnNewNode(t *testing.T) {
	store := NewStore()
	sub := store.Subscribe()
	t.Cleanup(sub.Close)

	done := make(chan struct{})
	go func() {
		if _, ok := <-sub.Events(); ok {
			close(done)
		}
	}()

	store.UpdateNodeStatus("node-new", NodeStatusReady, "connected", time.Now())

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected notification after adding new node via UpdateNodeStatus")
	}
}

func TestStoreSetStatsAndError(t *testing.T) {
	store := NewStore()
	stats := Stats{NodeID: "node-1", Rules: 10}
	store.SetStats(stats)
	store.SetError("boom")

	snapshot := store.Snapshot()
	if snapshot.Stats.NodeID != stats.NodeID || snapshot.Stats.Rules != stats.Rules {
		t.Fatalf("expected stats %+v, got %+v", stats, snapshot.Stats)
	}
	if snapshot.LastError != "boom" {
		t.Fatalf("expected last error boom, got %q", snapshot.LastError)
	}
	if snapshot.LastErrorAt.IsZero() {
		t.Fatal("expected last error timestamp to be set")
	}
}

func TestStoreSnapshotCopy(t *testing.T) {
	store := NewStore()
	store.snapshot.Nodes = []Node{{ID: "n1"}}
	store.snapshot.Alerts = []Alert{{ID: "a1", Text: "alert"}}

	snap := store.Snapshot()
	snap.Nodes[0].Name = "changed"
	snap.Alerts[0].Text = "mutated"

	if store.snapshot.Nodes[0].Name != "" {
		t.Fatalf("expected nodes copy to be isolated")
	}
	if store.snapshot.Alerts[0].Text != "alert" {
		t.Fatalf("expected alerts copy to be isolated")
	}
}

func TestStoreErrorAutoExpires(t *testing.T) {
	store := NewStore()
	store.errorTTL = 10 * time.Millisecond

	store.SetError("timeout")
	time.Sleep(25 * time.Millisecond)
	if err := store.Snapshot().LastError; err != "" {
		t.Fatalf("expected error to expire, still seeing %q", err)
	}
}

func TestStoreSetErrorReplacesExpiryTimer(t *testing.T) {
	store := NewStore()
	store.errorTTL = time.Second

	store.SetError("first")
	firstTimer := store.errorTimer
	store.SetError("second")

	if firstTimer == store.errorTimer {
		t.Fatal("expected a replacement error timer")
	}
	if firstTimer.Stop() {
		t.Fatal("expected the first error timer to already be stopped")
	}
	if got := store.Snapshot().LastError; got != "second" {
		t.Fatalf("expected latest error, got %q", got)
	}
}

func TestStoreClearErrorStopsExpiryTimer(t *testing.T) {
	store := NewStore()
	store.errorTTL = time.Second
	store.SetError("boom")
	timer := store.errorTimer

	store.ClearError()

	if store.errorTimer != nil {
		t.Fatal("expected error timer cleanup")
	}
	if timer.Stop() {
		t.Fatal("expected error timer to already be stopped")
	}
	if got := store.Snapshot().LastError; got != "" {
		t.Fatalf("expected cleared error, got %q", got)
	}
}
func TestStoreSubscriptionReceivesNotifications(t *testing.T) {
	store := NewStore()
	sub := store.Subscribe()
	defer sub.Close()

	done := make(chan struct{})
	go func() {
		if _, ok := <-sub.Events(); ok {
			close(done)
		}
	}()

	store.SetStats(Stats{Rules: 1})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for store notification")
	}
}

func TestStoreAddAlert(t *testing.T) {
	store := NewStore()
	store.AddAlert(Alert{ID: "a1", Text: "one", PayloadKind: AlertPayloadText})
	store.AddAlert(Alert{ID: "a2", Text: "two", PayloadKind: AlertPayloadText})

	snap := store.Snapshot()
	if len(snap.Alerts) != 2 {
		t.Fatalf("expected two alerts, got %d", len(snap.Alerts))
	}

	if snap.Alerts[0].ID != "a2" || snap.Alerts[1].ID != "a1" {
		t.Fatalf("alerts order unexpected: %#v", snap.Alerts)
	}

	for i := 0; i < maxAlerts; i++ {
		store.AddAlert(Alert{ID: fmt.Sprintf("extra-%d", i)})
	}
	snap = store.Snapshot()
	if len(snap.Alerts) != maxAlerts {
		t.Fatalf("expected maxAlerts entries, got %d", len(snap.Alerts))
	}
	for _, alert := range snap.Alerts {
		if alert.ID == "a1" || alert.ID == "a2" {
			t.Fatalf("expected oldest alerts to be evicted")
		}
	}
}

func TestStoreAlertCloneHandlesNilPayloadVariants(t *testing.T) {
	store := NewStore()
	kinds := []AlertPayloadKind{
		AlertPayloadNone,
		AlertPayloadText,
		AlertPayloadProcess,
		AlertPayloadConnection,
		AlertPayloadRule,
		AlertPayloadFirewall,
	}
	for _, kind := range kinds {
		store.AddAlert(Alert{ID: string(kind), PayloadKind: kind})
	}
	alerts := store.Snapshot().Alerts
	if len(alerts) != len(kinds) {
		t.Fatalf("expected %d alerts, got %d", len(kinds), len(alerts))
	}
	for _, alert := range alerts {
		if alert.Process != nil || alert.Connection != nil || alert.Rule != nil || alert.FirewallRule != nil {
			t.Fatalf("unexpected payload synthesized for %#v", alert)
		}
	}
}

func TestStoreAlertDeepCopyIsolation(t *testing.T) {
	alert := Alert{
		ID:          "a1",
		PayloadKind: AlertPayloadProcess,
		Process: &Process{
			Args:        []string{"one"},
			Env:         map[string]string{"TOKEN": "one"},
			Checksums:   map[string]string{"sha256": "one"},
			ProcessTree: []ProcessTreeEntry{{Path: "one", PID: 1}},
		},
		Rule: &Rule{Operator: RuleOperator{Children: []RuleOperator{{Data: "one"}}}},
		FirewallRule: &FirewallRule{Expressions: []FirewallExpression{{Statement: &FirewallStatement{
			Values: []FirewallStatementValue{{Key: "one", Value: "one"}},
		}}}},
		Connection: &Connection{
			ProcessArgs: []string{"one"}, ProcessEnv: map[string]string{"A": "one"},
			ProcessChecksums: map[string]string{"sha256": "one"},
			ProcessTree:      []ProcessTreeEntry{{Path: "one", PID: 1}},
		},
	}
	store := NewStore()
	store.AddAlert(alert)

	alert.Process.Args[0] = "mutated"
	alert.Process.Env["TOKEN"] = "mutated"
	alert.Rule.Operator.Children[0].Data = "mutated"
	alert.FirewallRule.Expressions[0].Statement.Values[0].Value = "mutated"
	alert.Connection.ProcessEnv["A"] = "mutated"

	first := store.Snapshot().Alerts[0]
	if first.Process.Args[0] != "one" || first.Process.Env["TOKEN"] != "one" ||
		first.Rule.Operator.Children[0].Data != "one" ||
		first.FirewallRule.Expressions[0].Statement.Values[0].Value != "one" ||
		first.Connection.ProcessEnv["A"] != "one" {
		t.Fatalf("store retained aliases to input: %#v", first)
	}

	first.Process.Args[0] = "snapshot"
	first.Process.Env["TOKEN"] = "snapshot"
	first.Rule.Operator.Children[0].Data = "snapshot"
	first.FirewallRule.Expressions[0].Statement.Values[0].Value = "snapshot"
	first.Connection.ProcessTree[0].Path = "snapshot"
	second := store.Snapshot().Alerts[0]
	if second.Process.Args[0] != "one" || second.Process.Env["TOKEN"] != "one" ||
		second.Rule.Operator.Children[0].Data != "one" ||
		second.FirewallRule.Expressions[0].Statement.Values[0].Value != "one" ||
		second.Connection.ProcessTree[0].Path != "one" {
		t.Fatalf("snapshot retained aliases to store: %#v", second)
	}
}

func TestStoreSetRulesCopiesData(t *testing.T) {
	store := NewStore()
	store.SetStats(Stats{NodeID: "node-1"})
	rules := []Rule{{
		NodeID:      "node-1",
		Name:        "ssh",
		Description: "allow ssh",
		Operator: RuleOperator{
			Type:    "process.path",
			Operand: "==",
			Children: []RuleOperator{{
				Type:     "list",
				Operand:  "contains",
				Children: []RuleOperator{{Type: "literal", Data: "/usr/bin/ssh"}},
			}},
		},
	}}
	store.SetRules("node-1", rules)

	// Mutate original slice to confirm store keeps a copy.
	rules[0].Name = "mutated"

	snap := store.Snapshot()
	if got := snap.Rules["node-1"][0].Name; got != "ssh" {
		t.Fatalf("expected snapshot rule name ssh, got %q", got)
	}

	// Mutate snapshot to confirm internal state remains untouched.
	snap.Rules["node-1"][0].Description = "changed"
	snap.Rules["node-1"][0].Operator.Children[0].Children[0].Data = "modified"
	if store.snapshot.Rules["node-1"][0].Description != "allow ssh" {
		t.Fatalf("expected internal rule description to remain unchanged")
	}
	if store.snapshot.Rules["node-1"][0].Operator.Children[0].Children[0].Data != "/usr/bin/ssh" {
		t.Fatalf("expected operator data to remain unchanged")
	}

	if store.snapshot.Stats.Rules != 1 {
		t.Fatalf("expected stats to reflect rule count, got %d", store.snapshot.Stats.Rules)
	}
}

func TestStoreSystemFirewallsAreIsolatedAndDeepCopied(t *testing.T) {
	store := NewStore()
	first := testSystemFirewall("node-1", "rule-1")
	second := testSystemFirewall("node-2", "rule-2")

	store.SetSystemFirewall("node-1", first)
	store.SetSystemFirewall("node-2", second)

	first.SystemRules[0].Rule.Description = "mutated input"
	first.SystemRules[0].Chains[0].Rules[0].Expressions[0].Statement.Values[0].Value = "mutated input"

	selected, ok := store.SystemFirewall("node-1")
	if !ok {
		t.Fatal("expected node-1 firewall")
	}
	selected.SystemRules[0].Rule.Description = "mutated selector"
	selected.SystemRules[0].Chains[0].Rules[0].Expressions[0].Statement.Values[0].Value = "mutated selector"

	snapshot := store.Snapshot()
	snapshot.SystemFirewalls["node-1"].SystemRules[0].Rule.Description = "mutated snapshot"
	snapshot.SystemFirewalls["node-1"].SystemRules[0].Chains[0].Rules[0].Expressions[0].Statement.Values[0].Value = "mutated snapshot"

	current := store.Snapshot()
	nodeOne := current.SystemFirewalls["node-1"]
	nodeTwo := current.SystemFirewalls["node-2"]
	if nodeOne.SystemRules[0].Rule.Description != "rule-1" {
		t.Fatalf("expected node-1 legacy rule to remain unchanged, got %q", nodeOne.SystemRules[0].Rule.Description)
	}
	if got := nodeOne.SystemRules[0].Chains[0].Rules[0].Expressions[0].Statement.Values[0].Value; got != "rule-1" {
		t.Fatalf("expected node-1 nested value to remain unchanged, got %q", got)
	}
	if nodeTwo.SystemRules[0].Rule.Description != "rule-2" {
		t.Fatalf("expected node-2 state to remain isolated, got %q", nodeTwo.SystemRules[0].Rule.Description)
	}
}

func TestStoreUpdateSystemFirewall(t *testing.T) {
	store := NewStore()
	store.SetSystemFirewall("node-1", testSystemFirewall("wrong-node", "rule-1"))

	if !store.UpdateSystemFirewall("node-1", func(firewall *SystemFirewall) {
		firewall.NodeID = "mutated"
		firewall.Running = false
		firewall.SystemRules[0].Chains[0].Policy = "drop"
	}) {
		t.Fatal("expected firewall update to succeed")
	}
	updated, ok := store.SystemFirewall("node-1")
	if !ok {
		t.Fatal("expected updated firewall")
	}
	if updated.NodeID != "node-1" {
		t.Fatalf("expected node ID to remain keyed to node-1, got %q", updated.NodeID)
	}
	if updated.Running {
		t.Fatal("expected running state to be updated")
	}
	if updated.SystemRules[0].Chains[0].Policy != "drop" {
		t.Fatalf("expected updated chain policy, got %q", updated.SystemRules[0].Chains[0].Policy)
	}
	if store.UpdateSystemFirewall("missing", func(_ *SystemFirewall) {}) {
		t.Fatal("expected missing firewall update to fail")
	}
	if store.UpdateSystemFirewall("node-1", nil) {
		t.Fatal("expected nil firewall update to fail")
	}
	if !store.RemoveSystemFirewall("node-1") {
		t.Fatal("expected firewall removal to succeed")
	}
	if _, ok := store.SystemFirewall("node-1"); ok {
		t.Fatal("expected firewall to be removed")
	}
	if store.RemoveSystemFirewall("node-1") {
		t.Fatal("expected repeated firewall removal to fail")
	}
}

func TestStoreSnapshotDeepCopiesEventData(t *testing.T) {
	store := NewStore()
	store.SetStats(Stats{Events: []Event{{
		Connection: Connection{
			ProcessArgs:      []string{"--flag"},
			ProcessChecksums: map[string]string{"sha256": "sum"},
		},
		Rule: Rule{Operator: RuleOperator{Children: []RuleOperator{{Data: "child"}}}},
	}}})

	snapshot := store.Snapshot()
	snapshot.Stats.Events[0].Connection.ProcessArgs[0] = "mutated"
	snapshot.Stats.Events[0].Connection.ProcessChecksums["sha256"] = "mutated"
	snapshot.Stats.Events[0].Rule.Operator.Children[0].Data = "mutated"

	current := store.Snapshot().Stats.Events[0]
	if current.Connection.ProcessArgs[0] != "--flag" {
		t.Fatalf("expected event args to remain isolated, got %q", current.Connection.ProcessArgs[0])
	}
	if current.Connection.ProcessChecksums["sha256"] != "sum" {
		t.Fatalf("expected event checksums to remain isolated, got %q", current.Connection.ProcessChecksums["sha256"])
	}
	if current.Rule.Operator.Children[0].Data != "child" {
		t.Fatalf("expected event rule operator to remain isolated, got %q", current.Rule.Operator.Children[0].Data)
	}
}

func testSystemFirewall(nodeID, marker string) SystemFirewall {
	return SystemFirewall{
		NodeID:  nodeID,
		Enabled: true,
		Running: true,
		Version: 2,
		SystemRules: []FirewallRuleGroup{{
			Rule: &FirewallRule{Description: marker},
			Chains: []FirewallChain{{
				Name:   "output",
				Table:  "opensnitch",
				Policy: "accept",
				Rules: []FirewallRule{{
					UUID: marker,
					Expressions: []FirewallExpression{{
						Statement: &FirewallStatement{
							Values: []FirewallStatementValue{{Key: "value", Value: marker}},
						},
					}},
				}},
			}},
		}},
	}
}

func TestStoreAddRuleUpdatesStats(t *testing.T) {
	store := NewStore()
	store.SetStats(Stats{NodeID: "node-1"})
	store.AddRule("node-1", Rule{Name: "http"})

	if got := store.snapshot.Stats.Rules; got != 1 {
		t.Fatalf("expected stats count 1, got %d", got)
	}
}

func TestStoreAddPromptBackfillsExpiry(t *testing.T) {
	store := NewStore()
	now := time.Now()
	store.AddPrompt(Prompt{ID: "p1", RequestedAt: now})
	snap := store.Snapshot()
	if len(snap.Prompts) != 1 {
		t.Fatalf("expected one prompt stored, got %d", len(snap.Prompts))
	}
	if snap.Prompts[0].ExpiresAt.IsZero() {
		t.Fatalf("expected prompt expiry to be populated")
	}

	settings := snap.Settings
	settings.PromptTimeout = 45 * time.Second
	store.SetSettings(settings)
	store.AddPrompt(Prompt{ID: "p2", RequestedAt: now})
	snap = store.Snapshot()
	if len(snap.Prompts) != 2 {
		t.Fatalf("expected two prompts stored, got %d", len(snap.Prompts))
	}
	second := snap.Prompts[1]
	delta := second.ExpiresAt.Sub(second.RequestedAt)
	if delta != 45*time.Second {
		t.Fatalf("expected expiry delta 45s, got %s", delta)
	}
}

func TestStoreUpdateRule(t *testing.T) {
	store := NewStore()
	store.SetRules("node-1", []Rule{{Name: "ssh", Enabled: false}})

	updated := store.UpdateRule("node-1", "ssh", func(r *Rule) {
		r.Enabled = true
	})
	if !updated {
		t.Fatal("expected update to succeed")
	}
	if !store.snapshot.Rules["node-1"][0].Enabled {
		t.Fatal("expected rule to be enabled")
	}
	if store.UpdateRule("node-1", "missing", func(_ *Rule) {}) {
		t.Fatal("expected missing rule update to return false")
	}
}

func TestStoreRemoveRule(t *testing.T) {
	store := NewStore()
	store.SetStats(Stats{NodeID: "node-1"})
	store.SetRules("node-1", []Rule{{Name: "ssh"}, {Name: "http"}})

	if !store.RemoveRule("node-1", "ssh") {
		t.Fatal("expected removal to succeed")
	}
	if len(store.snapshot.Rules["node-1"]) != 1 || store.snapshot.Rules["node-1"][0].Name != "http" {
		t.Fatalf("expected remaining rule http, got %#v", store.snapshot.Rules["node-1"])
	}
	if store.RemoveRule("node-1", "ssh") {
		t.Fatal("expected removing missing rule to fail")
	}
	if !store.RemoveRule("node-1", "http") {
		t.Fatal("expected final removal to succeed")
	}
	if _, ok := store.snapshot.Rules["node-1"]; ok {
		t.Fatal("expected node entry to be removed when no rules remain")
	}
	if store.snapshot.Stats.Rules != 0 {
		t.Fatalf("expected stats to drop to 0, got %d", store.snapshot.Stats.Rules)
	}
}

func TestStoreSubscriptionCloseStopsEvents(t *testing.T) {
	store := NewStore()
	sub := store.Subscribe()
	sub.Close()

	if _, ok := <-sub.Events(); ok {
		t.Fatal("expected events channel to be closed after Close")
	}

	store.SetStats(Stats{Rules: 2})
	select {
	case _, ok := <-sub.Events():
		if ok {
			t.Fatal("did not expect events after subscription closed")
		}
	case <-time.After(50 * time.Millisecond):
	}
}
