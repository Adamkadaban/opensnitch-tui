package state

import (
	"sync"
	"time"

	"github.com/adamkadaban/opensnitch-tui/internal/config"
)

// Store guards shared application state needed by multiple Bubble Tea models.
type Store struct {
	mu       sync.RWMutex
	snapshot Snapshot
	subs     map[int]*Subscription
	nextSub  int
}

const maxAlerts = 100

var errorDisplayTTL = 10 * time.Second

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
			Rules:      make(map[string][]Rule),
			Settings: Settings{
				DefaultPromptAction:   config.DefaultPromptAction,
				DefaultPromptDuration: config.DefaultPromptDuration,
				DefaultPromptTarget:   config.DefaultPromptTarget,
				PromptTimeout:         time.Duration(config.DefaultPromptTimeoutSeconds) * time.Second,
				AlertsInterrupt:       config.DefaultAlertsInterrupt,
			},
			Prompts: []Prompt{},
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
	copySnap.Alerts = cloneAlerts(s.snapshot.Alerts)
	copySnap.Rules = cloneRulesMap(s.snapshot.Rules)
	copySnap.Settings = s.snapshot.Settings
	copySnap.Stats = cloneStats(s.snapshot.Stats)
	copySnap.Prompts = clonePrompts(s.snapshot.Prompts)
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

	s.snapshot.Stats = cloneStats(stats)
	s.notifyLocked()
}

// SetError records a user-visible error message.
func (s *Store) SetError(msg string) {
	s.mu.Lock()
	issuedAt := time.Now()
	s.snapshot.LastError = msg
	s.snapshot.LastErrorAt = issuedAt
	s.notifyLocked()
	s.mu.Unlock()

	go s.expireError(issuedAt)
}

// ClearError removes the currently displayed error message, if any.
func (s *Store) ClearError() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.snapshot.LastError == "" {
		return
	}
	s.snapshot.LastError = ""
	s.snapshot.LastErrorAt = time.Time{}
	s.notifyLocked()
}

// SetRules replaces the rule list for a node.
func (s *Store) SetRules(nodeID string, rules []Rule) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.snapshot.Rules == nil {
		s.snapshot.Rules = make(map[string][]Rule)
	}
	s.snapshot.Rules[nodeID] = cloneRuleSlice(rules)
	s.syncRuleCountLocked(nodeID)
	s.notifyLocked()
}

// AddRule appends a rule entry for the specified node.
func (s *Store) AddRule(nodeID string, rule Rule) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.snapshot.Rules == nil {
		s.snapshot.Rules = make(map[string][]Rule)
	}
	rule.NodeID = nodeID
	s.snapshot.Rules[nodeID] = append(s.snapshot.Rules[nodeID], cloneRule(rule))
	s.syncRuleCountLocked(nodeID)
	s.notifyLocked()
}

func (s *Store) UpdateRule(nodeID, ruleName string, fn func(*Rule)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.updateRuleLocked(nodeID, ruleName, fn)
}

func (s *Store) RemoveRule(nodeID, ruleName string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	list, ok := s.snapshot.Rules[nodeID]
	if !ok {
		return false
	}
	for idx, rule := range list {
		if rule.Name != ruleName {
			continue
		}
		list = append(list[:idx], list[idx+1:]...)
		if len(list) == 0 {
			delete(s.snapshot.Rules, nodeID)
		} else {
			s.snapshot.Rules[nodeID] = list
		}
		s.syncRuleCountLocked(nodeID)
		s.notifyLocked()
		return true
	}
	return false
}

// AddPrompt enqueues a pending connection prompt.
func (s *Store) AddPrompt(prompt Prompt) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if prompt.RequestedAt.IsZero() {
		prompt.RequestedAt = time.Now()
	}
	if prompt.ExpiresAt.IsZero() {
		timeout := s.snapshot.Settings.PromptTimeout
		if timeout <= 0 {
			timeout = time.Duration(config.DefaultPromptTimeoutSeconds) * time.Second
		}
		prompt.ExpiresAt = prompt.RequestedAt.Add(timeout)
	}
	s.snapshot.Prompts = append(s.snapshot.Prompts, clonePrompt(prompt))
	s.notifyLocked()
}

// UpdatePrompt mutates a prompt by ID.
func (s *Store) UpdatePrompt(id string, fn func(*Prompt)) bool {
	if fn == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for idx, prompt := range s.snapshot.Prompts {
		if prompt.ID != id {
			continue
		}
		fn(&prompt)
		s.snapshot.Prompts[idx] = prompt
		s.notifyLocked()
		return true
	}
	return false
}

// RemovePrompt drops a prompt by ID.
func (s *Store) RemovePrompt(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for idx, prompt := range s.snapshot.Prompts {
		if prompt.ID != id {
			continue
		}
		s.snapshot.Prompts = append(s.snapshot.Prompts[:idx], s.snapshot.Prompts[idx+1:]...)
		s.notifyLocked()
		return true
	}
	return false
}

// SetSettings replaces the persisted settings snapshot.
func (s *Store) SetSettings(settings Settings) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.snapshot.Settings = settings
	s.notifyLocked()
}

// AddAlert prepends an alert to the rolling history.
func (s *Store) AddAlert(alert Alert) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.snapshot.Alerts = append([]Alert{alert}, s.snapshot.Alerts...)
	if len(s.snapshot.Alerts) > maxAlerts {
		s.snapshot.Alerts = s.snapshot.Alerts[:maxAlerts]
	}
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

func (s *Store) expireError(issuedAt time.Time) {
	timer := time.NewTimer(errorDisplayTTL)
	defer timer.Stop()
	<-timer.C

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.snapshot.LastError == "" {
		return
	}
	if !s.snapshot.LastErrorAt.Equal(issuedAt) {
		return
	}
	s.snapshot.LastError = ""
	s.snapshot.LastErrorAt = time.Time{}
	s.notifyLocked()
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

func cloneAlerts(alerts []Alert) []Alert {
	if len(alerts) == 0 {
		return nil
	}
	copyAlerts := make([]Alert, len(alerts))
	copy(copyAlerts, alerts)
	return copyAlerts
}

func cloneRulesMap(rules map[string][]Rule) map[string][]Rule {
	if len(rules) == 0 {
		return nil
	}
	copyMap := make(map[string][]Rule, len(rules))
	for nodeID, list := range rules {
		copyMap[nodeID] = cloneRuleSlice(list)
	}
	return copyMap
}

func clonePrompts(prompts []Prompt) []Prompt {
	if len(prompts) == 0 {
		return nil
	}
	copyPrompts := make([]Prompt, len(prompts))
	for i, prompt := range prompts {
		copyPrompts[i] = clonePrompt(prompt)
	}
	return copyPrompts
}

func cloneStats(stats Stats) Stats {
	stats.TopDestHosts = cloneBuckets(stats.TopDestHosts)
	stats.TopDestPorts = cloneBuckets(stats.TopDestPorts)
	stats.TopExecutables = cloneBuckets(stats.TopExecutables)
	return stats
}

func cloneBuckets(buckets []StatBucket) []StatBucket {
	if len(buckets) == 0 {
		return nil
	}
	copyBuckets := make([]StatBucket, len(buckets))
	copy(copyBuckets, buckets)
	return copyBuckets
}

func cloneRuleSlice(list []Rule) []Rule {
	if len(list) == 0 {
		return nil
	}
	copyRules := make([]Rule, len(list))
	for i, rule := range list {
		copyRules[i] = cloneRule(rule)
	}
	return copyRules
}

func cloneRule(rule Rule) Rule {
	rule.Operator = cloneRuleOperator(rule.Operator)
	return rule
}

func cloneRuleOperator(op RuleOperator) RuleOperator {
	if len(op.Children) == 0 {
		op.Children = nil
		return op
	}
	children := make([]RuleOperator, len(op.Children))
	for i, child := range op.Children {
		children[i] = cloneRuleOperator(child)
	}
	op.Children = children
	return op
}

func clonePrompt(prompt Prompt) Prompt {
	prompt.Connection = cloneConnection(prompt.Connection)
	return prompt
}

func cloneConnection(conn Connection) Connection {
	if len(conn.ProcessArgs) > 0 {
		args := make([]string, len(conn.ProcessArgs))
		copy(args, conn.ProcessArgs)
		conn.ProcessArgs = args
	}
	if len(conn.ProcessChecksums) > 0 {
		checksums := make(map[string]string, len(conn.ProcessChecksums))
		for key, value := range conn.ProcessChecksums {
			checksums[key] = value
		}
		conn.ProcessChecksums = checksums
	}
	return conn
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

func (s *Store) updateRuleLocked(nodeID, ruleName string, fn func(*Rule)) bool {
	if fn == nil {
		return false
	}
	list, ok := s.snapshot.Rules[nodeID]
	if !ok {
		return false
	}
	for idx, rule := range list {
		if rule.Name != ruleName {
			continue
		}
		fn(&rule)
		list[idx] = rule
		s.snapshot.Rules[nodeID] = list
		s.syncRuleCountLocked(nodeID)
		s.notifyLocked()
		return true
	}
	return false
}

func (s *Store) syncRuleCountLocked(nodeID string) {
	if nodeID == "" {
		return
	}
	if s.snapshot.Stats.NodeID != nodeID {
		return
	}
	s.snapshot.Stats.Rules = uint64(len(s.snapshot.Rules[nodeID]))
}
