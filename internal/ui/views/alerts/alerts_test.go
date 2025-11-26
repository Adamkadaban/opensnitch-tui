package alerts

import (
	"strings"
	"testing"
	"time"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
)

func TestAlertsViewEmpty(t *testing.T) {
	store := state.NewStore()
	th := theme.New(theme.Options{})
	m := New(store, th)
	m.SetSize(80, 10)

	out := m.View()
	if !strings.Contains(out, "No alerts yet") {
		t.Fatalf("expected empty copy, got %q", out)
	}
}

func TestAlertsViewRendersAlert(t *testing.T) {
	store := state.NewStore()
	store.AddAlert(state.Alert{
		ID:        "1",
		NodeID:    "node-1",
		Text:      "disk full",
		Priority:  "high",
		Type:      "warning",
		Action:    "show_alert",
		CreatedAt: time.Now().Add(-2 * time.Minute),
	})

	th := theme.New(theme.Options{})
	m := New(store, th)
	m.SetSize(80, 10)

	out := m.View()
	if !strings.Contains(out, "disk full") {
		t.Fatalf("expected alert text in view, got %q", out)
	}
	if !strings.Contains(strings.ToUpper(out), "HIGH") {
		t.Fatalf("expected priority label in view, got %q", out)
	}
}
