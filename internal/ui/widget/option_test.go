package widget

import (
	"strings"
	"testing"

	"github.com/adamkadaban/opensnitch-tui/internal/theme"
)

func TestIndexOf(t *testing.T) {
	options := []Option{
		{Label: "Allow", Value: "allow"},
		{Label: "Deny", Value: "deny"},
		{Label: "Ask", Value: "ask"},
	}

	tests := []struct {
		value string
		want  int
	}{
		{"allow", 0},
		{"deny", 1},
		{"ask", 2},
		{"ALLOW", 0},
		{"Deny", 1},
		{"unknown", 0},
		{"", 0},
	}

	for _, tt := range tests {
		got := IndexOf(options, tt.value)
		if got != tt.want {
			t.Errorf("IndexOf(%q) = %d, want %d", tt.value, got, tt.want)
		}
	}
}

func TestToggleOptions(t *testing.T) {
	opts := ToggleOptions()
	if len(opts) != 2 {
		t.Fatalf("ToggleOptions() returned %d options, want 2", len(opts))
	}
	if opts[0].Value != "off" || opts[1].Value != "on" {
		t.Errorf("ToggleOptions() = %v, want off/on", opts)
	}
}

func TestRenderOptionRow(t *testing.T) {
	th := theme.New(theme.Options{Name: "dark"})
	opts := []Option{{Label: "A", Value: "a"}, {Label: "B", Value: "b"}}

	result := RenderOptionRow(th, "Test", opts, 0, false)
	if !strings.Contains(result, "Test") {
		t.Errorf("RenderOptionRow missing label, got %q", result)
	}
	if !strings.Contains(result, "A") || !strings.Contains(result, "B") {
		t.Errorf("RenderOptionRow missing options, got %q", result)
	}
}

func TestRenderToggle(t *testing.T) {
	th := theme.New(theme.Options{Name: "dark"})

	result := RenderToggle(th, "Flag", true, false)
	if !strings.Contains(result, "Flag") {
		t.Errorf("RenderToggle missing label, got %q", result)
	}
	if !strings.Contains(result, "Off") || !strings.Contains(result, "On") {
		t.Errorf("RenderToggle missing toggle options, got %q", result)
	}
}
