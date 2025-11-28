package table

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestComputeMaxWidth(t *testing.T) {
	rows := []string{"abc", "de", "fghij"}
	if w := ComputeMaxWidth(rows); w != 5 {
		t.Fatalf("expected 5, got %d", w)
	}
}

func TestClipRowsAnsiSafe(t *testing.T) {
	rows := []string{"\x1b[31mred\x1b[0m text"}
	clipped := ClipRows(rows, 0, 3)
	if clipped[0] != "\x1b[31mred\x1b[0m" {
		t.Fatalf("unexpected clip: %q", clipped[0])
	}
}

func TestRenderCaretRow(t *testing.T) {
	row := RenderCaretRow(5, lipgloss.NewStyle())
	if len([]rune(row)) != 5 {
		t.Fatalf("expected width 5, got %d", len([]rune(row)))
	}
}

func TestPadAndStyle(t *testing.T) {
	st := lipgloss.NewStyle()
	res := PadAndStyle(st, "abc", 5, false)
	if len([]rune(res)) < 5 {
		t.Fatalf("expected padded width >=5, got %d", len([]rune(res)))
	}
}
