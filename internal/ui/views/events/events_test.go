package events

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/view/viewtest"
)

func TestEventsSnapshot(t *testing.T) {
	store := state.NewStore()
	now := time.Unix(1700000000, 0)

	events := []state.Event{
		{
			NodeID:   "node-1",
			Time:     now.Format(time.RFC3339),
			UnixNano: now.UnixNano(),
			Connection: state.Connection{
				DstIP:       "1.2.3.4",
				DstHost:     "example.com",
				Protocol:    "tcp",
				ProcessPath: "/usr/bin/curl",
				ProcessArgs: []string{"curl", "https://example.com"},
			},
			Rule: state.Rule{Name: "allow-curl", Action: "allow", Enabled: true},
		},
		{
			NodeID:   "node-1",
			Time:     now.Add(-time.Minute).Format(time.RFC3339),
			UnixNano: now.Add(-time.Minute).UnixNano(),
			Connection: state.Connection{
				DstIP:       "5.6.7.8",
				DstHost:     "example.org",
				Protocol:    "udp",
				ProcessPath: "/usr/bin/dig",
				ProcessArgs: []string{"dig", "example.org"},
			},
			Rule: state.Rule{Name: "deny-dns", Action: "deny", Enabled: false},
		},
	}

	stats := state.Stats{Events: events}
	store.SetStats(stats)

	th := theme.New(theme.Options{})
	m := New(store, th)
	m.SetSize(100, 20)

	viewtest.AssertSnapshot(t, m.View(), filepath.Join("testdata", "events.snap"))
}
