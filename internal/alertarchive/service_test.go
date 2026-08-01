package alertarchive

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
)

func TestExportPermissionsAtomicReplacementAndSanitizedName(t *testing.T) {
	service, root := newTestService(t, DefaultLimits())
	alert := state.Alert{
		ID: "../alert id", NodeID: "../../node", CreatedAt: time.Date(2026, 7, 31, 18, 0, 0, 0, time.UTC),
		Priority: state.AlertPriorityHigh, Type: state.AlertTypeWarning, What: state.AlertWhatGeneric,
		PayloadKind: state.AlertPayloadText, Text: "first",
	}
	path, err := service.Export(context.Background(), alert)
	if err != nil {
		t.Fatalf("Export error: %v", err)
	}
	if filepath.Dir(path) != root || strings.Contains(filepath.Base(path), "..") || filepath.Ext(path) != ".json" {
		t.Fatalf("unsafe export path %q", path)
	}
	assertMode(t, root, 0o700)
	assertMode(t, path, 0o600)

	alert.Text = "replacement"
	replaced, err := service.Export(context.Background(), alert)
	if err != nil || replaced != path {
		t.Fatalf("replacement Export = %q, %v", replaced, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir error: %v", err)
	}
	if len(entries) != 1 || strings.Contains(entries[0].Name(), ".tmp") {
		t.Fatalf("expected one atomically replaced file, got %+v", entries)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "replacement") {
		t.Fatalf("expected replacement data, got %q (%v)", data, err)
	}
}

func TestDefaultRootUsesPrivateXDGDataLocation(t *testing.T) {
	base, cleanup := testRoot(t)
	defer cleanup()
	t.Setenv("XDG_DATA_HOME", base)
	root, err := DefaultRoot()
	if err != nil {
		t.Fatalf("DefaultRoot error: %v", err)
	}
	want := filepath.Join(base, "opensnitch-tui", "alerts")
	if root != want {
		t.Fatalf("DefaultRoot = %q, want %q", root, want)
	}

	t.Setenv("XDG_DATA_HOME", "relative")
	if _, err := DefaultRoot(); err == nil {
		t.Fatal("expected relative XDG_DATA_HOME rejection")
	}
}

func TestExportRedactsEnvironmentAndSensitiveFields(t *testing.T) {
	service, _ := newTestService(t, DefaultLimits())
	alert := state.Alert{
		ID: "1", PayloadKind: state.AlertPayloadProcess,
		Process: &state.Process{
			Args: []string{"curl", "--token=top-secret"}, Env: map[string]string{"TOKEN": "env-secret"},
			Checksums: map[string]string{"authorization": "checksum-secret"},
		},
	}

	path, err := service.Export(context.Background(), alert)
	if err != nil {
		t.Fatalf("Export error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	text := string(data)
	for _, secret := range []string{"top-secret", "env-secret", "checksum-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("archive leaked %q: %s", secret, text)
		}
	}
	if !strings.Contains(text, "[redacted]") {
		t.Fatalf("expected redaction marker: %s", text)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

func TestExportPreservesCaseSensitiveRuleData(t *testing.T) {
	service, _ := newTestService(t, DefaultLimits())
	alert := state.Alert{
		ID: "case-sensitive", PayloadKind: state.AlertPayloadRule,
		Rule: &state.Rule{
			Name: "case-sensitive-path",
			Operator: state.RuleOperator{
				Type: "simple", Operand: "process.path", Data: "/Opt/Case/Sensitive/App", Sensitive: true,
			},
		},
	}

	path, err := service.Export(context.Background(), alert)
	if err != nil {
		t.Fatalf("Export error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if !strings.Contains(string(data), "/Opt/Case/Sensitive/App") {
		t.Fatalf("case-sensitive rule data was redacted: %s", data)
	}
}

func TestExportRejectsSymlinkDirectoryAndTarget(t *testing.T) {
	t.Run("directory", func(t *testing.T) {
		base, cleanup := testRoot(t)
		defer cleanup()
		target := filepath.Join(base, "target")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatalf("Mkdir error: %v", err)
		}
		root := filepath.Join(base, "alerts")
		if err := os.Symlink(target, root); err != nil {
			t.Fatalf("Symlink error: %v", err)
		}
		service, err := New(root, DefaultLimits())
		if err != nil {
			t.Fatalf("New error: %v", err)
		}
		if _, err := service.Export(context.Background(), state.Alert{ID: "1"}); err == nil || !strings.Contains(err.Error(), "regular directory") {
			t.Fatalf("expected symlink directory rejection, got %v", err)
		}
	})

	t.Run("parent directory", func(t *testing.T) {
		base, cleanup := testRoot(t)
		defer cleanup()
		target := filepath.Join(base, "target")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatalf("Mkdir error: %v", err)
		}
		parent := filepath.Join(base, "linked-parent")
		if err := os.Symlink(target, parent); err != nil {
			t.Fatalf("Symlink error: %v", err)
		}
		service, err := New(filepath.Join(parent, "alerts"), DefaultLimits())
		if err != nil {
			t.Fatalf("New error: %v", err)
		}
		if _, err := service.Export(context.Background(), state.Alert{ID: "1"}); err == nil || !strings.Contains(err.Error(), "regular directory") {
			t.Fatalf("expected symlink parent rejection, got %v", err)
		}
	})

	t.Run("target", func(t *testing.T) {
		service, root := newTestService(t, DefaultLimits())
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatalf("MkdirAll error: %v", err)
		}
		alert := state.Alert{ID: "1"}
		path := filepath.Join(root, archiveFilename(alert))
		target := filepath.Join(root, "target")
		if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
			t.Fatalf("WriteFile error: %v", err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatalf("Symlink error: %v", err)
		}
		if _, err := service.Export(context.Background(), alert); err == nil || !strings.Contains(err.Error(), "non-regular") {
			t.Fatalf("expected symlink target rejection, got %v", err)
		}
	})
}

func TestExportBoundsStructuredOutput(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxTextBytes = 32
	limits.MaxListItems = 2
	limits.MaxMapItems = 2
	limits.MaxOperatorDepth = 2
	limits.MaxOperators = 2
	service, _ := newTestService(t, limits)

	long := strings.Repeat("x", 10000)
	alert := state.Alert{
		ID: long, PayloadKind: state.AlertPayloadProcess,
		Process: &state.Process{
			Args: []string{long, long, long},
			Env:  map[string]string{"A": long, "B": long, "C": long},
			ProcessTree: []state.ProcessTreeEntry{
				{Path: long, PID: 1}, {Path: long, PID: 2}, {Path: long, PID: 3},
			},
		},
	}
	path, err := service.Export(context.Background(), alert)
	if err != nil {
		t.Fatalf("Export error: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat error: %v", err)
	}
	if info.Size() > int64(limits.MaxFileBytes) || info.Size() > 4096 {
		t.Fatalf("expected bounded output, got %d bytes", info.Size())
	}
}

func newTestService(t *testing.T, limits Limits) (*Service, string) {
	t.Helper()
	base, cleanup := testRoot(t)
	t.Cleanup(cleanup)
	root := filepath.Join(base, "alerts")
	service, err := New(root, limits)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	return service, root
}

func testRoot(t *testing.T) (string, func()) {
	t.Helper()
	root, err := os.MkdirTemp(".", ".alertarchive-test-")
	if err != nil {
		t.Fatalf("MkdirTemp error: %v", err)
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("Abs error: %v", err)
	}
	return absolute, func() {
		if err := os.RemoveAll(absolute); err != nil {
			t.Errorf("RemoveAll error: %v", err)
		}
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", path, got, want)
	}
}
