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

func TestExportImportRoundTripImmediateList(t *testing.T) {
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
				Type: "simple", Operand: "dest.host", Data: "example.com",
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

func TestExportRejectsNestedListForOpenSnitchV18(t *testing.T) {
	service, _ := newTestService(t, DefaultLimits())
	node := state.Node{ID: "node-1", Name: "nested-export"}
	rule := validRule("nested")
	rule.Operator = state.RuleOperator{
		Type: "list",
		Children: []state.RuleOperator{{
			Type: "list",
			Children: []state.RuleOperator{{
				Type: "simple", Operand: "dest.host", Data: "example.com",
			}},
		}},
	}

	_, err := service.Export(context.Background(), node, []state.Rule{rule})
	if err == nil ||
		!strings.Contains(err.Error(), `rule "nested" operator`) ||
		!strings.Contains(err.Error(), "operator depth 2") ||
		!strings.Contains(err.Error(), "OpenSnitch v1.8") {
		t.Fatalf("expected rule, depth, and v1.8 incompatibility error, got %v", err)
	}
	if _, statErr := os.Lstat(service.Directory(node)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("unsafe export created node archive: %v", statErr)
	}
}

func TestImportRejectsUnsafeNestedAllowRule(t *testing.T) {
	service, _ := newTestService(t, DefaultLimits())
	node := state.Node{ID: "node-1", Name: "nested-import"}
	dir := prepareNodeDir(t, service, node)
	writeArchive(t, filepath.Join(dir, "unsafe.json"), `{
  "name": "unsafe-allow",
  "action": "allow",
  "duration": "always",
  "operator": {
    "type": "list",
    "list": [
      {"type": "simple", "operand": "process.path", "data": "/usr/bin/curl"},
      {"type": "list"}
    ]
  },
  "enabled": true,
  "precedence": false,
  "nolog": false
}`)

	rules, _, err := service.Import(context.Background(), node)
	if err == nil ||
		!strings.Contains(err.Error(), `rule "unsafe-allow" operator`) ||
		!strings.Contains(err.Error(), "operator depth 2") ||
		!strings.Contains(err.Error(), "OpenSnitch v1.8") {
		t.Fatalf("expected unsafe allow import rejection, got rules=%+v err=%v", rules, err)
	}
	if len(rules) != 0 {
		t.Fatalf("unsafe allow rule must not be returned for application: %+v", rules)
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

func TestExportPermissionsReplacementAndStaleRemoval(t *testing.T) {
	service, _ := newTestService(t, DefaultLimits())
	node := state.Node{ID: "node-1", Name: "alpha"}
	rule := validRule("one")
	dir, err := service.Export(context.Background(), node, []state.Rule{rule, validRule("stale")})
	if err != nil {
		t.Fatalf("first Export error: %v", err)
	}
	assertMode(t, dir, 0o700)
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		assertMode(t, filepath.Join(dir, entry.Name()), 0o600)
	}

	rule.Description = "replacement"
	if _, err := service.Export(context.Background(), node, []state.Rule{rule}); err != nil {
		t.Fatalf("replacement Export error: %v", err)
	}
	entries, _ = os.ReadDir(dir)
	if len(entries) != 1 || strings.Contains(entries[0].Name(), ".tmp") {
		t.Fatalf("expected replacement to remove stale files, got %+v", entries)
	}
	path := filepath.Join(dir, entries[0].Name())
	assertMode(t, dir, 0o700)
	assertMode(t, path, 0o600)
	data, _ := os.ReadFile(path)
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil || decoded["description"] != "replacement" {
		t.Fatalf("expected complete replacement JSON, got %s (err=%v)", data, err)
	}
	assertNoGenerationResidue(t, service.root)
}

func TestExportStageFailureLeavesExistingGenerationUnchanged(t *testing.T) {
	service, _ := newTestService(t, DefaultLimits())
	node := state.Node{ID: "node-1", Name: "stage-failure"}
	dir, err := service.Export(context.Background(), node, []state.Rule{validRule("old")})
	if err != nil {
		t.Fatalf("initial Export error: %v", err)
	}
	before := snapshotArchive(t, dir)
	service.exportHook = func(step exportStep) error {
		if step.phase == exportStepFileStaged && step.index == 0 {
			return errors.New("injected staged write failure")
		}
		return nil
	}

	_, err = service.Export(context.Background(), node, []state.Rule{validRule("new"), validRule("later")})
	if err == nil || !strings.Contains(err.Error(), "injected staged write failure") {
		t.Fatalf("expected injected staging failure, got %v", err)
	}
	assertArchiveSnapshot(t, dir, before)
	assertNoGenerationResidue(t, service.root)
}

func TestExportCancellationAfterStagedFileLeavesExistingGenerationUnchanged(t *testing.T) {
	service, _ := newTestService(t, DefaultLimits())
	node := state.Node{ID: "node-1", Name: "cancel-stage"}
	dir, err := service.Export(context.Background(), node, []state.Rule{validRule("old")})
	if err != nil {
		t.Fatalf("initial Export error: %v", err)
	}
	before := snapshotArchive(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	service.exportHook = func(step exportStep) error {
		if step.phase == exportStepFileStaged && step.index == 0 {
			cancel()
		}
		return nil
	}

	_, err = service.Export(ctx, node, []state.Rule{validRule("new"), validRule("later")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	assertArchiveSnapshot(t, dir, before)
	assertNoGenerationResidue(t, service.root)
}

func TestExportPublishFailuresRollBackExistingGeneration(t *testing.T) {
	for _, phase := range []string{exportStepBackupCreated, exportStepPublished} {
		t.Run(phase, func(t *testing.T) {
			service, _ := newTestService(t, DefaultLimits())
			node := state.Node{ID: "node-1", Name: "publish-failure"}
			dir, err := service.Export(context.Background(), node, []state.Rule{validRule("old")})
			if err != nil {
				t.Fatalf("initial Export error: %v", err)
			}
			before := snapshotArchive(t, dir)
			service.exportHook = func(step exportStep) error {
				if step.phase == phase {
					return errors.New("injected publish failure")
				}
				return nil
			}

			_, err = service.Export(context.Background(), node, []state.Rule{validRule("new")})
			if err == nil || !strings.Contains(err.Error(), "injected publish failure") {
				t.Fatalf("expected injected publish failure, got %v", err)
			}
			assertArchiveSnapshot(t, dir, before)
			assertNoGenerationResidue(t, service.root)
		})
	}
}

func TestExportRejectsUnsafeExistingTargets(t *testing.T) {
	tests := []struct {
		name string
		make func(*testing.T, *Service, state.Node)
		want string
	}{
		{
			name: "target symlink",
			make: func(t *testing.T, service *Service, node state.Node) {
				target := filepath.Join(service.root, "other")
				if err := os.Mkdir(target, 0o700); err != nil {
					t.Fatalf("Mkdir target: %v", err)
				}
				if err := os.Symlink(target, service.Directory(node)); err != nil {
					t.Fatalf("Symlink target: %v", err)
				}
			},
			want: "not a regular directory",
		},
		{
			name: "target file",
			make: func(t *testing.T, service *Service, node state.Node) {
				writeArchive(t, service.Directory(node), "not a directory")
			},
			want: "not a regular directory",
		},
		{
			name: "symlink file",
			make: func(t *testing.T, service *Service, node state.Node) {
				dir := prepareNodeDir(t, service, node)
				target := filepath.Join(service.root, "outside.json")
				writeArchive(t, target, validArchiveJSON("outside"))
				if err := os.Symlink(target, filepath.Join(dir, "linked.json")); err != nil {
					t.Fatalf("Symlink file: %v", err)
				}
			},
			want: "symlink archive file",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, _ := newTestService(t, DefaultLimits())
			node := state.Node{ID: "node-1", Name: test.name}
			test.make(t, service, node)
			_, err := service.Export(context.Background(), node, []state.Rule{validRule("new")})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q error, got %v", test.want, err)
			}
			assertNoGenerationResidue(t, service.root)
		})
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

func snapshotArchive(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", dir, err)
	}
	snapshot := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("ReadFile %s: %v", entry.Name(), err)
		}
		snapshot[entry.Name()] = data
	}
	return snapshot
}

func assertArchiveSnapshot(t *testing.T, dir string, want map[string][]byte) {
	t.Helper()
	if got := snapshotArchive(t, dir); !reflect.DeepEqual(got, want) {
		t.Fatalf("archive changed\nwant: %#v\ngot:  %#v", want, got)
	}
}

func assertNoGenerationResidue(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir root: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".stage-") || strings.Contains(entry.Name(), ".backup-") {
			t.Fatalf("unexpected rule archive generation residue: %s", entry.Name())
		}
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
