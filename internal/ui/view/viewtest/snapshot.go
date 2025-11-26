package viewtest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// AssertSnapshot compares the rendered view against the stored snapshot file.
func AssertSnapshot(t *testing.T, actual, goldenPath string) {
	t.Helper()

	abs := goldenPath
	if !filepath.IsAbs(goldenPath) {
		wd, err := os.Getwd()
		if err != nil {
			t.Fatalf("get working directory: %v", err)
		}
		abs = filepath.Join(wd, goldenPath)
	}

	if os.Getenv("UPDATE_SNAPSHOTS") == "1" {
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("create snapshot dir: %v", err)
		}
		if err := os.WriteFile(abs, []byte(actual), 0o600); err != nil {
			t.Fatalf("write snapshot %s: %v", abs, err)
		}
	}

	expected, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read snapshot %s: %v", abs, err)
	}
	if diff := cmp.Diff(string(expected), actual); diff != "" {
		t.Fatalf("snapshot mismatch (-want +got):\n%s", diff)
	}
}
