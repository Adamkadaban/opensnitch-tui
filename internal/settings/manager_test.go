package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adamkadaban/opensnitch-tui/internal/config"
)

func TestManagerSettersPersistNormalizedValues(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	initial := config.Config{}
	mgr := NewManager(cfgPath, initial)

	theme, err := mgr.SetTheme(" Dawn ")
	if err != nil {
		t.Fatalf("SetTheme error: %v", err)
	}
	if theme != config.ThemeDawn {
		t.Fatalf("expected normalized theme %s, got %s", config.ThemeDawn, theme)
	}

	action, err := mgr.SetDefaultPromptAction("ALLOW")
	if err != nil {
		t.Fatalf("SetDefaultPromptAction error: %v", err)
	}
	if action != "allow" {
		t.Fatalf("expected normalized action allow, got %s", action)
	}

	duration, err := mgr.SetDefaultPromptDuration("ALWAYS")
	if err != nil {
		t.Fatalf("SetDefaultPromptDuration error: %v", err)
	}
	if duration != "always" {
		t.Fatalf("expected normalized duration always, got %s", duration)
	}

	target, err := mgr.SetDefaultPromptTarget("process.ID")
	if err != nil {
		t.Fatalf("SetDefaultPromptTarget error: %v", err)
	}
	if target != "process.id" {
		t.Fatalf("expected normalized target process.id, got %s", target)
	}

	alertsInterrupt, err := mgr.SetAlertsInterrupt(true)
	if err != nil {
		t.Fatalf("SetAlertsInterrupt error: %v", err)
	}
	if !alertsInterrupt {
		t.Fatalf("expected alertsInterrupt true")
	}

	timeoutSeconds, err := mgr.SetPromptTimeout(1) // below minimum; should normalize up
	if err != nil {
		t.Fatalf("SetPromptTimeout error: %v", err)
	}
	if timeoutSeconds != config.DefaultPromptTimeoutSeconds {
		t.Fatalf("expected normalized timeout %d, got %d", config.DefaultPromptTimeoutSeconds, timeoutSeconds)
	}

	// Verify persistence to disk
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read persisted config: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected config file to be written")
	}

	persisted, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if persisted.Theme != config.ThemeDawn {
		t.Fatalf("expected persisted theme %s, got %s", config.ThemeDawn, persisted.Theme)
	}
	if persisted.DefaultPromptAction != "allow" || persisted.DefaultPromptDuration != "always" || persisted.DefaultPromptTarget != "process.id" {
		t.Fatalf("unexpected persisted prompt defaults: %+v", persisted)
	}
	if persisted.AlertsInterrupt != true {
		t.Fatalf("expected persisted alertsInterrupt true")
	}
	if persisted.PromptTimeoutSeconds != config.DefaultPromptTimeoutSeconds {
		t.Fatalf("expected persisted prompt timeout %d, got %d", config.DefaultPromptTimeoutSeconds, persisted.PromptTimeoutSeconds)
	}

	pauseOnInspect, err := mgr.SetPausePromptOnInspect(false)
	if err != nil {
		t.Fatalf("SetPausePromptOnInspect error: %v", err)
	}
	if pauseOnInspect != false {
		t.Fatalf("expected pauseOnInspect false")
	}

	persisted2, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if persisted2.PausePromptOnInspect != false {
		t.Fatalf("expected persisted PausePromptOnInspect false, got %v", persisted2.PausePromptOnInspect)
	}

	yaraTempDir := t.TempDir()
	yaraDir, err := mgr.SetYaraRuleDir(yaraTempDir)
	if err != nil {
		t.Fatalf("SetYaraRuleDir error: %v", err)
	}
	if yaraDir != yaraTempDir {
		t.Fatalf("expected yara dir %s, got %s", yaraTempDir, yaraDir)
	}

	yaraEnabled, err := mgr.SetYaraEnabled(true)
	if err != nil {
		t.Fatalf("SetYaraEnabled error: %v", err)
	}
	if !yaraEnabled {
		t.Fatalf("expected YaraEnabled true")
	}

	persisted3, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if persisted3.YaraRuleDir != yaraTempDir {
		t.Fatalf("expected persisted YaraRuleDir %s, got %s", yaraTempDir, persisted3.YaraRuleDir)
	}
	if !persisted3.YaraEnabled {
		t.Fatalf("expected persisted YaraEnabled true")
	}

	// invalid path should error
	if _, err := mgr.SetYaraRuleDir("/nonexistent/dir/hopefully"); err == nil {
		t.Fatalf("expected error for invalid YaraRuleDir path")
	}
}
