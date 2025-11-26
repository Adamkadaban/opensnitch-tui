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
	Theme string `yaml:"theme"`
	Nodes []Node `yaml:"nodes"`
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
		Theme: ThemeAuto,
		Nodes: []Node{},
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
