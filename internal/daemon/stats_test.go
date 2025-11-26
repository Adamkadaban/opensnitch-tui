package daemon

import (
	"testing"
	"time"

	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
)

func TestConvertStatsNil(t *testing.T) {
	start := time.Now().Add(-time.Second)

	stats := convertStats(nil, "node-1", "primary")

	if stats.NodeID != "node-1" {
		t.Fatalf("expected node id node-1, got %q", stats.NodeID)
	}
	if stats.NodeName != "primary" {
		t.Fatalf("expected node name primary, got %q", stats.NodeName)
	}
	if stats.UpdatedAt.Before(start) {
		t.Fatalf("expected UpdatedAt to be after %s, got %s", start, stats.UpdatedAt)
	}
}

func TestConvertStatsPopulated(t *testing.T) {
	start := time.Now()
	proto := &pb.Statistics{
		DaemonVersion: "1.5.4",
		Rules:         42,
		Connections:   101,
		Accepted:      77,
		Dropped:       3,
		Ignored:       5,
		RuleHits:      88,
		RuleMisses:    13,
		ByHost: map[string]uint64{
			"api.local": 10,
			"db.local":  3,
		},
		ByPort: map[string]uint64{
			"443": 40,
			"80":  5,
		},
		ByExecutable: map[string]uint64{
			"curl": 3,
			"ssh":  7,
		},
	}

	stats := convertStats(proto, "node-2", "backup")

	if stats.DaemonVersion != proto.DaemonVersion {
		t.Fatalf("daemon version mismatch: want %q got %q", proto.DaemonVersion, stats.DaemonVersion)
	}
	if stats.Rules != proto.Rules || stats.Connections != proto.Connections {
		t.Fatalf("counts were not copied: got %+v", stats)
	}
	if stats.Accepted != proto.Accepted || stats.Dropped != proto.Dropped || stats.Ignored != proto.Ignored {
		t.Fatalf("packet counts were not copied: got %+v", stats)
	}
	if stats.RuleHits != proto.RuleHits || stats.RuleMisses != proto.RuleMisses {
		t.Fatalf("rule stats mismatch: got %+v", stats)
	}
	if stats.NodeID != "node-2" || stats.NodeName != "backup" {
		t.Fatalf("node metadata mismatch: %+v", stats)
	}
	if stats.UpdatedAt.Before(start) {
		t.Fatalf("expected UpdatedAt to be after %s, got %s", start, stats.UpdatedAt)
	}
	if len(stats.TopDestHosts) != 2 || stats.TopDestHosts[0].Label != "api.local" {
		t.Fatalf("expected host buckets, got %+v", stats.TopDestHosts)
	}
	if len(stats.TopDestPorts) != 2 || stats.TopDestPorts[0].Label != "443" {
		t.Fatalf("expected ports sorted, got %+v", stats.TopDestPorts)
	}
	if len(stats.TopExecutables) != 2 || stats.TopExecutables[0].Label != "ssh" {
		t.Fatalf("expected executables sorted, got %+v", stats.TopExecutables)
	}
}
