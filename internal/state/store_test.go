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

func TestStoreSetStatsAndError(t *testing.T) {
	store := NewStore()
	stats := Stats{NodeID: "node-1", Rules: 10}
	store.SetStats(stats)
	store.SetError("boom")

	snapshot := store.Snapshot()
	if snapshot.Stats != stats {
		t.Fatalf("expected stats %+v, got %+v", stats, snapshot.Stats)
	}
	if snapshot.LastError != "boom" {
		t.Fatalf("expected last error boom, got %q", snapshot.LastError)
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
	store.AddAlert(Alert{ID: "a1", Text: "one"})
	store.AddAlert(Alert{ID: "a2", Text: "two"})

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

func TestStoreAddRuleUpdatesStats(t *testing.T) {
	store := NewStore()
	store.SetStats(Stats{NodeID: "node-1"})
	store.AddRule("node-1", Rule{Name: "http"})

	if got := store.snapshot.Stats.Rules; got != 1 {
		t.Fatalf("expected stats count 1, got %d", got)
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
	if store.UpdateRule("node-1", "missing", func(r *Rule) {}) {
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
