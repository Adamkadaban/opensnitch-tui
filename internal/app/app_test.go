package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunReturnsErrorOnInvalidListenAddr(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := Run(ctx, Options{ListenAddr: "unix://"})
	if err == nil {
		t.Fatalf("expected error for invalid listen address, got nil")
	}
}

func TestRunReturnsErrorOnUnreadableConfig(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dir := t.TempDir()
	// Create a directory path but pass it as a file to force read error
	cfgPath := filepath.Join(dir, "cfgdir")
	if err := os.MkdirAll(cfgPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	err := Run(ctx, Options{ConfigPath: cfgPath})
	if err == nil {
		t.Fatalf("expected error for unreadable config (directory), got nil")
	}
}
