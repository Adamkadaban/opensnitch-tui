package root

import (
	"strings"
	"testing"

	"github.com/adamkadaban/opensnitch-tui/internal/keymap"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
)

func TestFooterLineIncludesError(t *testing.T) {
	th := theme.New(theme.Options{})
	km := keymap.DefaultGlobal()

	model := &Model{keymap: km, theme: th}
	snapshot := state.Snapshot{ActiveView: state.ViewDashboard, Nodes: []state.Node{{}}, LastError: "boom"}

	line := model.footerLine(snapshot)
	if !strings.Contains(line, "boom") {
		t.Fatalf("expected footer to include error text, got %q", line)
	}
}

func TestFooterLineWithoutError(t *testing.T) {
	th := theme.New(theme.Options{})
	km := keymap.DefaultGlobal()

	model := &Model{keymap: km, theme: th}
	snapshot := state.Snapshot{ActiveView: state.ViewDashboard, Nodes: []state.Node{{}}, LastError: ""}

	line := model.footerLine(snapshot)
	if strings.Contains(line, "boom") {
		t.Fatalf("did not expect footer to include error text, got %q", line)
	}
}
