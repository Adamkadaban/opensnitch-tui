package keymap

import (
	"strings"
	"testing"
)

func TestDefaultGlobal(t *testing.T) {
	km := DefaultGlobal()

	if len(km.Quit.Keys()) == 0 {
		t.Fatal("expected Quit to have keys")
	}
	if len(km.Help.Keys()) == 0 {
		t.Fatal("expected Help to have keys")
	}
	if len(km.NextView.Keys()) == 0 {
		t.Fatal("expected NextView to have keys")
	}
	if len(km.PrevView.Keys()) == 0 {
		t.Fatal("expected PrevView to have keys")
	}
}

func TestShortHelp(t *testing.T) {
	km := DefaultGlobal()
	help := km.ShortHelp()

	if help == "" {
		t.Fatal("expected non-empty short help")
	}
	if !strings.Contains(help, "ctrl+c") {
		t.Fatalf("expected short help to contain quit key, got %q", help)
	}
	if !strings.Contains(help, "tab") {
		t.Fatalf("expected short help to contain tab, got %q", help)
	}
}

func TestShortHelpSeparator(t *testing.T) {
	km := DefaultGlobal()
	help := km.ShortHelp()

	if !strings.Contains(help, " · ") {
		t.Fatalf("expected short help to use · separator, got %q", help)
	}
}
