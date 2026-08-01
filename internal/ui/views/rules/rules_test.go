package rules

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	"github.com/adamkadaban/opensnitch-tui/internal/rulearchive"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
)

type fakeRuleController struct {
	action   string
	nodeID   string
	ruleName string
	rule     state.Rule
	rules    []state.Rule
	err      error
}

func (f *fakeRuleController) EnableRule(nodeID, ruleName string) error {
	f.action = "enable"
	f.nodeID = nodeID
	f.ruleName = ruleName
	return f.err
}

func (f *fakeRuleController) DisableRule(nodeID, ruleName string) error {
	f.action = "disable"
	f.nodeID = nodeID
	f.ruleName = ruleName
	return f.err
}

func (f *fakeRuleController) DeleteRule(nodeID, ruleName string) error {
	f.action = "delete"
	f.nodeID = nodeID
	f.ruleName = ruleName
	return f.err
}

func (f *fakeRuleController) ChangeRule(nodeID string, rule state.Rule) error {
	f.action = "change"
	f.nodeID = nodeID
	f.ruleName = rule.Name
	f.rule = rule
	return f.err
}

func (f *fakeRuleController) ApplyRules(_ context.Context, nodeID string, rules []state.Rule) error {
	f.action = "apply"
	f.nodeID = nodeID
	f.rules = append([]state.Rule(nil), rules...)
	if len(rules) > 0 {
		f.ruleName = rules[0].Name
		f.rule = rules[0]
	}
	return f.err
}

var _ controller.RuleManager = (*fakeRuleController)(nil)
var _ controller.RuleBatchManager = (*fakeRuleController)(nil)

type fakeRuleArchive struct {
	path          string
	imported      []state.Rule
	importErr     error
	exportErr     error
	exportedNode  state.Node
	exportedRules []state.Rule
}

func (f *fakeRuleArchive) Directory(state.Node) string {
	return f.path
}

func (f *fakeRuleArchive) Export(_ context.Context, node state.Node, rules []state.Rule) (string, error) {
	f.exportedNode = node
	f.exportedRules = append([]state.Rule(nil), rules...)
	return f.path, f.exportErr
}

func (f *fakeRuleArchive) Import(_ context.Context, _ state.Node) ([]state.Rule, string, error) {
	return append([]state.Rule(nil), f.imported...), f.path, f.importErr
}

var _ controller.RuleArchive = (*fakeRuleArchive)(nil)

func TestRulesViewEmpty(t *testing.T) {
	store := state.NewStore()
	view := New(store, theme.New(theme.Options{}), nil)
	view.SetSize(80, 20)

	if out := view.View(); !strings.Contains(out, "No nodes connected") {
		t.Fatalf("expected empty copy, got %q", out)
	}
}

func TestRulesEnableAction(t *testing.T) {
	store := state.NewStore()
	store.SetNodes([]state.Node{{ID: "node-1", Name: "alpha", Address: "10.0.0.2"}})
	store.SetRules("node-1", []state.Rule{{Name: "ssh", Action: "allow", Duration: "once", Operator: state.RuleOperator{Type: "process"}}})
	ctrl := &fakeRuleController{}
	view := New(store, theme.New(theme.Options{}), ctrl)
	view.SetSize(80, 25)

	view.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})

	if ctrl.action != "enable" || ctrl.ruleName != "ssh" {
		t.Fatalf("expected enable action for ssh, got %+v", ctrl)
	}
	if out := strings.ToLower(view.View()); !strings.Contains(out, "requested enable") {
		t.Fatalf("expected status line after enable, got %q", out)
	}
}

func TestRulesDeleteAction(t *testing.T) {
	store := state.NewStore()
	store.SetNodes([]state.Node{{ID: "node-1", Name: "alpha", Address: "10.0.0.2"}})
	store.SetRules("node-1", []state.Rule{{Name: "ssh", Operator: state.RuleOperator{Type: "process"}}})
	ctrl := &fakeRuleController{}
	view := New(store, theme.New(theme.Options{}), ctrl)
	view.SetSize(80, 25)

	view.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})

	if ctrl.action != "delete" {
		t.Fatalf("expected delete action, got %s", ctrl.action)
	}
	if out := strings.ToLower(view.View()); !strings.Contains(out, "requested delete") {
		t.Fatalf("expected status message after delete, got %q", out)
	}
}

func TestRulesModifyAction(t *testing.T) {
	store := state.NewStore()
	store.SetNodes([]state.Node{{ID: "node-1", Name: "alpha", Address: "10.0.0.2"}})
	store.SetRules("node-1", []state.Rule{{Name: "ssh", Action: "allow", Duration: "once", Description: "orig"}})
	ctrl := &fakeRuleController{}
	view := New(store, theme.New(theme.Options{}), ctrl)
	view.SetSize(80, 25)

	view.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	view.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if ctrl.action != "change" || ctrl.ruleName != "ssh" {
		t.Fatalf("expected change action for ssh, got %+v", ctrl)
	}
}

func TestRulesModifyNavigateFieldsWithArrows(t *testing.T) {
	store := state.NewStore()
	store.SetNodes([]state.Node{{ID: "node-1", Name: "alpha", Address: "10.0.0.2"}})
	store.SetRules("node-1", []state.Rule{{Name: "ssh", Action: "allow"}})
	ctrl := &fakeRuleController{}
	view := New(store, theme.New(theme.Options{}), ctrl)
	view.SetSize(80, 25)

	view.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	m, ok := view.(*Model)
	if !ok {
		t.Fatalf("expected *Model, got %T", view)
	}
	if m.editFocus != 0 {
		t.Fatalf("expected initial editFocus 0, got %d", m.editFocus)
	}
	// Up from first wraps to last
	view.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.editFocus != editFieldCount-1 {
		t.Fatalf("expected editFocus wrap to last, got %d", m.editFocus)
	}
	// Down moves back to first
	view.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.editFocus != 0 {
		t.Fatalf("expected editFocus move to 0, got %d", m.editFocus)
	}
}

func TestRulesCopyRunsAsynchronouslyWithAvailableName(t *testing.T) {
	store := state.NewStore()
	node := state.Node{ID: "node-1", Name: "alpha", Status: state.NodeStatusReady}
	store.SetNodes([]state.Node{node})
	store.SetRules(node.ID, []state.Rule{
		{Name: "curl", Action: "allow", Duration: "always", CreatedAt: time.Now(), UpdatedAt: time.Now(), Operator: state.RuleOperator{Type: "simple"}},
		{Name: "curl-copy-1", Action: "allow", Duration: "always", Operator: state.RuleOperator{Type: "simple"}},
	})
	ctrl := &fakeRuleController{}
	view := New(store, theme.New(theme.Options{}), ctrl)
	view.SetSize(80, 40)

	_, cmd := view.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if cmd == nil {
		t.Fatal("expected asynchronous copy command")
	}
	if out := view.View(); !strings.Contains(out, "Copying curl as curl-copy-2") {
		t.Fatalf("expected copy in-progress feedback, got %q", out)
	}
	msg := cmd()
	view.Update(msg)
	if ctrl.action != "apply" || ctrl.ruleName != "curl-copy-2" {
		t.Fatalf("unexpected batch copy request: %+v", ctrl)
	}
	if !ctrl.rule.CreatedAt.IsZero() {
		t.Fatalf("expected copied rule creation timestamp to be cleared, got %s", ctrl.rule.CreatedAt)
	}
	if !ctrl.rule.UpdatedAt.IsZero() {
		t.Fatalf("expected copied rule update timestamp to be cleared, got %s", ctrl.rule.UpdatedAt)
	}
	if out := view.View(); !strings.Contains(out, "Copied rule as curl-copy-2") {
		t.Fatalf("expected copy success feedback, got %q", out)
	}
}

func TestRulesImportAndExportRunAsynchronously(t *testing.T) {
	store := state.NewStore()
	node := state.Node{ID: "node-1", Name: "alpha", Status: state.NodeStatusReady}
	store.SetNodes([]state.Node{node})
	store.SetRules(node.ID, []state.Rule{{
		Name: "current", Action: "allow", Duration: "always", Operator: state.RuleOperator{Type: "simple"},
	}})
	ctrl := &fakeRuleController{}
	archive := &fakeRuleArchive{
		path: "/safe/archive/alpha-1234",
		imported: []state.Rule{{
			Name: "imported", Action: "deny", Duration: "always", Operator: state.RuleOperator{Type: "simple"},
		}},
	}
	view := New(store, theme.New(theme.Options{}), ctrl, archive)
	view.SetSize(120, 40)

	_, importCmd := view.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if importCmd == nil || !strings.Contains(view.View(), archive.path) {
		t.Fatalf("expected import command and resolved directory feedback, got %q", view.View())
	}
	view.Update(importCmd())
	if len(ctrl.rules) != 1 || ctrl.rules[0].Name != "imported" {
		t.Fatalf("unexpected imported batch: %+v", ctrl.rules)
	}
	if out := view.View(); !strings.Contains(out, "Imported 1 rule(s)") || !strings.Contains(out, archive.path) {
		t.Fatalf("expected import success feedback, got %q", out)
	}

	_, exportCmd := view.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if exportCmd == nil {
		t.Fatal("expected asynchronous export command")
	}
	view.Update(exportCmd())
	if archive.exportedNode.ID != node.ID || len(archive.exportedRules) != 1 {
		t.Fatalf("unexpected export request: node=%+v rules=%+v", archive.exportedNode, archive.exportedRules)
	}
	if out := view.View(); !strings.Contains(out, "Exported 1 rule(s)") || !strings.Contains(out, archive.path) {
		t.Fatalf("expected export success feedback, got %q", out)
	}
}

func TestRulesImportErrorReportsPartialProgress(t *testing.T) {
	store := state.NewStore()
	node := state.Node{ID: "node-1", Name: "alpha", Status: state.NodeStatusReady}
	store.SetNodes([]state.Node{node})
	ctrl := &fakeRuleController{
		err: errors.New(`rule 2 "second" failed after 1 of 2 rules applied: rule rejected`),
	}
	archive := &fakeRuleArchive{
		path: "/safe/archive/alpha-1234",
		imported: []state.Rule{
			{Name: "first", Action: "allow", Duration: "always", Operator: state.RuleOperator{Type: "simple"}},
			{Name: "second", Action: "deny", Duration: "always", Operator: state.RuleOperator{Type: "simple"}},
		},
	}
	view := New(store, theme.New(theme.Options{}), ctrl, archive)
	view.SetSize(120, 40)

	_, cmd := view.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if cmd == nil {
		t.Fatal("expected asynchronous import command")
	}
	view.Update(cmd())
	out := view.View()
	if !strings.Contains(out, "Import failed for alpha") ||
		!strings.Contains(out, `rule 2 "second"`) ||
		!strings.Contains(out, "1 of 2 rules applied") {
		t.Fatalf("expected partial import progress feedback, got %q", out)
	}
}

func TestRulesImportDoesNotApplyUnsafeNestedAllowRule(t *testing.T) {
	root, err := os.MkdirTemp(".", ".rules-archive-test-")
	if err != nil {
		t.Fatalf("MkdirTemp error: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("RemoveAll error: %v", err)
		}
	})
	root, err = filepath.Abs(root)
	if err != nil {
		t.Fatalf("Abs error: %v", err)
	}
	archive, err := rulearchive.New(root, rulearchive.DefaultLimits())
	if err != nil {
		t.Fatalf("New archive error: %v", err)
	}
	node := state.Node{ID: "node-1", Name: "alpha", Status: state.NodeStatusReady}
	dir := archive.Directory(node)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}
	data := `{
  "name": "unsafe-allow",
  "action": "allow",
  "duration": "always",
  "operator": {"type": "list", "list": [{"type": "list"}]},
  "enabled": true,
  "precedence": false,
  "nolog": false
}`
	if err := os.WriteFile(filepath.Join(dir, "unsafe.json"), []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	store := state.NewStore()
	store.SetNodes([]state.Node{node})
	ctrl := &fakeRuleController{}
	view := New(store, theme.New(theme.Options{}), ctrl, archive)
	view.SetSize(120, 40)
	_, cmd := view.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	if cmd == nil {
		t.Fatal("expected import command")
	}
	view.Update(cmd())
	if len(ctrl.rules) != 0 || ctrl.action == "apply" {
		t.Fatalf("unsafe allow rule reached ApplyRules: action=%q rules=%+v", ctrl.action, ctrl.rules)
	}
	if out := view.View(); !strings.Contains(out, "OpenSnitch v1.8") {
		t.Fatalf("expected v1.8 incompatibility feedback, got %q", out)
	}
}

func TestRulesArchiveActionsRejectInvalidSelections(t *testing.T) {
	t.Run("no node", func(t *testing.T) {
		view := New(state.NewStore(), theme.New(theme.Options{}), &fakeRuleController{}, &fakeRuleArchive{path: "/safe/archive"})
		_, cmd := view.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
		if cmd != nil || !strings.Contains(view.View(), "No node selected") {
			t.Fatalf("expected no-node rejection, got cmd=%v output=%q", cmd != nil, view.View())
		}
	})

	t.Run("disconnected", func(t *testing.T) {
		store := state.NewStore()
		node := state.Node{ID: "node-1", Name: "alpha", Status: state.NodeStatusDisconnected}
		store.SetNodes([]state.Node{node})
		store.SetRules(node.ID, []state.Rule{{Name: "one"}})
		view := New(store, theme.New(theme.Options{}), &fakeRuleController{}, &fakeRuleArchive{path: "/safe/archive"})
		_, cmd := view.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
		if cmd != nil || !strings.Contains(view.View(), "not connected") {
			t.Fatalf("expected disconnected rejection, got cmd=%v output=%q", cmd != nil, view.View())
		}
	})

	t.Run("no rules", func(t *testing.T) {
		store := state.NewStore()
		node := state.Node{ID: "node-1", Name: "alpha", Status: state.NodeStatusReady}
		store.SetNodes([]state.Node{node})
		view := New(store, theme.New(theme.Options{}), &fakeRuleController{}, &fakeRuleArchive{path: "/safe/archive"})
		for _, key := range []rune{'c', 'o'} {
			_, cmd := view.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
			if cmd != nil || !strings.Contains(view.View(), "No rules available") {
				t.Fatalf("expected no-rules rejection for %q, got cmd=%v output=%q", key, cmd != nil, view.View())
			}
		}
	})
}

func TestAvailableCopyName(t *testing.T) {
	rules := []state.Rule{{Name: "base-copy-1"}, {Name: "base-copy-3"}, {Name: "base"}}
	if got := availableCopyName("base", rules); got != "base-copy-2" {
		t.Fatalf("availableCopyName = %q, want base-copy-2", got)
	}
}

func TestRulesArchiveFeedbackFitsTerminalBounds(t *testing.T) {
	for _, size := range []struct{ width, height int }{{80, 40}, {120, 40}} {
		store := state.NewStore()
		node := state.Node{ID: "node-1", Name: "alpha", Status: state.NodeStatusReady}
		store.SetNodes([]state.Node{node})
		store.SetRules(node.ID, makeTestRules(10))
		archive := &fakeRuleArchive{path: "/home/user/.local/share/opensnitch-tui/rules/alpha-1234567890"}
		view := New(store, theme.New(theme.Options{}), &fakeRuleController{}, archive)
		view.SetSize(size.width, size.height)
		view.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
		output := view.View()
		if got := lipgloss.Width(output); got > size.width {
			t.Fatalf("%dx%d output width = %d", size.width, size.height, got)
		}
		if got := lipgloss.Height(output); got > size.height {
			t.Fatalf("%dx%d output height = %d", size.width, size.height, got)
		}
	}
}
func TestRulesTableWindowing(t *testing.T) {
	store := state.NewStore()
	node := state.Node{ID: "node-1", Name: "alpha"}
	store.SetNodes([]state.Node{node})
	store.SetRules(node.ID, makeTestRules(10))
	view := New(store, theme.New(theme.Options{}), nil)
	view.SetSize(80, 10)

	initial := view.View()
	if !strings.Contains(initial, "NAME") || !strings.Contains(initial, "OPERATOR") {
		t.Fatalf("expected header labels in table, got %q", initial)
	}
	if !strings.Contains(initial, "rule-00") || !strings.Contains(initial, "rule-02") {
		t.Fatalf("expected first window to show leading rules, got %q", initial)
	}
	if strings.Contains(initial, "rule-06") {
		t.Fatalf("expected later rules to be clipped initially, got %q", initial)
	}

	for i := 0; i < 7; i++ {
		view.Update(tea.KeyMsg{Type: tea.KeyDown})
	}

	out := view.View()
	if strings.Contains(out, "rule-00") {
		t.Fatalf("expected earlier rules to scroll out of view, still saw rule-00: %q", out)
	}
	if !strings.Contains(out, "rule-07") {
		t.Fatalf("expected current selection to be visible after scrolling, got %q", out)
	}
	if !strings.Contains(out, "Name: rule-07") {
		t.Fatalf("expected detail section with rule name, got %q", out)
	}
}

func TestRulesTableCapacityClamp(t *testing.T) {
	model := New(state.NewStore(), theme.New(theme.Options{}), nil).(*Model)
	model.SetSize(80, 0)
	if capacity := model.tableCapacity(); capacity != 5 {
		t.Fatalf("expected default capacity fallback of 5, got %d", capacity)
	}
	model.SetSize(80, 7)
	if capacity := model.tableCapacity(); capacity != 3 {
		t.Fatalf("expected minimum capacity of 3, got %d", capacity)
	}
	model.SetSize(80, 22)
	if capacity := model.tableCapacity(); capacity != 8 {
		t.Fatalf("expected capped capacity of 8, got %d", capacity)
	}
}

func TestRuleDetailShowsAllFields(t *testing.T) {
	store := state.NewStore()
	node := state.Node{ID: "node-1", Name: "alpha"}
	store.SetNodes([]state.Node{node})
	store.SetRules(node.ID, []state.Rule{makeTestRules(1)[0]})
	view := New(store, theme.New(theme.Options{}), nil)
	view.SetSize(90, 12)
	out := view.View()
	checks := []string{
		"Name:",
		"Node:",
		"Description:",
		"Action:",
		"Duration:",
		"Enabled: true",
		"Precedence:",
		"NoLog:",
		"Created:",
		"Operator:",
	}
	for _, token := range checks {
		if !strings.Contains(out, token) {
			t.Fatalf("expected detail output to contain %q, got %q", token, out)
		}
	}
}

func TestRulesTableMaxRows(t *testing.T) {
	store := state.NewStore()
	node := state.Node{ID: "node-1", Name: "alpha"}
	store.SetNodes([]state.Node{node})
	store.SetRules(node.ID, makeTestRules(20))
	view := New(store, theme.New(theme.Options{}), nil)
	view.SetSize(120, 40)
	out := view.View()
	if strings.Contains(out, "rule-15") {
		t.Fatalf("expected far rules to be clipped despite tall viewport, got %q", out)
	}
	rows := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "rule-") && strings.Contains(line, "allow") && strings.Contains(line, "always") {
			rows++
		}
	}
	if rows > 8 {
		t.Fatalf("expected at most 8 table rows, saw %d\noutput: %s", rows, out)
	}
	if strings.Contains(out, "rule-08") {
		t.Fatalf("expected window of first 8 rows only, saw rule-08 present: %q", out)
	}
}

func TestRulesTableHeaderPresence(t *testing.T) {
	store := state.NewStore()
	node := state.Node{ID: "node-1", Name: "alpha"}
	store.SetNodes([]state.Node{node})
	store.SetRules(node.ID, []state.Rule{makeTestRules(1)[0]})
	view := New(store, theme.New(theme.Options{}), nil)
	view.SetSize(90, 12)
	lines := strings.Split(view.View(), "\n")
	headerFound := false
	for _, line := range lines {
		if strings.Contains(line, "NAME") && strings.Contains(line, "OPERATOR") {
			headerFound = true
			break
		}
	}
	if !headerFound {
		t.Fatalf("expected table header line with labels, got %q", view.View())
	}
}

func TestRulesTableShowsFlagsAndOperator(t *testing.T) {
	store := state.NewStore()
	node := state.Node{ID: "node-1", Name: "alpha"}
	store.SetNodes([]state.Node{node})
	rule := state.Rule{
		NodeID:      node.ID,
		Name:        "rule-main",
		Description: "desc",
		Action:      "allow",
		Duration:    "always",
		Enabled:     true,
		Precedence:  true,
		NoLog:       false,
		Operator: state.RuleOperator{
			Type:    "process",
			Operand: "/usr/bin/foo",
		},
	}
	store.SetRules(node.ID, []state.Rule{rule})
	view := New(store, theme.New(theme.Options{}), nil)
	view.SetSize(100, 12)
	var row string
	for _, line := range strings.Split(view.View(), "\n") {
		if strings.Contains(line, "rule-main") {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatalf("expected to find row for rule-main")
	}
	if !strings.Contains(row, "yes") || !strings.Contains(row, "no") {
		t.Fatalf("expected precedence yes and nolog no columns, got %q", row)
	}
	if !strings.Contains(row, "process") {
		t.Fatalf("expected operator column to include process descriptor, got %q", row)
	}
}

func makeTestRules(count int) []state.Rule {
	rules := make([]state.Rule, count)
	base := time.Date(2024, time.January, 1, 13, 0, 0, 0, time.UTC)
	for i := 0; i < count; i++ {
		rules[i] = state.Rule{
			NodeID:      "node-1",
			Name:        fmt.Sprintf("rule-%02d", i),
			Description: fmt.Sprintf("rule %d description", i),
			Action:      "allow",
			Duration:    "always",
			Enabled:     i%2 == 0,
			Precedence:  i%2 == 1,
			NoLog:       i%3 == 0,
			CreatedAt:   base.Add(time.Duration(i) * time.Hour),
			Operator: state.RuleOperator{
				Type:    "process",
				Operand: fmt.Sprintf("proc-%d", i),
			},
		}
	}
	return rules
}
