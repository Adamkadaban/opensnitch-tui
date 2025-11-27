package settings

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
)

type fakeSettingsController struct {
	setThemeCalls int
	lastTheme     string
}

func (f *fakeSettingsController) SetTheme(name string) (string, error) {
	f.setThemeCalls++
	f.lastTheme = name
	return name, nil
}
func (f *fakeSettingsController) SetDefaultPromptAction(action string) (string, error) {
	return action, nil
}
func (f *fakeSettingsController) SetDefaultPromptDuration(duration string) (string, error) {
	return duration, nil
}
func (f *fakeSettingsController) SetDefaultPromptTarget(target string) (string, error) {
	return target, nil
}
func (f *fakeSettingsController) SetAlertsInterrupt(enabled bool) (bool, error) { return enabled, nil }
func (f *fakeSettingsController) SetPromptTimeout(seconds int) (int, error)     { return seconds, nil }
func (f *fakeSettingsController) SetPausePromptOnInspect(enabled bool) (bool, error) {
	return enabled, nil
}
func (f *fakeSettingsController) SetYaraRuleDir(path string) (string, error) { return path, nil }
func (f *fakeSettingsController) SetYaraEnabled(enabled bool) (bool, error)  { return enabled, nil }

func TestSettingsViewRenderContainsFields(t *testing.T) {
	store := state.NewStore()
	th := theme.New(theme.Options{})
	m := New(store, th, &fakeSettingsController{}).(*Model)
	m.SetSize(80, 20)

	out := m.View()
	checks := []string{"Theme", "Default action", "Default duration", "Default target", "Prompt timeout", "Alerts interrupt", "Pause alert timeout on inspect", "YARA scanning enabled", "YARA rule directory"}
	for _, c := range checks {
		if !strings.Contains(out, c) {
			t.Fatalf("expected view to contain %q, got: %s", c, out)
		}
	}
}

func TestSettingsViewPersistThemeOnSaveFocused(t *testing.T) {
	store := state.NewStore()
	th := theme.New(theme.Options{})
	ctrl := &fakeSettingsController{}
	m := New(store, th, ctrl).(*Model)
	m.SetSize(80, 20)

	// Focus on theme (default focus) and press 's'
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	if ctrl.setThemeCalls != 1 {
		t.Fatalf("expected SetTheme to be called once, got %d", ctrl.setThemeCalls)
	}
	if ctrl.lastTheme == "" {
		t.Fatalf("expected lastTheme to be set")
	}
}
