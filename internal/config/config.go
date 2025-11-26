package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	ThemeAuto  = "auto"
	ThemeDark  = "dark"
	ThemeLight = "light"
)

// Config captures persisted user preferences and known daemon nodes.
type Config struct {
	Theme                 string `yaml:"theme"`
	DefaultPromptAction   string `yaml:"default_prompt_action"`
	DefaultPromptDuration string `yaml:"default_prompt_duration"`
	DefaultPromptTarget   string `yaml:"default_prompt_target"`
	PromptTimeoutSeconds  int    `yaml:"prompt_timeout_seconds"`
	AlertsInterrupt       bool   `yaml:"alerts_interrupt"`
	Nodes                 []Node `yaml:"nodes"`
}

// Node contains metadata required to connect to an OpenSnitch daemon instance.
type Node struct {
	ID        string `yaml:"id"`
	Name      string `yaml:"name"`
	Address   string `yaml:"address"`
	CertPath  string `yaml:"cert_path"`
	KeyPath   string `yaml:"key_path"`
	SkipTLS   bool   `yaml:"skip_tls"`
	Authority string `yaml:"authority"`
}

// Load reads configuration data from the provided path. If the file does not exist,
// a default configuration is returned without an error.
func Load(path string) (Config, error) {
	cfg := Default()

	resolved, err := resolvePath(path)
	if err != nil {
		return cfg, fmt.Errorf("resolve config path: %w", err)
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}

	return cfg, nil
}

// Default returns a usable configuration when no file exists yet.
func Default() Config {
	return Config{
		Theme:                 ThemeAuto,
		DefaultPromptAction:   DefaultPromptAction,
		DefaultPromptDuration: DefaultPromptDuration,
		DefaultPromptTarget:   DefaultPromptTarget,
		PromptTimeoutSeconds:  DefaultPromptTimeoutSeconds,
		AlertsInterrupt:       DefaultAlertsInterrupt,
		Nodes:                 []Node{},
	}
}

// DefaultPath returns the standard configuration path within the user's
// XDG config directory.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("user config dir: %w", err)
	}
	return filepath.Join(dir, "opensnitch-tui", "config.yaml"), nil
}

func resolvePath(path string) (string, error) {
	if path != "" {
		return path, nil
	}
	return DefaultPath()
}

const DefaultPromptAction = "deny"
const DefaultPromptDuration = "once"
const DefaultPromptTarget = "process.path"
const DefaultPromptTimeoutSeconds = 30
const DefaultAlertsInterrupt = true

// NormalizePromptAction ensures stored prompts actions stay within supported values.
func NormalizePromptAction(action string) string {
	switch action {
	case "allow", "deny", "reject":
		return action
	default:
		return DefaultPromptAction
	}
}

// NormalizePromptDuration clamps duration defaults to supported values.
func NormalizePromptDuration(duration string) string {
	switch duration {
	case "once", "until restart", "always":
		return duration
	default:
		return DefaultPromptDuration
	}
}

// NormalizePromptTarget restricts target defaults to known operands.
func NormalizePromptTarget(target string) string {
	switch target {
	case "process.path", "process.command", "process.id", "user.id", "dest.ip", "dest.host", "dest.port":
		return target
	default:
		return DefaultPromptTarget
	}
}

// NormalizePromptTimeoutSeconds ensures a reasonable timeout window.
func NormalizePromptTimeoutSeconds(seconds int) int {
	if seconds < 5 {
		return DefaultPromptTimeoutSeconds
	}
	if seconds > 600 {
		return 600
	}
	return seconds
}

// ResolvePath returns the concrete config file path.
func ResolvePath(path string) (string, error) {
	return resolvePath(path)
}

// Save writes configuration data to disk.
func Save(path string, cfg Config) error {
	resolved, err := resolvePath(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return fmt.Errorf("ensure config dir: %w", err)
	}
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.WriteFile(resolved, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}
