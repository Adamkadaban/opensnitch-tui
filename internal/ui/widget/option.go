package widget

import (
	"fmt"
	"strings"

	"github.com/adamkadaban/opensnitch-tui/internal/theme"
)

// Option represents a selectable item with a display label and underlying value.
type Option struct {
	Label string
	Value string
}

// IndexOf returns the index of the option with the given value, or 0 if not found.
func IndexOf(options []Option, value string) int {
	value = strings.ToLower(value)
	for i, opt := range options {
		if strings.ToLower(opt.Value) == value {
			return i
		}
	}
	return 0
}

// ToggleOptions returns the standard Off/On toggle options.
func ToggleOptions() []Option {
	return []Option{{Label: "Off", Value: "off"}, {Label: "On", Value: "on"}}
}

// RenderOptionRow renders a horizontal row of selectable options with the given
// label, highlighting the selected option and optionally styling for focus.
func RenderOptionRow(th theme.Theme, label string, opts []Option, selected int, focused bool) string {
	cells := make([]string, len(opts))
	for idx, opt := range opts {
		style := th.TabInactive
		marker := " "
		if idx == selected {
			style = th.TabActive
			if focused {
				style = style.Underline(true).Bold(true)
				marker = th.Warning.Render(">")
			}
		} else if focused {
			style = style.Faint(true)
		}
		cells[idx] = fmt.Sprintf("%s%s", marker, style.Render(opt.Label))
	}
	return fmt.Sprintf("%s %s", th.Header.Render(label+":"), strings.Join(cells, " "))
}

// RenderToggle renders a binary On/Off toggle row.
func RenderToggle(th theme.Theme, label string, enabled, focused bool) string {
	idx := 0
	if enabled {
		idx = 1
	}
	return RenderOptionRow(th, label, ToggleOptions(), idx, focused)
}
