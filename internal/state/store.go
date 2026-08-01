package state

import (
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/adamkadaban/opensnitch-tui/internal/config"
)

// Store guards shared application state needed by multiple Bubble Tea models.
type Store struct {
	mu         sync.RWMutex
	snapshot   Snapshot
	subs       map[int]*Subscription
	nextSub    int
	errorTTL   time.Duration
	errorTimer *time.Timer
}

const (
	maxAlerts              = 100
	maxTopBuckets          = 5
	defaultErrorDisplayTTL = 10 * time.Second
)

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
			ActiveView:      ViewDashboard,
			Nodes:           []Node{},
			StatsByNode:     make(map[string]Stats),
			Rules:           make(map[string][]Rule),
			SystemFirewalls: make(map[string]SystemFirewall),
			NodeConfigs:     make(map[string]NodeConfigState),
			Settings: Settings{
				ThemeName:             config.DefaultThemeName,
				DefaultPromptAction:   config.DefaultPromptAction,
				DefaultPromptDuration: config.DefaultPromptDuration,
				DefaultPromptTarget:   config.DefaultPromptTarget,
				PromptTimeout:         time.Duration(config.DefaultPromptTimeoutSeconds) * time.Second,
				AlertsInterrupt:       config.DefaultAlertsInterrupt,
				PausePromptOnInspect:  config.DefaultPausePromptOnInspect,
				YaraEnabled:           config.DefaultYaraEnabled,
			},
			Prompts: []Prompt{},
		},
		subs:     make(map[int]*Subscription),
		errorTTL: defaultErrorDisplayTTL,
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
	copySnap.SystemFirewalls = cloneFirewallsMap(s.snapshot.SystemFirewalls)
	copySnap.NodeConfigs = cloneNodeConfigsMap(s.snapshot.NodeConfigs)
	copySnap.Settings = s.snapshot.Settings
	copySnap.Stats = cloneStats(s.snapshot.Stats)
	copySnap.StatsByNode = cloneStatsMap(s.snapshot.StatsByNode)
	copySnap.Prompts = clonePrompts(s.snapshot.Prompts)
	return copySnap
}

// SetNodeConfig replaces the preserved daemon configuration for one node.
func (s *Store) SetNodeConfig(nodeID string, config NodeConfigState) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.snapshot.NodeConfigs == nil {
		s.snapshot.NodeConfigs = make(map[string]NodeConfigState)
	}
	s.snapshot.NodeConfigs[nodeID] = cloneNodeConfigState(config)
	s.notifyLocked()
}

// NodeConfig returns a copy of one node's preserved daemon configuration.
func (s *Store) NodeConfig(nodeID string) (NodeConfigState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	config, ok := s.snapshot.NodeConfigs[nodeID]
	if !ok {
		return NodeConfigState{}, false
	}
	return cloneNodeConfigState(config), true
}

// SetNodes replaces the tracked daemon node list.
func (s *Store) SetNodes(nodes []Node) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.snapshot.Nodes = cloneNodes(nodes)
	s.refreshStatsNodeNamesLocked()
	s.aggregateReadyStatsLocked()
	s.notifyLocked()
}

// UpsertNode inserts or updates a node entry.
func (s *Store) UpsertNode(node Node) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.upsertNodeLocked(node)
	s.aggregateReadyStatsLocked()
	s.notifyLocked()
}

// MigrateNodeIdentity merges provisional transport-scoped state into a stable node.
func (s *Store) MigrateNodeIdentity(fromID, toID, nodeName string) {
	if fromID == "" || toID == "" || fromID == toID {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	fromIdx := s.indexOfLocked(fromID)
	toIdx := s.indexOfLocked(toID)
	if fromIdx != -1 {
		migrated := s.snapshot.Nodes[fromIdx]
		migrated.ID = toID
		if nodeName != "" {
			migrated.Name = nodeName
		}
		if toIdx == -1 {
			s.snapshot.Nodes[fromIdx] = migrated
		} else {
			s.snapshot.Nodes[toIdx] = mergeNodes(s.snapshot.Nodes[toIdx], migrated)
			s.snapshot.Nodes = append(s.snapshot.Nodes[:fromIdx], s.snapshot.Nodes[fromIdx+1:]...)
		}
	}

	if stats, ok := s.snapshot.StatsByNode[fromID]; ok {
		stats.NodeID = toID
		if nodeName != "" {
			stats.NodeName = nodeName
		}
		migrateEvents(stats.Events, fromID, toID)
		if current, exists := s.snapshot.StatsByNode[toID]; exists {
			stats.Events = mergeEvents(stats.Events, current.Events, maxEvents)
			if current.UpdatedAt.After(stats.UpdatedAt) {
				current.Events = stats.Events
				stats = current
			}
		}
		s.snapshot.StatsByNode[toID] = stats
		delete(s.snapshot.StatsByNode, fromID)
	}
	migrateEvents(s.snapshot.Stats.Events, fromID, toID)

	if rules, ok := s.snapshot.Rules[fromID]; ok {
		for i := range rules {
			rules[i].NodeID = toID
		}
		current := s.snapshot.Rules[toID]
		indexes := make(map[string]int, len(current))
		for i := range current {
			current[i].NodeID = toID
			indexes[current[i].Name] = i
		}
		for _, rule := range rules {
			if index, exists := indexes[rule.Name]; exists {
				current[index] = rule
				continue
			}
			indexes[rule.Name] = len(current)
			current = append(current, rule)
		}
		s.snapshot.Rules[toID] = current
		delete(s.snapshot.Rules, fromID)
		s.syncRuleCountLocked(toID)
	}
	if firewall, ok := s.snapshot.SystemFirewalls[fromID]; ok {
		firewall.NodeID = toID
		s.snapshot.SystemFirewalls[toID] = firewall
		delete(s.snapshot.SystemFirewalls, fromID)
	}
	if config, ok := s.snapshot.NodeConfigs[fromID]; ok {
		s.snapshot.NodeConfigs[toID] = config
		delete(s.snapshot.NodeConfigs, fromID)
	}
	for i := range s.snapshot.Prompts {
		if s.snapshot.Prompts[i].NodeID == fromID {
			s.snapshot.Prompts[i].NodeID = toID
			s.snapshot.Prompts[i].NodeName = nodeName
		}
	}
	for i := range s.snapshot.Alerts {
		if s.snapshot.Alerts[i].NodeID == fromID {
			s.snapshot.Alerts[i].NodeID = toID
			if s.snapshot.Alerts[i].Rule != nil {
				s.snapshot.Alerts[i].Rule.NodeID = toID
			}
		}
	}

	s.refreshStatsNodeNameLocked(toID, nodeName)
	s.aggregateReadyStatsLocked()
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
	s.refreshStatsNodeNameLocked(node.ID, node.Name)
	s.aggregateReadyStatsLocked()
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
		s.aggregateReadyStatsLocked()
		s.notifyLocked()
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
	s.aggregateReadyStatsLocked()
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

// SetStats replaces one node's cached dashboard statistics.
func (s *Store) SetStats(stats Stats) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.snapshot.StatsByNode == nil {
		s.snapshot.StatsByNode = make(map[string]Stats)
	}
	if stats.NodeID != "" {
		s.snapshot.StatsByNode[stats.NodeID] = cloneStats(stats)
	}
	s.snapshot.Stats.Events = mergeEvents(s.snapshot.Stats.Events, stats.Events, maxEvents)
	s.aggregateReadyStatsLocked()
	s.notifyLocked()
}

const maxEvents = 200

func mergeEvents(old, incoming []Event, limit int) []Event {
	if limit <= 0 {
		limit = maxEvents
	}
	merged := make([]Event, 0, len(old)+len(incoming))
	seen := make(map[string]struct{}, len(old)+len(incoming))

	appendUnique := func(ev Event) {
		key := eventKey(ev)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		merged = append(merged, ev)
	}

	for _, ev := range incoming {
		appendUnique(ev)
	}
	for _, ev := range old {
		appendUnique(ev)
	}

	// Sort by time, newest first
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].UnixNano > merged[j].UnixNano
	})

	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged
}

func eventKey(ev Event) string {
	return fmt.Sprintf("%s|%d|%s|%s|%s|%s|%d", ev.NodeID, ev.UnixNano, ev.Time, ev.Rule.Name, ev.Connection.DstHost, ev.Connection.DstIP, ev.Connection.DstPort)
}

func migrateEvents(events []Event, fromID, toID string) {
	for i := range events {
		if events[i].NodeID == fromID {
			events[i].NodeID = toID
		}
		if events[i].Rule.NodeID == fromID {
			events[i].Rule.NodeID = toID
		}
	}
}

func (s *Store) aggregateReadyStatsLocked() {
	events := s.snapshot.Stats.Events
	ready := make([]Node, 0, len(s.snapshot.Nodes))
	seen := make(map[string]struct{}, len(s.snapshot.Nodes))
	for _, node := range s.snapshot.Nodes {
		if node.Status != NodeStatusReady {
			continue
		}
		if _, ok := seen[node.ID]; ok {
			continue
		}
		seen[node.ID] = struct{}{}
		ready = append(ready, node)
	}

	aggregate := Stats{}
	if len(ready) == 1 {
		if stats, ok := s.snapshot.StatsByNode[ready[0].ID]; ok {
			aggregate = cloneStats(stats)
		}
	} else {
		for _, node := range ready {
			stats, ok := s.snapshot.StatsByNode[node.ID]
			if !ok {
				continue
			}
			aggregate.Rules += stats.Rules
			aggregate.Connections += stats.Connections
			aggregate.Accepted += stats.Accepted
			aggregate.Dropped += stats.Dropped
			aggregate.Ignored += stats.Ignored
			aggregate.RuleHits += stats.RuleHits
			aggregate.RuleMisses += stats.RuleMisses
			aggregate.TopDestHosts = mergeStatBuckets(aggregate.TopDestHosts, stats.TopDestHosts, 0)
			aggregate.TopDestPorts = mergeStatBuckets(aggregate.TopDestPorts, stats.TopDestPorts, 0)
			aggregate.TopExecutables = mergeStatBuckets(aggregate.TopExecutables, stats.TopExecutables, 0)
			aggregate.TopUsers = mergeStatBuckets(aggregate.TopUsers, stats.TopUsers, 0)
			if stats.UpdatedAt.After(aggregate.UpdatedAt) {
				aggregate.UpdatedAt = stats.UpdatedAt
			}
		}
		if len(ready) > 1 {
			aggregate.TopDestHosts = mergeStatBuckets(aggregate.TopDestHosts, nil, maxTopBuckets)
			aggregate.TopDestPorts = mergeStatBuckets(aggregate.TopDestPorts, nil, maxTopBuckets)
			aggregate.TopExecutables = mergeStatBuckets(aggregate.TopExecutables, nil, maxTopBuckets)
			aggregate.TopUsers = mergeStatBuckets(aggregate.TopUsers, nil, maxTopBuckets)
			aggregate.NodeName = fmt.Sprintf("Aggregated telemetry (%d ready daemons)", len(ready))
			aggregate.DaemonVersion = "multiple daemons"
		}
	}
	aggregate.Events = cloneEvents(events)
	s.snapshot.Stats = aggregate
}

func mergeStatBuckets(current, incoming []StatBucket, limit int) []StatBucket {
	values := make(map[string]uint64, len(current)+len(incoming))
	for _, bucket := range current {
		values[bucket.Label] += bucket.Value
	}
	for _, bucket := range incoming {
		values[bucket.Label] += bucket.Value
	}
	buckets := make([]StatBucket, 0, len(values))
	for label, value := range values {
		if value == 0 {
			continue
		}
		buckets = append(buckets, StatBucket{Label: label, Value: value})
	}
	sort.Slice(buckets, func(i, j int) bool {
		if buckets[i].Value == buckets[j].Value {
			return buckets[i].Label < buckets[j].Label
		}
		return buckets[i].Value > buckets[j].Value
	})
	if limit > 0 && len(buckets) > limit {
		buckets = buckets[:limit]
	}
	return buckets
}

// SetError records a user-visible error message.
func (s *Store) SetError(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.errorTimer != nil {
		s.errorTimer.Stop()
	}
	issuedAt := time.Now()
	s.snapshot.LastError = msg
	s.snapshot.LastErrorAt = issuedAt
	s.notifyLocked()
	s.errorTimer = time.AfterFunc(s.errorTTL, func() {
		s.expireError(issuedAt)
	})
}

// ClearError removes the currently displayed error message, if any.
func (s *Store) ClearError() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.errorTimer != nil {
		s.errorTimer.Stop()
		s.errorTimer = nil
	}
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

// ApplyRules replaces same-name rules and appends new rules for one node.
func (s *Store) ApplyRules(nodeID string, incoming []Rule) {
	if len(incoming) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.snapshot.Rules == nil {
		s.snapshot.Rules = make(map[string][]Rule)
	}
	replacements := make(map[string]Rule, len(incoming))
	order := make([]string, 0, len(incoming))
	for _, rule := range incoming {
		rule.NodeID = nodeID
		if _, exists := replacements[rule.Name]; !exists {
			order = append(order, rule.Name)
		}
		replacements[rule.Name] = cloneRule(rule)
	}

	current := s.snapshot.Rules[nodeID]
	merged := make([]Rule, 0, len(current)+len(incoming))
	for _, rule := range current {
		replacement, ok := replacements[rule.Name]
		if !ok {
			merged = append(merged, cloneRule(rule))
			continue
		}
		merged = append(merged, replacement)
		delete(replacements, rule.Name)
	}
	for _, name := range order {
		if rule, ok := replacements[name]; ok {
			merged = append(merged, rule)
		}
	}
	s.snapshot.Rules[nodeID] = merged
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

// SetSystemFirewall replaces the system firewall state for a node.
func (s *Store) SetSystemFirewall(nodeID string, firewall SystemFirewall) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.snapshot.SystemFirewalls == nil {
		s.snapshot.SystemFirewalls = make(map[string]SystemFirewall)
	}
	firewall.NodeID = nodeID
	s.snapshot.SystemFirewalls[nodeID] = cloneSystemFirewall(firewall)
	s.notifyLocked()
}

// SystemFirewall returns the system firewall state for a node.
func (s *Store) SystemFirewall(nodeID string) (SystemFirewall, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	firewall, ok := s.snapshot.SystemFirewalls[nodeID]
	if !ok {
		return SystemFirewall{}, false
	}
	return cloneSystemFirewall(firewall), true
}

// UpdateSystemFirewall mutates the system firewall state for a node.
func (s *Store) UpdateSystemFirewall(nodeID string, fn func(*SystemFirewall)) bool {
	if fn == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	firewall, ok := s.snapshot.SystemFirewalls[nodeID]
	if !ok {
		return false
	}
	firewall = cloneSystemFirewall(firewall)
	fn(&firewall)
	firewall.NodeID = nodeID
	s.snapshot.SystemFirewalls[nodeID] = cloneSystemFirewall(firewall)
	s.notifyLocked()
	return true
}

// RemoveSystemFirewall removes the system firewall state for a node.
func (s *Store) RemoveSystemFirewall(nodeID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.snapshot.SystemFirewalls[nodeID]; !ok {
		return false
	}
	delete(s.snapshot.SystemFirewalls, nodeID)
	s.notifyLocked()
	return true
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

	s.snapshot.Alerts = append([]Alert{cloneAlert(alert)}, s.snapshot.Alerts...)
	if len(s.snapshot.Alerts) > maxAlerts {
		s.snapshot.Alerts = s.snapshot.Alerts[:maxAlerts]
	}
	s.notifyLocked()
}

// DeleteAlert removes the matching local alert.
func (s *Store) DeleteAlert(selected Alert) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for idx, alert := range s.snapshot.Alerts {
		if !reflect.DeepEqual(alert, selected) {
			continue
		}
		s.snapshot.Alerts = append(s.snapshot.Alerts[:idx], s.snapshot.Alerts[idx+1:]...)
		s.notifyLocked()
		return true
	}
	return false
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
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.snapshot.LastError == "" {
		return
	}
	if !s.snapshot.LastErrorAt.Equal(issuedAt) {
		return
	}
	s.errorTimer = nil
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
	for i, alert := range alerts {
		copyAlerts[i] = cloneAlert(alert)
	}
	return copyAlerts
}

func cloneAlert(alert Alert) Alert {
	if alert.Process != nil {
		process := cloneProcess(*alert.Process)
		alert.Process = &process
	}
	if alert.Connection != nil {
		connection := cloneConnection(*alert.Connection)
		alert.Connection = &connection
	}
	if alert.Rule != nil {
		rule := cloneRule(*alert.Rule)
		alert.Rule = &rule
	}
	if alert.FirewallRule != nil {
		rule := cloneFirewallRule(*alert.FirewallRule)
		alert.FirewallRule = &rule
	}
	return alert
}

func cloneProcess(process Process) Process {
	process.Args = cloneStrings(process.Args)
	process.Env = cloneStringMap(process.Env)
	process.Checksums = cloneStringMap(process.Checksums)
	if len(process.ProcessTree) > 0 {
		process.ProcessTree = append([]ProcessTreeEntry(nil), process.ProcessTree...)
	} else {
		process.ProcessTree = nil
	}
	return process
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

func cloneFirewallsMap(firewalls map[string]SystemFirewall) map[string]SystemFirewall {
	if len(firewalls) == 0 {
		return nil
	}
	copyMap := make(map[string]SystemFirewall, len(firewalls))
	for nodeID, firewall := range firewalls {
		copyMap[nodeID] = cloneSystemFirewall(firewall)
	}
	return copyMap
}

func cloneNodeConfigsMap(configs map[string]NodeConfigState) map[string]NodeConfigState {
	if len(configs) == 0 {
		return nil
	}
	copyMap := make(map[string]NodeConfigState, len(configs))
	for nodeID, config := range configs {
		copyMap[nodeID] = cloneNodeConfigState(config)
	}
	return copyMap
}

func cloneNodeConfigState(config NodeConfigState) NodeConfigState {
	return config
}

func cloneSystemFirewall(firewall SystemFirewall) SystemFirewall {
	if len(firewall.SystemRules) == 0 {
		firewall.SystemRules = nil
		return firewall
	}
	groups := make([]FirewallRuleGroup, len(firewall.SystemRules))
	for i, group := range firewall.SystemRules {
		groups[i] = cloneFirewallRuleGroup(group)
	}
	firewall.SystemRules = groups
	return firewall
}

func cloneFirewallRuleGroup(group FirewallRuleGroup) FirewallRuleGroup {
	if group.Rule != nil {
		rule := cloneFirewallRule(*group.Rule)
		group.Rule = &rule
	}
	if len(group.Chains) == 0 {
		group.Chains = nil
		return group
	}
	chains := make([]FirewallChain, len(group.Chains))
	for i, chain := range group.Chains {
		chains[i] = cloneFirewallChain(chain)
	}
	group.Chains = chains
	return group
}

func cloneFirewallChain(chain FirewallChain) FirewallChain {
	if len(chain.Rules) == 0 {
		chain.Rules = nil
		return chain
	}
	rules := make([]FirewallRule, len(chain.Rules))
	for i, rule := range chain.Rules {
		rules[i] = cloneFirewallRule(rule)
	}
	chain.Rules = rules
	return chain
}

func cloneFirewallRule(rule FirewallRule) FirewallRule {
	if len(rule.Expressions) == 0 {
		rule.Expressions = nil
		return rule
	}
	expressions := make([]FirewallExpression, len(rule.Expressions))
	for i, expression := range rule.Expressions {
		expressions[i] = cloneFirewallExpression(expression)
	}
	rule.Expressions = expressions
	return rule
}

func cloneFirewallExpression(expression FirewallExpression) FirewallExpression {
	if expression.Statement == nil {
		return expression
	}
	statement := *expression.Statement
	if len(statement.Values) == 0 {
		statement.Values = nil
	} else {
		values := make([]FirewallStatementValue, len(statement.Values))
		copy(values, statement.Values)
		statement.Values = values
	}
	expression.Statement = &statement
	return expression
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
	stats.TopUsers = cloneBuckets(stats.TopUsers)
	stats.Events = cloneEvents(stats.Events)
	return stats
}

func cloneStatsMap(statsByNode map[string]Stats) map[string]Stats {
	if len(statsByNode) == 0 {
		return nil
	}
	copyMap := make(map[string]Stats, len(statsByNode))
	for nodeID, stats := range statsByNode {
		copyMap[nodeID] = cloneStats(stats)
	}
	return copyMap
}

func cloneEvents(events []Event) []Event {
	if len(events) == 0 {
		return nil
	}
	copyEvents := make([]Event, len(events))
	for i, event := range events {
		event.Connection = cloneConnection(event.Connection)
		event.Rule = cloneRule(event.Rule)
		copyEvents[i] = event
	}
	return copyEvents
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
	conn.ProcessArgs = cloneStrings(conn.ProcessArgs)
	conn.ProcessEnv = cloneStringMap(conn.ProcessEnv)
	conn.ProcessChecksums = cloneStringMap(conn.ProcessChecksums)
	if len(conn.ProcessTree) > 0 {
		conn.ProcessTree = append([]ProcessTreeEntry(nil), conn.ProcessTree...)
	} else {
		conn.ProcessTree = nil
	}
	return conn
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values...)
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func (s *Store) upsertNodeLocked(node Node) {
	idx := s.indexOfLocked(node.ID)
	if idx == -1 {
		s.snapshot.Nodes = append(s.snapshot.Nodes, node)
		s.refreshStatsNodeNameLocked(node.ID, node.Name)
		return
	}
	existing := s.snapshot.Nodes[idx]
	merged := mergeNodes(existing, node)
	s.snapshot.Nodes[idx] = merged
	s.refreshStatsNodeNameLocked(merged.ID, merged.Name)
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
	stats, ok := s.snapshot.StatsByNode[nodeID]
	if !ok {
		return
	}
	stats.Rules = uint64(len(s.snapshot.Rules[nodeID]))
	s.snapshot.StatsByNode[nodeID] = stats
	s.aggregateReadyStatsLocked()
}

func (s *Store) refreshStatsNodeNamesLocked() {
	for _, node := range s.snapshot.Nodes {
		s.refreshStatsNodeNameLocked(node.ID, node.Name)
	}
}

func (s *Store) refreshStatsNodeNameLocked(nodeID, nodeName string) {
	if nodeName == "" {
		return
	}
	stats, ok := s.snapshot.StatsByNode[nodeID]
	if !ok {
		return
	}
	stats.NodeName = nodeName
	s.snapshot.StatsByNode[nodeID] = stats
}
