package dashboard

import (
	"path/filepath"
	"testing"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/view/viewtest"
)

func TestDashboardViewWaitingSnapshot(t *testing.T) {
	store := state.NewStore()
	store.SetStats(state.Stats{})

	th := theme.New(theme.Options{})
	m := New(store, th)
	m.SetSize(120, 18)

	viewtest.AssertSnapshot(t, m.View(), filepath.Join("testdata", "dashboard_waiting.snap"))
}
