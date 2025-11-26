package nodes

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/view/viewtest"
)

func TestNodesViewEmptySnapshot(t *testing.T) {
	store := state.NewStore()
	th := theme.New(theme.Options{})
	m := New(store, th)
	m.SetSize(90, 12)

	viewtest.AssertSnapshot(t, m.View(), filepath.Join("testdata", "nodes_empty.snap"))
}

func TestNodesViewPopulatedSnapshot(t *testing.T) {
	store := state.NewStore()
	now := time.Time{}
	store.SetNodes([]state.Node{
		{
			ID:              "tcp://10.0.0.2:50051",
			Name:            "alpha",
			Address:         "10.0.0.2:50051",
			Version:         "1.6.0",
			Message:         "ready",
			Status:          state.NodeStatusReady,
			FirewallEnabled: true,
			LastSeen:        now,
		},
		{
			ID:      "tcp://10.0.0.3:50051",
			Address: "10.0.0.3:50051",
			Message: "dialing",
			Status:  state.NodeStatusConnecting,
		},
	})

	th := theme.New(theme.Options{})
	m := New(store, th)
	m.SetSize(90, 14)

	viewtest.AssertSnapshot(t, m.View(), filepath.Join("testdata", "nodes_populated.snap"))
}
