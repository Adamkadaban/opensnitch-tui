package root

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

func TestViewFitsWindow(t *testing.T) {
	for _, size := range []struct {
		width  int
		height int
	}{
		{width: 120, height: 40},
		{width: 80, height: 40},
	} {
		store := state.NewStore()
		model := New(store, Options{Theme: theme.New(theme.Options{})})
		model.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})

		rendered := model.View()
		if got := lipgloss.Width(rendered); got > size.width {
			t.Fatalf("rendered width %d exceeds terminal width %d", got, size.width)
		}
		if got := lipgloss.Height(rendered); got > size.height {
			t.Fatalf("rendered height %d exceeds terminal height %d", got, size.height)
		}
	}
}
