package daemon

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveStaleUnixSocketRejectsRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "osui.sock")
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write regular file: %v", err)
	}

	if err := removeStaleUnixSocket(path); err == nil {
		t.Fatal("expected non-socket path rejection")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected regular file to remain: %v", err)
	}
}

func TestRemoveStaleUnixSocketRejectsActiveSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "osui.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen on unix socket: %v", err)
	}
	defer listener.Close()

	if err := removeStaleUnixSocket(path); err == nil {
		t.Fatal("expected active socket rejection")
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("expected active socket to remain: %v", err)
	}
}

func TestRemoveStaleUnixSocketRemovesClosedSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "osui.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatalf("listen on unix socket: %v", err)
	}
	listener.SetUnlinkOnClose(false)
	if err := listener.Close(); err != nil {
		t.Fatalf("close unix socket: %v", err)
	}

	if err := removeStaleUnixSocket(path); err != nil {
		t.Fatalf("remove closed unix socket: %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("expected stale socket removal, got %v", err)
	}
}

func TestRemoveStaleUnixSocketAllowsMissingPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.sock")
	if err := removeStaleUnixSocket(path); err != nil {
		t.Fatalf("missing socket path: %v", err)
	}
}
