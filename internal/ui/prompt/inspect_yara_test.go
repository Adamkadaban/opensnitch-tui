//go:build !cgo || no_yara

package prompt

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
)

func TestInspectYaraUnavailableStub(t *testing.T) {
	store := state.NewStore()
	store.AddPrompt(state.Prompt{
		ID:         "p1",
		Connection: state.Connection{ProcessPath: "/bin/echo"},
	})
	settings := store.Snapshot().Settings
	settings.YaraEnabled = true
	settings.YaraRuleDir = "/tmp"
	store.SetSettings(settings)

	m := New(store, theme.New(theme.Options{}), nil)
	m.SetSize(80, 25)

	if _, handled := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}}); !handled {
		t.Fatalf("expected inspect toggle key to be handled")
	}
	if !m.inspect {
		t.Fatalf("expected model to be in inspect mode")
	}
	view := m.View()
	if !strings.Contains(view, "YARA") {
		t.Fatalf("expected YARA status in inspect view; got %q", view)
	}
}
