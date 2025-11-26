package rules

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
)

type fakeRuleController struct {
	action   string
	nodeID   string
	ruleName string
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

var _ controller.RuleManager = (*fakeRuleController)(nil)

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
		view.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
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
	if cap := model.tableCapacity(); cap != 5 {
		t.Fatalf("expected default capacity fallback of 5, got %d", cap)
	}
	model.SetSize(80, 7)
	if cap := model.tableCapacity(); cap != 3 {
		t.Fatalf("expected minimum capacity of 3, got %d", cap)
	}
	model.SetSize(80, 22)
	if cap := model.tableCapacity(); cap != 8 {
		t.Fatalf("expected capped capacity of 8, got %d", cap)
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
