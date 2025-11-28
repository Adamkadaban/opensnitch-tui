package rules

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
)

func TestSubmitEditRejectsEmptyOptions(t *testing.T) {
	origActions, origDurations := ruleActionOptions, ruleDurationOptions
	defer func() { ruleActionOptions, ruleDurationOptions = origActions, origDurations }()

	ruleActionOptions = nil
	store := state.NewStore()
	node := state.Node{ID: "node-1", Name: "alpha"}
	store.SetNodes([]state.Node{node})
	store.SetRules(node.ID, []state.Rule{{Name: "r1", Action: "allow", Duration: "once", Enabled: true}})

	m := New(store, theme.New(theme.Options{}), &recordingRuleManager{}).(*Model)
	m.nodeIdx = 0
	m.editRuleName = "r1"
	m.editInputs = []textinput.Model{textinput.New()}

	m.submitEdit(store.Snapshot())

	if m.statusLine == "" || !strings.Contains(m.statusLine, "No action options") {
		t.Fatalf("expected statusLine to mention missing action options, got %q", m.statusLine)
	}

	ruleActionOptions = origActions
	ruleDurationOptions = nil
	m.statusLine = ""

	m.submitEdit(store.Snapshot())
	if m.statusLine == "" || !strings.Contains(m.statusLine, "No duration options") {
		t.Fatalf("expected statusLine to mention missing duration options, got %q", m.statusLine)
	}
}

func TestSubmitEditWrapsIndicesAndPreservesName(t *testing.T) {
	store := state.NewStore()
	node := state.Node{ID: "node-1", Name: "alpha"}
	store.SetNodes([]state.Node{node})
	store.SetRules(node.ID, []state.Rule{{Name: "r1", Action: "allow", Duration: "once", Enabled: true}})

	rec := &recordingRuleManager{}
	m := New(store, theme.New(theme.Options{}), rec).(*Model)
	m.nodeIdx = 0
	m.ruleIdx = 0
	m.editRuleName = "r1"
	m.editInputs = []textinput.Model{textinput.New()}
	m.editInputs[0].SetValue("updated desc")
	m.editActionIdx = 100 // wraps to index 1 => deny
	m.editDurIdx = -5     // wraps to index 1 => until restart
	m.editNoLog = true
	m.editPrecedence = true

	m.submitEdit(store.Snapshot())

	if rec.last == nil {
		t.Fatalf("expected ChangeRule to be called")
	}
	if rec.last.Name != "r1" {
		t.Fatalf("expected rule name preserved, got %q", rec.last.Name)
	}
	if rec.last.Description != "updated desc" {
		t.Fatalf("expected description to update, got %q", rec.last.Description)
	}
	if rec.last.Action != "deny" {
		t.Fatalf("expected wrapped action 'deny', got %q", rec.last.Action)
	}
	if rec.last.Duration != "until restart" {
		t.Fatalf("expected wrapped duration 'until restart', got %q", rec.last.Duration)
	}
	if !rec.last.NoLog || !rec.last.Precedence {
		t.Fatalf("expected NoLog and Precedence to be true")
	}
}

type recordingRuleManager struct {
	last *state.Rule
}

func (r *recordingRuleManager) EnableRule(string, string) error  { return nil }
func (r *recordingRuleManager) DisableRule(string, string) error { return nil }
func (r *recordingRuleManager) DeleteRule(string, string) error  { return nil }
func (r *recordingRuleManager) ChangeRule(_ string, rule state.Rule) error {
	ruleCopy := rule
	r.last = &ruleCopy
	return nil
}
