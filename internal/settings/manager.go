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

// Config returns a copy of the managed config.
func (m *Manager) Config() config.Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}
