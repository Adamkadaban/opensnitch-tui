package rulearchive

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
)

func TestExportImportRoundTripNestedOperators(t *testing.T) {
	service, root := newTestService(t, DefaultLimits())
	node := state.Node{ID: "tcp://10.0.0.1:50051", Name: "../Office / Node"}
	created := time.Date(2026, time.July, 31, 12, 34, 56, 123000000, time.FixedZone("test", -7*60*60))
	updated := created.Add(5 * time.Minute)
	rules := []state.Rule{{
		NodeID:      node.ID,
		Name:        "../allow curl",
		Description: "nested rule",
		Action:      "allow",
		Duration:    "always",
		Enabled:     true,
		Precedence:  true,
		NoLog:       true,
		CreatedAt:   created,
		UpdatedAt:   updated,
		Operator: state.RuleOperator{
			Type: "list",
			Children: []state.RuleOperator{{
				Type: "simple", Operand: "process.path", Data: "/usr/bin/curl", Sensitive: true,
			}, {
				Type: "list",
				Children: []state.RuleOperator{{
					Type: "simple", Operand: "dest.host", Data: "example.com",
				}},
			}},
		},
	}}

	dir, err := service.Export(context.Background(), node, rules)
	if err != nil {
		t.Fatalf("Export error: %v", err)
	}
	if !strings.HasPrefix(dir, root+string(filepath.Separator)) || strings.Contains(filepath.Base(dir), "..") {
		t.Fatalf("expected sanitized node directory under root, got %q", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir error: %v", err)
	}
	if len(entries) != 1 || strings.Contains(entries[0].Name(), "/") || strings.Contains(entries[0].Name(), "..") {
		t.Fatalf("expected one sanitized archive filename, got %+v", entries)
	}
	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if !strings.Contains(string(data), `"name": "../allow curl"`) || !strings.Contains(string(data), "\n  \"") {
		t.Fatalf("expected canonical name and indented JSON, got %s", data)
	}

	imported, resolved, err := service.Import(context.Background(), node)
	if err != nil {
		t.Fatalf("Import error: %v", err)
	}
	if resolved != dir || len(imported) != 1 {
		t.Fatalf("unexpected import result: dir=%q rules=%+v", resolved, imported)
	}
	want := rules[0]
	want.CreatedAt = created.UTC()
	want.UpdatedAt = updated.UTC()
	if !reflect.DeepEqual(imported[0], want) {
		t.Fatalf("round trip mismatch\nwant: %#v\ngot:  %#v", want, imported[0])
	}
}

func TestImportAcceptsLegacyUnixTimestamp(t *testing.T) {
	service, _ := newTestService(t, DefaultLimits())
	node := state.Node{ID: "node-1", Name: "alpha"}
	dir := prepareNodeDir(t, service, node)
	writeArchive(t, filepath.Join(dir, "legacy.json"), `{
  "created": 1704067200,
  "updated": "2024-01-02T03:04:05Z",
  "name": "legacy",
  "action": "allow",
  "duration": "always",
  "operator": {"type": "simple", "operand": "process.path", "data": "/bin/true"},
  "enabled": true,
  "precedence": false,
  "nolog": false
}`)

	rules, _, err := service.Import(context.Background(), node)
	if err != nil {
		t.Fatalf("Import error: %v", err)
	}
	if got := rules[0].CreatedAt.UTC(); !got.Equal(time.Unix(1704067200, 0).UTC()) {
		t.Fatalf("unexpected legacy timestamp %s", got)
	}
	if got := rules[0].UpdatedAt.UTC(); !got.Equal(time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("unexpected RFC3339 timestamp %s", got)
	}
}

func TestExportPermissionsAndAtomicReplacement(t *testing.T) {
	service, _ := newTestService(t, DefaultLimits())
	node := state.Node{ID: "node-1", Name: "alpha"}
	rule := validRule("one")
	dir, err := service.Export(context.Background(), node, []state.Rule{rule})
	if err != nil {
		t.Fatalf("first Export error: %v", err)
	}
	assertMode(t, dir, 0o700)
	entries, _ := os.ReadDir(dir)
	path := filepath.Join(dir, entries[0].Name())
	assertMode(t, path, 0o600)

	rule.Description = "replacement"
	if _, err := service.Export(context.Background(), node, []state.Rule{rule}); err != nil {
		t.Fatalf("replacement Export error: %v", err)
	}
	entries, _ = os.ReadDir(dir)
	if len(entries) != 1 || strings.Contains(entries[0].Name(), ".tmp") {
		t.Fatalf("expected atomic replacement without temporary files, got %+v", entries)
	}
	data, _ := os.ReadFile(path)
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil || decoded["description"] != "replacement" {
		t.Fatalf("expected complete replacement JSON, got %s (err=%v)", data, err)
	}
}

func TestImportRejectsSymlinkAndNonRegularJSON(t *testing.T) {
	for _, test := range []struct {
		name string
		make func(*testing.T, string)
		want string
	}{
		{
			name: "symlink",
			make: func(t *testing.T, dir string) {
				target := filepath.Join(dir, "target.txt")
				writeArchive(t, target, `{}`)
				if err := os.Symlink(target, filepath.Join(dir, "linked.json")); err != nil {
					t.Fatalf("Symlink error: %v", err)
				}
			},
			want: "symlink",
		},
		{
			name: "directory",
			make: func(t *testing.T, dir string) {
				if err := os.Mkdir(filepath.Join(dir, "nested.json"), 0o700); err != nil {
					t.Fatalf("Mkdir error: %v", err)
				}
			},
			want: "non-regular",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, _ := newTestService(t, DefaultLimits())
			node := state.Node{ID: "node-1", Name: test.name}
			dir := prepareNodeDir(t, service, node)
			test.make(t, dir)
			_, _, err := service.Import(context.Background(), node)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
		})
	}
}

func TestImportBounds(t *testing.T) {
	t.Run("file count", func(t *testing.T) {
		limits := DefaultLimits()
		limits.MaxFiles = 1
		service, _ := newTestService(t, limits)
		node := state.Node{ID: "node-1", Name: "count"}
		dir := prepareNodeDir(t, service, node)
		writeArchive(t, filepath.Join(dir, "one.json"), validArchiveJSON("one"))
		writeArchive(t, filepath.Join(dir, "two.json"), validArchiveJSON("two"))
		_, _, err := service.Import(context.Background(), node)
		if err == nil || !strings.Contains(err.Error(), "limit is 1") {
			t.Fatalf("expected count limit error, got %v", err)
		}
	})

	t.Run("file size", func(t *testing.T) {
		limits := DefaultLimits()
		limits.MaxFileBytes = 64
		service, _ := newTestService(t, limits)
		node := state.Node{ID: "node-1", Name: "size"}
		dir := prepareNodeDir(t, service, node)
		writeArchive(t, filepath.Join(dir, "large.json"), validArchiveJSON("large"))
		_, _, err := service.Import(context.Background(), node)
		if err == nil || !strings.Contains(err.Error(), "limit is 64") {
			t.Fatalf("expected file size error, got %v", err)
		}
	})

	t.Run("total size", func(t *testing.T) {
		limits := DefaultLimits()
		limits.MaxTotalBytes = int64(len(validArchiveJSON("one")) + 8)
		service, _ := newTestService(t, limits)
		node := state.Node{ID: "node-1", Name: "total"}
		dir := prepareNodeDir(t, service, node)
		writeArchive(t, filepath.Join(dir, "one.json"), validArchiveJSON("one"))
		writeArchive(t, filepath.Join(dir, "two.json"), validArchiveJSON("two"))
		_, _, err := service.Import(context.Background(), node)
		if err == nil || !strings.Contains(err.Error(), "total size limit") {
			t.Fatalf("expected total size error, got %v", err)
		}
	})
}

func TestImportRejectsDuplicateMalformedAndUnsafeRules(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name: "duplicate",
			files: map[string]string{
				"one.json": validArchiveJSON("same"),
				"two.json": validArchiveJSON("same"),
			},
			want: `duplicate rule name "same"`,
		},
		{
			name:  "malformed",
			files: map[string]string{"bad.json": `{"name":`},
			want:  "parse bad.json",
		},
		{
			name:  "empty name",
			files: map[string]string{"bad.json": strings.Replace(validArchiveJSON("same"), `"name":"same"`, `"name":""`, 1)},
			want:  "rule name is empty",
		},
		{
			name:  "unsafe operator",
			files: map[string]string{"bad.json": strings.Replace(validArchiveJSON("same"), `"/bin/true"`, `"/bin/\u0000true"`, 1)},
			want:  "unsafe control data",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, _ := newTestService(t, DefaultLimits())
			node := state.Node{ID: "node-1", Name: test.name}
			dir := prepareNodeDir(t, service, node)
			for name, data := range test.files {
				writeArchive(t, filepath.Join(dir, name), data)
			}
			_, _, err := service.Import(context.Background(), node)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
		})
	}
}

func TestImportHonorsCanceledContext(t *testing.T) {
	service, _ := newTestService(t, DefaultLimits())
	node := state.Node{ID: "node-1", Name: "cancel"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := service.Import(ctx, node)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func newTestService(t *testing.T, limits Limits) (*Service, string) {
	t.Helper()
	root, err := os.MkdirTemp(".", ".rulearchive-test-")
	if err != nil {
		t.Fatalf("MkdirTemp error: %v", err)
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("Abs error: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("RemoveAll error: %v", err)
		}
	})
	service, err := New(absolute, limits)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}
	return service, absolute
}

func prepareNodeDir(t *testing.T, service *Service, node state.Node) string {
	t.Helper()
	dir := service.Directory(node)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}
	return dir
}

func writeArchive(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile error: %v", err)
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

func validRule(name string) state.Rule {
	return state.Rule{
		Name:     name,
		Action:   "allow",
		Duration: "always",
		Enabled:  true,
		Operator: state.RuleOperator{Type: "simple", Operand: "process.path", Data: "/bin/true"},
	}
}

func validArchiveJSON(name string) string {
	return `{"name":"` + name + `","action":"allow","duration":"always","operator":{"type":"simple","operand":"process.path","data":"/bin/true"},"enabled":true,"precedence":false,"nolog":false}`
}
