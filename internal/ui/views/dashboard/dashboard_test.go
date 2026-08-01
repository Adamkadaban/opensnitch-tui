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

	viewtest.AssertSnapshot(t, trimSnapshotPadding(m.View()), filepath.Join("testdata", "dashboard_waiting.snap"))
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
	sizes := []struct {
		width  int
		height int
	}{
		{width: 60, height: 10},
		{width: 80, height: 18},
		{width: 120, height: 18},
		{width: 80, height: 40},
		{width: 120, height: 40},
	}
	for _, preset := range theme.Presets() {
		for _, size := range sizes {
			t.Run(fmt.Sprintf("%s/%dx%d", preset.Name, size.width, size.height), func(t *testing.T) {
				m := New(populatedStore(), theme.New(theme.Options{Name: preset.Name}))
				m.SetSize(size.width, size.height)
				rendered := m.View()
				if got := lipgloss.Width(rendered); got > size.width {
					t.Fatalf("rendered width %d exceeds %d", got, size.width)
				}
				if got := lipgloss.Height(rendered); got != size.height {
					t.Fatalf("rendered height %d, want %d", got, size.height)
				}
			})
		}
	}
}

func TestDashboardShortTerminalContentPriority(t *testing.T) {
	tests := []struct {
		name      string
		width     int
		height    int
		wanted    []string
		notWanted []string
	}{
		{
			name:      "wide short",
			width:     120,
			height:    18,
			wanted:    []string{"Rules", "Connections", "Accepted", "Dropped", "Traffic mix", "2 daemons aggregated", shortTerminalHint},
			notWanted: []string{"Top destinations"},
		},
		{
			name:      "narrow short",
			width:     80,
			height:    18,
			wanted:    []string{"Rules", "Connections", "Accepted", "Dropped", "Traffic mix", "2 daemons aggregated", shortTerminalHint},
			notWanted: []string{"Top destinations"},
		},
		{
			name:      "minimum practical body",
			width:     60,
			height:    10,
			wanted:    []string{"Rules", "Connections", "Accepted", "Dropped", "2 daemons aggregated", shortTerminalHint},
			notWanted: []string{"Traffic mix", "Top destinations"},
		},
		{
			name:      "tall",
			width:     120,
			height:    40,
			wanted:    []string{"Traffic mix", "Top destinations", "five.example", "2 daemons aggregated"},
			notWanted: []string{shortTerminalHint},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := New(populatedStore(), theme.New(theme.Options{}))
			m.SetSize(tc.width, tc.height)
			rendered := m.View()
			for _, wanted := range tc.wanted {
				if !strings.Contains(rendered, wanted) {
					t.Fatalf("expected %q in dashboard:\n%s", wanted, rendered)
				}
			}
			for _, unwanted := range tc.notWanted {
				if strings.Contains(rendered, unwanted) {
					t.Fatalf("did not expect %q in dashboard:\n%s", unwanted, rendered)
				}
			}
		})
	}
}

func populatedStore() *state.Store {
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
	return store
}

func trimSnapshotPadding(value string) string {
	lines := strings.Split(value, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	for idx := range lines {
		lines[idx] = strings.TrimRight(lines[idx], " ")
	}
	return strings.Join(lines, "\n")
}
