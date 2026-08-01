package dashboard

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/view/viewtest"
	"github.com/charmbracelet/lipgloss"
)

func TestDashboardViewWaitingSnapshot(t *testing.T) {
	store := state.NewStore()
	store.SetStats(state.Stats{})

	th := theme.New(theme.Options{})
	m := New(store, th)
	m.SetSize(120, 18)

	viewtest.AssertSnapshot(t, m.View(), filepath.Join("testdata", "dashboard_waiting.snap"))
}

func TestTrimToWidth(t *testing.T) {
	cases := []struct {
		name   string
		value  string
		width  int
		expect string
	}{
		{"zero width", "hello", 0, ""},
		{"short", "hi", 5, "hi"},
		{"width 1", "hello", 1, "h"},
		{"width 3", "hello", 3, "hel"},
		{"width 4", "hello", 4, "h..."},
		{"unicode", "héllo", 4, "h..."},
	}

	for _, tc := range cases {
		got := trimToWidth(tc.value, tc.width)
		if got != tc.expect {
			t.Fatalf("%s: expected %q, got %q", tc.name, tc.expect, got)
		}
		if runeCount := len([]rune(got)); runeCount > tc.width {
			t.Fatalf("%s: result rune length %d exceeds width %d", tc.name, runeCount, tc.width)
		}
	}
}

func TestCardColumns(t *testing.T) {
	cases := []struct {
		width   int
		minimum int
		expect  int
	}{
		{width: 120, minimum: 28, expect: 4},
		{width: 80, minimum: 28, expect: 2},
		{width: 55, minimum: 28, expect: 1},
	}

	for _, tc := range cases {
		if got := cardColumns(tc.width, tc.minimum); got != tc.expect {
			t.Fatalf("cardColumns(%d, %d) = %d, want %d", tc.width, tc.minimum, got, tc.expect)
		}
	}
}

func TestDashboardMetaReadyNodes(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		nodes     []state.Node
		stats     []state.Stats
		want      string
		notWanted []string
	}{
		{
			name: "zero ready",
			nodes: []state.Node{{
				ID: "node-1", Name: "alpha", Status: state.NodeStatusDisconnected,
			}},
			want: "No ready daemons · Telemetry paused until reconnect",
		},
		{
			name: "one ready",
			nodes: []state.Node{{
				ID: "node-1", Name: "alpha", Version: "1.6.0", Status: state.NodeStatusReady,
			}},
			stats: []state.Stats{{
				NodeID: "node-1", NodeName: "alpha", DaemonVersion: "1.6.0", UpdatedAt: now,
			}},
			want: "Node alpha · Daemon 1.6.0 · Updated",
		},
		{
			name: "multiple ready",
			nodes: []state.Node{
				{ID: "node-1", Name: "alpha", Status: state.NodeStatusReady},
				{ID: "node-2", Name: "beta", Status: state.NodeStatusReady},
			},
			stats: []state.Stats{
				{NodeID: "node-1", NodeName: "alpha", DaemonVersion: "1.6.0", UpdatedAt: now},
				{NodeID: "node-2", NodeName: "beta", DaemonVersion: "1.7.0", UpdatedAt: now},
			},
			want:      "2 daemons aggregated · Updated",
			notWanted: []string{"alpha", "beta", "1.6.0", "1.7.0"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := state.NewStore()
			store.SetNodes(tc.nodes)
			for _, stats := range tc.stats {
				store.SetStats(stats)
			}
			m := New(store, theme.New(theme.Options{})).(*Model)
			got := m.metaLine(store.Snapshot())
			if !strings.Contains(got, tc.want) {
				t.Fatalf("expected %q in %q", tc.want, got)
			}
			for _, unwanted := range tc.notWanted {
				if strings.Contains(got, unwanted) {
					t.Fatalf("did not expect %q in %q", unwanted, got)
				}
			}
		})
	}
}

func TestDashboardRenderingStaysWithinTerminalBounds(t *testing.T) {
	for _, size := range []struct {
		width  int
		height int
	}{
		{width: 80, height: 40},
		{width: 120, height: 40},
	} {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			store := state.NewStore()
			store.SetNodes([]state.Node{
				{ID: "node-1", Name: "alpha", Status: state.NodeStatusReady},
				{ID: "node-2", Name: "beta", Status: state.NodeStatusReady},
			})
			buckets := []state.StatBucket{
				{Label: "one.example", Value: 10},
				{Label: "two.example", Value: 9},
				{Label: "three.example", Value: 8},
				{Label: "four.example", Value: 7},
				{Label: "five.example", Value: 6},
			}
			for _, nodeID := range []string{"node-1", "node-2"} {
				store.SetStats(state.Stats{
					NodeID: nodeID, Connections: 30, Accepted: 20, Dropped: 5, Ignored: 5,
					TopDestHosts: buckets, TopDestPorts: buckets, TopExecutables: buckets,
					TopUsers: buckets, UpdatedAt: time.Now(),
				})
			}

			m := New(store, theme.New(theme.Options{}))
			m.SetSize(size.width, size.height)
			rendered := m.View()
			if got := lipgloss.Width(rendered); got > size.width {
				t.Fatalf("rendered width %d exceeds %d", got, size.width)
			}
			if got := lipgloss.Height(rendered); got > size.height {
				t.Fatalf("rendered height %d exceeds %d", got, size.height)
			}
		})
	}
}
