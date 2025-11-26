package theme

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Mode controls the global color palette selection.
type Mode string

const (
	ModeAuto  Mode = "auto"
	ModeDark  Mode = "dark"
	ModeLight Mode = "light"
)

// Options configure the active theme at runtime.
type Options struct {
	Override  string
	Preferred string
}

// Theme exposes reusable lipgloss styles for the UI.
type Theme struct {
	Mode        Mode
	Title       lipgloss.Style
	Header      lipgloss.Style
	Footer      lipgloss.Style
	TabActive   lipgloss.Style
	TabInactive lipgloss.Style
	Body        lipgloss.Style
	Card        lipgloss.Style
	Success     lipgloss.Style
	Warning     lipgloss.Style
	Danger      lipgloss.Style
	Subtle      lipgloss.Style
}

// New constructs a theme based on the provided preferences.
func New(opts Options) Theme {
	mode := selectMode(opts.Override, opts.Preferred)
	if mode == ModeLight {
		return buildLight(mode)
	}
	return buildDark(mode)
}

// RenderTab prints a tab label using the appropriate style.
func (t Theme) RenderTab(label string, active bool) string {
	style := t.TabInactive
	if active {
		style = t.TabActive
	}
	return style.Render(label)
}

func selectMode(override, preferred string) Mode {
	if mode := parseMode(override); mode != "" {
		return applyAuto(mode)
	}
	if mode := parseMode(preferred); mode != "" {
		return applyAuto(mode)
	}
	return ModeDark
}

func parseMode(value string) Mode {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(ModeDark):
		return ModeDark
	case string(ModeLight):
		return ModeLight
	case string(ModeAuto):
		return ModeAuto
	default:
		return ""
	}
}

func applyAuto(mode Mode) Mode {
	if mode == ModeAuto {
		return ModeDark
	}
	return mode
}

func buildDark(mode Mode) Theme {
	bg := lipgloss.Color("#0f1115")
	fg := lipgloss.Color("#e7e7e7")
	primary := lipgloss.Color("#7de2d1")
	subtle := lipgloss.Color("#6b6f76")

	body := lipgloss.NewStyle().Foreground(fg).Background(bg).Padding(1, 2)

	return Theme{
		Mode:        mode,
		Title:       lipgloss.NewStyle().Foreground(primary).Bold(true).PaddingRight(1),
		Header:      lipgloss.NewStyle().Foreground(primary).Background(bg).Padding(0, 1),
		Footer:      lipgloss.NewStyle().Foreground(subtle).Background(bg).Padding(0, 1),
		TabActive:   lipgloss.NewStyle().Foreground(bg).Background(primary).Padding(0, 2).Bold(true),
		TabInactive: lipgloss.NewStyle().Foreground(primary).Background(bg).Padding(0, 2),
		Body:        body,
		Card:        body.Copy().BorderStyle(lipgloss.NormalBorder()).BorderForeground(primary).Padding(1, 2).MarginRight(2),
		Success:     lipgloss.NewStyle().Foreground(lipgloss.Color("#4ade80")).Bold(true),
		Warning:     lipgloss.NewStyle().Foreground(lipgloss.Color("#facc15")).Bold(true),
		Danger:      lipgloss.NewStyle().Foreground(lipgloss.Color("#f87171")).Bold(true),
		Subtle:      lipgloss.NewStyle().Foreground(subtle),
	}
}

func buildLight(mode Mode) Theme {
	bg := lipgloss.Color("#f7f7f7")
	fg := lipgloss.Color("#1b1e23")
	primary := lipgloss.Color("#155e75")
	accent := lipgloss.Color("#d97706")
	subtle := lipgloss.Color("#6b7280")

	body := lipgloss.NewStyle().Foreground(fg).Background(bg).Padding(1, 2)

	return Theme{
		Mode:        mode,
		Title:       lipgloss.NewStyle().Foreground(primary).Bold(true).PaddingRight(1),
		Header:      lipgloss.NewStyle().Foreground(primary).Background(bg).Padding(0, 1),
		Footer:      lipgloss.NewStyle().Foreground(subtle).Background(bg).Padding(0, 1),
		TabActive:   lipgloss.NewStyle().Foreground(bg).Background(primary).Padding(0, 2).Bold(true),
		TabInactive: lipgloss.NewStyle().Foreground(primary).Background(bg).Padding(0, 2),
		Body:        body,
		Card:        body.Copy().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(primary).Padding(1, 2).MarginRight(2),
		Success:     lipgloss.NewStyle().Foreground(lipgloss.Color("#15803d")).Bold(true),
		Warning:     lipgloss.NewStyle().Foreground(accent).Bold(true),
		Danger:      lipgloss.NewStyle().Foreground(lipgloss.Color("#b91c1c")).Bold(true),
		Subtle:      lipgloss.NewStyle().Foreground(subtle),
	}
}
