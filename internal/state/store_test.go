package state

import (
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
