package daemon

import (
	"sort"
	"time"

	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
)

func convertStats(stats *pb.Statistics, nodeID, nodeName string) state.Stats {
	if stats == nil {
		return state.Stats{
			NodeID:    nodeID,
			NodeName:  nodeName,
			UpdatedAt: time.Now(),
		}
	}

	return state.Stats{
		NodeID:         nodeID,
		NodeName:       nodeName,
		DaemonVersion:  stats.GetDaemonVersion(),
		Rules:          stats.GetRules(),
		Connections:    stats.GetConnections(),
		Accepted:       stats.GetAccepted(),
		Dropped:        stats.GetDropped(),
		Ignored:        stats.GetIgnored(),
		RuleHits:       stats.GetRuleHits(),
		RuleMisses:     stats.GetRuleMisses(),
		TopDestHosts:   topBuckets(stats.GetByHost(), 5),
		TopDestPorts:   topBuckets(stats.GetByPort(), 5),
		TopExecutables: topBuckets(stats.GetByExecutable(), 5),
		UpdatedAt:      time.Now(),
	}
}

func topBuckets(values map[string]uint64, size int) []state.StatBucket {
	if len(values) == 0 || size <= 0 {
		return nil
	}
	buckets := make([]state.StatBucket, 0, len(values))
	for key, value := range values {
		if value == 0 {
			continue
		}
		buckets = append(buckets, state.StatBucket{Label: key, Value: value})
	}
	if len(buckets) == 0 {
		return nil
	}
	sort.Slice(buckets, func(i, j int) bool {
		if buckets[i].Value == buckets[j].Value {
			return buckets[i].Label < buckets[j].Label
		}
		return buckets[i].Value > buckets[j].Value
	})
	if len(buckets) > size {
		buckets = buckets[:size]
	}
	return buckets
}
