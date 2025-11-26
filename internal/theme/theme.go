package theme

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/adamkadaban/opensnitch-tui/internal/config"
)

// Options configure the active palette.
type Options struct {
	Name string
}

// Theme exposes reusable lipgloss styles for the UI.
type Theme struct {
	Name           string
	IsLight        bool
	Title          lipgloss.Style
	Header         lipgloss.Style
	Footer         lipgloss.Style
	TabActive      lipgloss.Style
	TabInactive    lipgloss.Style
	Body           lipgloss.Style
	Card           lipgloss.Style
	Success        lipgloss.Style
	Warning        lipgloss.Style
	Danger         lipgloss.Style
	Subtle         lipgloss.Style
	TableRowEven   lipgloss.Color
	TableRowOdd    lipgloss.Color
	TableRowSelect lipgloss.Color
}

// Preset describes metadata about a theme.
type Preset struct {
	Name  string
	Label string
	Light bool
}

var presets = []Preset{
	{Name: config.ThemeMidnight, Label: "Midnight", Light: false},
	{Name: config.ThemeCanopy, Label: "Canopy", Light: false},
	{Name: config.ThemeDawn, Label: "Dawn", Light: true},
}

// Presets returns the supported theme catalog.
func Presets() []Preset {
	result := make([]Preset, len(presets))
	copy(result, presets)
	return result
}

// Normalize returns the canonical name for a theme identifier.
func Normalize(name string) string {
	return config.NormalizeThemeName(name)
}

// Label returns the friendly label for a theme name.
func Label(name string) string {
	for _, preset := range presets {
		if preset.Name == config.NormalizeThemeName(name) {
			return preset.Label
		}
	}
	return name
}

// New constructs a theme based on the provided preferences.
func New(opts Options) Theme {
	name := Normalize(opts.Name)
	switch name {
	case config.ThemeCanopy:
		return buildCanopy(name)
	case config.ThemeDawn:
		return buildDawn(name)
	default:
		return buildMidnight(name)
	}
}

// RenderTab prints a tab label using the appropriate style.
func (t Theme) RenderTab(label string, active bool) string {
	style := t.TabInactive
	if active {
		style = t.TabActive
	}
	return style.Render(label)
}

func buildMidnight(name string) Theme {
	bg := lipgloss.Color("#0f1115")
	fg := lipgloss.Color("#e7e7e7")
	primary := lipgloss.Color("#7de2d1")
	subtle := lipgloss.Color("#6b6f76")
	body := baseBody(bg, fg)

	return Theme{
		Name:           name,
		IsLight:        false,
		Title:          lipgloss.NewStyle().Foreground(primary).Bold(true).PaddingRight(1),
		Header:         lipgloss.NewStyle().Foreground(primary).Background(bg).Padding(0, 1),
		Footer:         lipgloss.NewStyle().Foreground(subtle).Background(bg).Padding(0, 1),
		TabActive:      lipgloss.NewStyle().Foreground(bg).Background(primary).Padding(0, 2).Bold(true),
		TabInactive:    lipgloss.NewStyle().Foreground(primary).Background(bg).Padding(0, 2),
		Body:           body,
		Card:           body.Copy().BorderStyle(lipgloss.NormalBorder()).BorderForeground(primary).Padding(1, 2).MarginRight(2),
		Success:        lipgloss.NewStyle().Foreground(lipgloss.Color("#4ade80")).Bold(true),
		Warning:        lipgloss.NewStyle().Foreground(lipgloss.Color("#facc15")).Bold(true),
		Danger:         lipgloss.NewStyle().Foreground(lipgloss.Color("#f87171")).Bold(true),
		Subtle:         lipgloss.NewStyle().Foreground(subtle),
		TableRowEven:   lipgloss.Color("#1f2a3a"),
		TableRowOdd:    lipgloss.Color("#111624"),
		TableRowSelect: lipgloss.Color("#2d3b52"),
	}
}

func buildCanopy(name string) Theme {
	bg := lipgloss.Color("#101712")
	fg := lipgloss.Color("#e3f2df")
	primary := lipgloss.Color("#9be15b")
	subtle := lipgloss.Color("#8ea08a")
	body := baseBody(bg, fg)

	return Theme{
		Name:           name,
		IsLight:        false,
		Title:          lipgloss.NewStyle().Foreground(primary).Bold(true).PaddingRight(1),
		Header:         lipgloss.NewStyle().Foreground(primary).Background(bg).Padding(0, 1),
		Footer:         lipgloss.NewStyle().Foreground(subtle).Background(bg).Padding(0, 1),
		TabActive:      lipgloss.NewStyle().Foreground(bg).Background(primary).Padding(0, 2).Bold(true),
		TabInactive:    lipgloss.NewStyle().Foreground(primary).Background(bg).Padding(0, 2),
		Body:           body,
		Card:           body.Copy().BorderStyle(lipgloss.RoundedBorder()).BorderForeground(primary).Padding(1, 2).MarginRight(2),
		Success:        lipgloss.NewStyle().Foreground(lipgloss.Color("#86efac")).Bold(true),
		Warning:        lipgloss.NewStyle().Foreground(lipgloss.Color("#fde047")).Bold(true),
		Danger:         lipgloss.NewStyle().Foreground(lipgloss.Color("#f97316")).Bold(true),
		Subtle:         lipgloss.NewStyle().Foreground(subtle),
		TableRowEven:   lipgloss.Color("#1b2a1f"),
		TableRowOdd:    lipgloss.Color("#131d15"),
		TableRowSelect: lipgloss.Color("#223c24"),
	}
}

func buildDawn(name string) Theme {
	bg := lipgloss.Color("#f5f1eb")
	fg := lipgloss.Color("#1f2023")
	primary := lipgloss.Color("#c2410c")
	subtle := lipgloss.Color("#7c7a75")
	body := baseBody(bg, fg)

	return Theme{
		Name:           name,
		IsLight:        true,
		Title:          lipgloss.NewStyle().Foreground(primary).Bold(true).PaddingRight(1),
		Header:         lipgloss.NewStyle().Foreground(primary).Background(bg).Padding(0, 1),
		Footer:         lipgloss.NewStyle().Foreground(subtle).Background(bg).Padding(0, 1),
		TabActive:      lipgloss.NewStyle().Foreground(bg).Background(primary).Padding(0, 2).Bold(true),
		TabInactive:    lipgloss.NewStyle().Foreground(primary).Background(bg).Padding(0, 2),
		Body:           body,
		Card:           body.Copy().BorderStyle(lipgloss.DoubleBorder()).BorderForeground(primary).Padding(1, 2).MarginRight(2),
		Success:        lipgloss.NewStyle().Foreground(lipgloss.Color("#15803d")).Bold(true),
		Warning:        lipgloss.NewStyle().Foreground(lipgloss.Color("#b45309")).Bold(true),
		Danger:         lipgloss.NewStyle().Foreground(lipgloss.Color("#b91c1c")).Bold(true),
		Subtle:         lipgloss.NewStyle().Foreground(subtle),
		TableRowEven:   lipgloss.Color("#ffffff"),
		TableRowOdd:    lipgloss.Color("#f2e8de"),
		TableRowSelect: lipgloss.Color("#f1d9c7"),
	}
}

func baseBody(bg, fg lipgloss.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(fg).Background(bg).Padding(1, 2)
}
