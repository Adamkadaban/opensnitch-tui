package table

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/adamkadaban/opensnitch-tui/internal/util"
)

// ComputeMaxWidth returns the widest row width (ANSI-safe rune width).
func ComputeMaxWidth(rows []string) int {
	maxWidth := 0
	for _, row := range rows {
		if w := util.RuneWidth(row); w > maxWidth {
			maxWidth = w
		}
	}
	return maxWidth
}

// ClipRows slices each row horizontally using ANSI-safe slicing.
func ClipRows(rows []string, xOffset, width int) []string {
	if width <= 0 {
		width = 1
	}
	clipped := make([]string, len(rows))
	for i, row := range rows {
		clipped[i] = util.AnsiSlice(row, xOffset, width)
	}
	return clipped
}

// RenderCaretRow renders a caret indicator row for truncated tables.
func RenderCaretRow(width int, style lipgloss.Style) string {
	if width <= 0 {
		width = 3
	}
	glyphs := make([]rune, width)
	for i := range glyphs {
		glyphs[i] = ' '
	}
	positions := []int{0, width / 2, max(0, width-1)}
	for _, pos := range positions {
		if pos >= 0 && pos < width {
			glyphs[pos] = 'v'
		}
	}
	return style.Render(string(glyphs))
}

// PadAndStyle truncates/pads text and renders it with the given style.
func PadAndStyle(style lipgloss.Style, text string, width int, truncate bool) string {
	if width <= 0 {
		return ""
	}
	content := text
	if truncate {
		content = util.TruncateString(text, width)
	}
	if util.RuneWidth(content) < width {
		content = util.PadString(content, width)
	}
	return style.Render(content)
}
