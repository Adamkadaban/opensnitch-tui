package theme

import (
	"testing"

	"github.com/adamkadaban/opensnitch-tui/internal/config"
)

func TestNewMidnight(t *testing.T) {
	th := New(Options{Name: config.ThemeMidnight})
	if th.Name != config.ThemeMidnight {
		t.Fatalf("expected name %q, got %q", config.ThemeMidnight, th.Name)
	}
	if th.IsLight {
		t.Fatal("Midnight theme should not be light")
	}
}

func TestNewCanopy(t *testing.T) {
	th := New(Options{Name: config.ThemeCanopy})
	if th.Name != config.ThemeCanopy {
		t.Fatalf("expected name %q, got %q", config.ThemeCanopy, th.Name)
	}
	if th.IsLight {
		t.Fatal("Canopy theme should not be light")
	}
}

func TestNewDawn(t *testing.T) {
	th := New(Options{Name: config.ThemeDawn})
	if th.Name != config.ThemeDawn {
		t.Fatalf("expected name %q, got %q", config.ThemeDawn, th.Name)
	}
	if !th.IsLight {
		t.Fatal("Dawn theme should be light")
	}
}

func TestNewDefaultToMidnight(t *testing.T) {
	th := New(Options{Name: ""})
	if th.Name != config.ThemeMidnight {
		t.Fatalf("expected default name %q, got %q", config.ThemeMidnight, th.Name)
	}
}

func TestNormalize(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"", config.ThemeMidnight},
		{" MIDNIGHT ", config.ThemeMidnight},
		{"canopy", config.ThemeCanopy},
		{" Dawn ", config.ThemeDawn},
		{"unknown", config.ThemeMidnight},
	}
	for _, tc := range cases {
		got := Normalize(tc.input)
		if got != tc.expected {
			t.Errorf("Normalize(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestLabel(t *testing.T) {
	if l := Label(config.ThemeMidnight); l != "Midnight" {
		t.Errorf("Label(%q) = %q, want Midnight", config.ThemeMidnight, l)
	}
	if l := Label(config.ThemeCanopy); l != "Canopy" {
		t.Errorf("Label(%q) = %q, want Canopy", config.ThemeCanopy, l)
	}
	if l := Label(config.ThemeDawn); l != "Dawn" {
		t.Errorf("Label(%q) = %q, want Dawn", config.ThemeDawn, l)
	}
}

func TestPresets(t *testing.T) {
	presets := Presets()
	if len(presets) < 3 {
		t.Fatalf("expected at least 3 presets, got %d", len(presets))
	}

	names := make(map[string]bool)
	for _, p := range presets {
		names[p.Name] = true
	}
	if !names[config.ThemeMidnight] {
		t.Error("expected Midnight in presets")
	}
	if !names[config.ThemeCanopy] {
		t.Error("expected Canopy in presets")
	}
	if !names[config.ThemeDawn] {
		t.Error("expected Dawn in presets")
	}
}

func TestRenderTab(t *testing.T) {
	th := New(Options{Name: config.ThemeMidnight})

	activeTab := th.RenderTab("Test", true)
	inactiveTab := th.RenderTab("Test", false)

	if activeTab == "" || inactiveTab == "" {
		t.Fatal("expected non-empty tab renders")
	}
	// Both use "Test" as content, styling may be embedded
	if len(activeTab) == 0 || len(inactiveTab) == 0 {
		t.Fatal("expected tab content to be present")
	}
}
