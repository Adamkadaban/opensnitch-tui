package util

import (
	"strings"
	"time"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
)

// Fallback returns def when value is empty or whitespace only.
func Fallback(value, def string) string {
	if strings.TrimSpace(value) == "" {
		return def
	}
	return value
}

// RelativeTime renders a human-friendly duration ago value.
func RelativeTime(ts time.Time) string {
	delta := time.Since(ts)
	if delta < time.Second {
		delta = time.Second
	}
	return delta.Truncate(time.Second).String() + " ago"
}

// WrapIndex wraps the index within [0,length).
func WrapIndex(current, delta, length int) int {
	if length <= 0 {
		return 0
	}
	next := (current + delta) % length
	if next < 0 {
		next += length
	}
	return next
}

// DisplayName returns node.Name when present, otherwise the node.Address.
func DisplayName(node state.Node) string {
	if node.Name != "" {
		return node.Name
	}
	return node.Address
}

// TruncateString truncates a string to width runes with an ellipsis when needed.
func TruncateString(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width <= 3 {
		return string(runes[:width])
	}
	return string(runes[:width-3]) + "..."
}

// PadString pads value with spaces up to width runes.
func PadString(value string, width int) string {
	padding := width - len([]rune(value))
	if padding > 0 {
		return value + strings.Repeat(" ", padding)
	}
	return value
}
