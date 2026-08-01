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
