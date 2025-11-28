package prompt

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	"github.com/adamkadaban/opensnitch-tui/internal/yara"
)

func TestInspectInsertsYaraMatchesBeforeProcessTree(t *testing.T) {
	store := state.NewStore()
	store.AddPrompt(state.Prompt{ID: "p1", Connection: state.Connection{ProcessPath: "/bin/echo", ProcessID: uint32(os.Getpid())}})
	settings := store.Snapshot().Settings
	settings.YaraEnabled = false // avoid real scan; we'll simulate result
	store.SetSettings(settings)

	m := New(store, theme.New(theme.Options{}), nil)
	m.SetSize(80, 25)

	if _, handled := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}}); !handled {
		t.Fatalf("expected inspect toggle key to be handled")
	}
	if !m.inspect {
		t.Fatalf("expected model to be in inspect mode")
	}

	matches := []yara.Match{{Rule: "rule-one"}, {Rule: "rule-two"}}
	if _, handled := m.Update(yaraResultMsg{promptID: "p1", result: yara.Result{Matches: matches}}); !handled {
		t.Fatalf("expected yaraResultMsg to be handled")
	}

	lines := m.inspectInfo.Lines
	matchIdx := -1
	ptIdx := -1
	for i, line := range lines {
		if matchIdx == -1 && strings.Contains(line, "rule-one") {
			matchIdx = i
		}
		if strings.HasPrefix(line, "Process Tree:") {
			ptIdx = i
			break
		}
	}
	if matchIdx == -1 {
		t.Fatalf("expected inspect content to include yara match line, got: %s", strings.Join(lines, "\n"))
	}
	if ptIdx != -1 {
		if matchIdx > ptIdx {
			t.Fatalf("expected yara matches before process tree, got matchIdx=%d ptIdx=%d", matchIdx, ptIdx)
		}
	}
}
