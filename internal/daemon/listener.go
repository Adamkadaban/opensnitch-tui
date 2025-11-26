package daemon

import (
	"fmt"
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
