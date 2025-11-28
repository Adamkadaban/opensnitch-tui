package rules

import (
	"path/filepath"
	"testing"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	"github.com/adamkadaban/opensnitch-tui/internal/ui/view/viewtest"
)

func TestRulesSnapshot(t *testing.T) {
	store := state.NewStore()
	node := state.Node{ID: "node-1", Name: "alpha", Address: "10.0.0.1"}
	store.SetNodes([]state.Node{node})
	store.SetRules(node.ID, []state.Rule{
		{Name: "allow-curl", Action: "allow", Duration: "once", Enabled: true, Operator: state.RuleOperator{Type: "process.path", Operand: "startswith", Data: "/usr/bin/curl"}},
		{Name: "deny-dns", Action: "deny", Duration: "always", Enabled: false, NoLog: true, Operator: state.RuleOperator{Type: "dest.host", Operand: "equals", Data: "example.org"}},
	})

	th := theme.New(theme.Options{})
	m := New(store, th, noopRuleManager{})
	m.SetSize(100, 20)

	viewtest.AssertSnapshot(t, m.View(), filepath.Join("testdata", "rules.snap"))
}

type noopRuleManager struct{}

func (noopRuleManager) EnableRule(string, string) error  { return nil }
func (noopRuleManager) DisableRule(string, string) error { return nil }
func (noopRuleManager) DeleteRule(string, string) error  { return nil }
func (noopRuleManager) ChangeRule(string, state.Rule) error {
	return nil
}
