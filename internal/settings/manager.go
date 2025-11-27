package settings

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/adamkadaban/opensnitch-tui/internal/config"
)

// Manager persists user-facing settings to disk.
type Manager struct {
	path string
	mu   sync.Mutex
	cfg  config.Config
}

// NewManager returns a manager initialized with the current configuration snapshot.
func NewManager(path string, cfg config.Config) *Manager {
	cfg.Theme = config.NormalizeThemeName(cfg.Theme)
	return &Manager{path: path, cfg: cfg}
}

// SetTheme updates the preferred color palette and writes it to disk.
func (m *Manager) SetTheme(name string) (string, error) {
	normalized := config.NormalizeThemeName(name)
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cfg.Theme = normalized
	if err := config.Save(m.path, m.cfg); err != nil {
		return "", err
	}
	return normalized, nil
}

// SetDefaultPromptAction stores the normalized default prompt action and writes it to disk.
func (m *Manager) SetDefaultPromptAction(action string) (string, error) {
	normalized := config.NormalizePromptAction(strings.ToLower(strings.TrimSpace(action)))
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cfg.DefaultPromptAction = normalized
	if err := config.Save(m.path, m.cfg); err != nil {
		return "", err
	}
	return normalized, nil
}

// SetDefaultPromptDuration stores the normalized default prompt duration and writes it to disk.
func (m *Manager) SetDefaultPromptDuration(duration string) (string, error) {
	normalized := config.NormalizePromptDuration(strings.ToLower(strings.TrimSpace(duration)))
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cfg.DefaultPromptDuration = normalized
	if err := config.Save(m.path, m.cfg); err != nil {
		return "", err
	}
	return normalized, nil
}

// SetDefaultPromptTarget stores the normalized default prompt target and writes it to disk.
func (m *Manager) SetDefaultPromptTarget(target string) (string, error) {
	normalized := config.NormalizePromptTarget(strings.ToLower(strings.TrimSpace(target)))
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cfg.DefaultPromptTarget = normalized
	if err := config.Save(m.path, m.cfg); err != nil {
		return "", err
	}
	return normalized, nil
}

// SetAlertsInterrupt toggles whether alerts interrupt active work.
func (m *Manager) SetAlertsInterrupt(enabled bool) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cfg.AlertsInterrupt = enabled
	if err := config.Save(m.path, m.cfg); err != nil {
		return m.cfg.AlertsInterrupt, err
	}
	return m.cfg.AlertsInterrupt, nil
}

// SetPromptTimeout updates the default prompt timeout duration in seconds.
func (m *Manager) SetPromptTimeout(seconds int) (int, error) {
	normalized := config.NormalizePromptTimeoutSeconds(seconds)
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cfg.PromptTimeoutSeconds = normalized
	if err := config.Save(m.path, m.cfg); err != nil {
		return 0, err
	}
	return normalized, nil
}

// SetPausePromptOnInspect toggles whether to pause prompt timeout while inspecting.
func (m *Manager) SetPausePromptOnInspect(enabled bool) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cfg.PausePromptOnInspect = enabled
	if err := config.Save(m.path, m.cfg); err != nil {
		return m.cfg.PausePromptOnInspect, err
	}
	return m.cfg.PausePromptOnInspect, nil
}

// SetYaraRuleDir sets the directory containing YARA rules.
func (m *Manager) SetYaraRuleDir(path string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	path = strings.TrimSpace(path)
	if path != "" {
		info, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("%s: %w", path, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("%s is not a directory", path)
		}
	}

	m.cfg.YaraRuleDir = path
	if err := config.Save(m.path, m.cfg); err != nil {
		return m.cfg.YaraRuleDir, err
	}
	return m.cfg.YaraRuleDir, nil
}

// SetYaraEnabled toggles YARA scanning.
func (m *Manager) SetYaraEnabled(enabled bool) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cfg.YaraEnabled = enabled
	if err := config.Save(m.path, m.cfg); err != nil {
		return m.cfg.YaraEnabled, err
	}
	return m.cfg.YaraEnabled, nil
}

// Config returns a copy of the managed config.
func (m *Manager) Config() config.Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}
