package keymap

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
)

// Global defines top-level key bindings shared across all views.
type Global struct {
	Quit     key.Binding
	Help     key.Binding
	NextView key.Binding
	PrevView key.Binding
}

// DefaultGlobal returns the default global key bindings.
func DefaultGlobal() Global {
	return Global{
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		NextView: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "next view"),
		),
		PrevView: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("shift+tab", "previous view"),
		),
	}
}

// ShortHelp renders a compact help string for the footer.
func (g Global) ShortHelp() string {
	bindings := []key.Binding{g.Quit, g.NextView, g.PrevView}
	snippets := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		help := binding.Help()
		if help.Desc == "" {
			continue
		}
		snippets = append(snippets, fmt.Sprintf("%s %s", help.Key, help.Desc))
	}
	return strings.Join(snippets, " · ")
}
