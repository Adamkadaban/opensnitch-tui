package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/adamkadaban/opensnitch-tui/internal/app"
)

func main() {
	var (
		configPath string
		themeName  string
		listenAddr string
	)

	flag.StringVar(&configPath, "config", "", "Path to the config file (defaults to XDG config dir)")
	flag.StringVar(&themeName, "theme", "", "Override theme (light, dark, auto)")
	flag.StringVar(&listenAddr, "listen", "127.0.0.1:50051", "gRPC listen address for daemon connections")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	opts := app.Options{
		ConfigPath: configPath,
		Theme:      themeName,
		ListenAddr: listenAddr,
	}

	if err := app.Run(ctx, opts); err != nil {
		fmt.Fprintf(os.Stderr, "opensnitch-tui: %v\n", err)
		os.Exit(1)
	}
}
