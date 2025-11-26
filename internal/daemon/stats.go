package daemon

import (
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
		NodeID:        nodeID,
		NodeName:      nodeName,
		DaemonVersion: stats.GetDaemonVersion(),
		Rules:         stats.GetRules(),
		Connections:   stats.GetConnections(),
		Accepted:      stats.GetAccepted(),
		Dropped:       stats.GetDropped(),
		Ignored:       stats.GetIgnored(),
		RuleHits:      stats.GetRuleHits(),
		RuleMisses:    stats.GetRuleMisses(),
		UpdatedAt:     time.Now(),
	}
}
