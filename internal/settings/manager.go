package settings

import (
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
	return &Manager{path: path, cfg: cfg}
}

// SetDefaultPromptAction stores the normalized default prompt action and writes it to disk.
func (m *Manager) SetDefaultPromptAction(action string) (string, error) {
	normalized := config.NormalizePromptAction(action)
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
	normalized := config.NormalizePromptDuration(duration)
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
	normalized := config.NormalizePromptTarget(target)
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

// Config returns a copy of the managed config.
func (m *Manager) Config() config.Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}
