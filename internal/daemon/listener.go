package daemon

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

type listenTarget struct {
	network string
	address string
}

func parseListenAddr(addr string) (listenTarget, error) {
	value := strings.TrimSpace(addr)
	if value == "" {
		return listenTarget{}, fmt.Errorf("listen address cannot be empty")
	}
	if strings.HasPrefix(value, "unix://") {
		path := strings.TrimPrefix(value, "unix://")
		if path == "" {
			return listenTarget{}, fmt.Errorf("unix socket path cannot be empty")
		}
		return listenTarget{network: "unix", address: path}, nil
	}
	return listenTarget{network: "tcp", address: value}, nil
}

func removeStaleUnixSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect unix socket: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to remove non-socket path %q", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale socket: %w", err)
	}
	return nil
}
