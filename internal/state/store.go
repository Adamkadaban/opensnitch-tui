package state

import (
	"sync"
	"time"
)

// Store guards shared application state needed by multiple Bubble Tea models.
type Store struct {
	mu       sync.RWMutex
	snapshot Snapshot
	subs     map[int]*Subscription
	nextSub  int
}

// Subscription delivers notifications when the store mutates.
type Subscription struct {
	id     int
	store  *Store
	events chan struct{}
}

// NewStore creates a state store seeded with default values.
func NewStore() *Store {
	return &Store{
		snapshot: Snapshot{
			ActiveView: ViewDashboard,
			Nodes:      []Node{},
		},
		subs: make(map[int]*Subscription),
	}
}

// Snapshot returns a copy of the current application state.
func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	copySnap := s.snapshot
	copySnap.Nodes = cloneNodes(s.snapshot.Nodes)
	return copySnap
}

// SetNodes replaces the tracked daemon node list.
func (s *Store) SetNodes(nodes []Node) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.snapshot.Nodes = cloneNodes(nodes)
	s.notifyLocked()
}

// UpsertNode inserts or updates a node entry.
func (s *Store) UpsertNode(node Node) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.upsertNodeLocked(node)
	s.notifyLocked()
}

// UpdateNode applies a mutation to an existing node.
func (s *Store) UpdateNode(id string, fn func(*Node)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := s.indexOfLocked(id)
	if idx == -1 {
		return false
	}
	node := s.snapshot.Nodes[idx]
	fn(&node)
	s.snapshot.Nodes[idx] = node
	s.notifyLocked()
	return true
}

// UpdateNodeStatus sets the status/message/last seen for a given node.
func (s *Store) UpdateNodeStatus(id string, status NodeStatus, message string, lastSeen time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := s.indexOfLocked(id)
	if idx == -1 {
		s.snapshot.Nodes = append(s.snapshot.Nodes, Node{
			ID:       id,
			Name:     id,
			Status:   status,
			Message:  message,
			LastSeen: lastSeen,
		})
		return
	}
	node := s.snapshot.Nodes[idx]
	node.Status = status
	if message != "" {
		node.Message = message
	}
	if !lastSeen.IsZero() {
		node.LastSeen = lastSeen
	}
	s.snapshot.Nodes[idx] = node
	s.notifyLocked()
}

// SetActiveView updates the router's active view.
func (s *Store) SetActiveView(kind ViewKind) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.snapshot.ActiveView = kind
	s.notifyLocked()
}

// ActiveView returns the currently selected view.
func (s *Store) ActiveView() ViewKind {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.snapshot.ActiveView
}

// SetStats replaces the cached dashboard statistics.
func (s *Store) SetStats(stats Stats) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.snapshot.Stats = stats
	s.notifyLocked()
}

// SetError records a user-visible error message.
func (s *Store) SetError(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.snapshot.LastError = msg
	s.notifyLocked()
}

// Subscribe returns a subscription that receives a signal whenever the store mutates.
func (s *Store) Subscribe() *Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()

	sub := &Subscription{
		id:     s.nextSub,
		store:  s,
		events: make(chan struct{}, 1),
	}
	s.nextSub++
	s.subs[sub.id] = sub
	return sub
}

func (s *Store) notifyLocked() {
	for _, sub := range s.subs {
		select {
		case sub.events <- struct{}{}:
		default:
		}
	}
}

func (s *Store) removeSubscription(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sub, ok := s.subs[id]; ok {
		delete(s.subs, id)
		close(sub.events)
	}
}

// Events returns a channel that receives a signal for each store mutation.
func (sub *Subscription) Events() <-chan struct{} {
	if sub == nil {
		return nil
	}
	return sub.events
}

// Close stops the subscription and releases associated resources.
func (sub *Subscription) Close() {
	if sub == nil || sub.store == nil {
		return
	}
	sub.store.removeSubscription(sub.id)
	sub.store = nil
}

func cloneNodes(nodes []Node) []Node {
	if len(nodes) == 0 {
		return nil
	}
	copyNodes := make([]Node, len(nodes))
	copy(copyNodes, nodes)
	return copyNodes
}

func (s *Store) upsertNodeLocked(node Node) {
	idx := s.indexOfLocked(node.ID)
	if idx == -1 {
		s.snapshot.Nodes = append(s.snapshot.Nodes, node)
		return
	}
	existing := s.snapshot.Nodes[idx]
	s.snapshot.Nodes[idx] = mergeNodes(existing, node)
}

func (s *Store) indexOfLocked(id string) int {
	for idx, node := range s.snapshot.Nodes {
		if node.ID == id {
			return idx
		}
	}
	return -1
}

func mergeNodes(current, update Node) Node {
	if update.ID == "" {
		update.ID = current.ID
	}
	if update.Name == "" {
		update.Name = current.Name
	}
	if update.Address == "" {
		update.Address = current.Address
	}
	if update.Version == "" {
		update.Version = current.Version
	}
	if update.LastSeen.IsZero() {
		update.LastSeen = current.LastSeen
	}
	if update.Status == "" {
		update.Status = current.Status
	}
	if update.Message == "" {
		update.Message = current.Message
	}
	if !update.FirewallEnabled && current.FirewallEnabled {
		update.FirewallEnabled = true
	}
	return update
}
