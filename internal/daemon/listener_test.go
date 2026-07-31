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

func TestRemoveStaleUnixSocketRemovesSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "osui.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen on unix socket: %v", err)
	}
	defer listener.Close()

	if err := removeStaleUnixSocket(path); err != nil {
		t.Fatalf("remove unix socket: %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("expected unix socket removal, got %v", err)
	}
}

func TestRemoveStaleUnixSocketAllowsMissingPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.sock")
	if err := removeStaleUnixSocket(path); err != nil {
		t.Fatalf("missing socket path: %v", err)
	}
}
