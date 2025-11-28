package rules

import (
	"strings"
	"testing"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
	"github.com/adamkadaban/opensnitch-tui/internal/util"
)

func TestRulesTableTruncatesNameColumn(t *testing.T) {
	store := state.NewStore()
	node := state.Node{ID: "node-1", Name: "alpha"}
	store.SetNodes([]state.Node{node})
	// Long name, short operator
	store.SetRules(node.ID, []state.Rule{{
		Name:     "verylongrulename_that_should_truncate",
		Action:   "allow",
		Duration: "always",
		Operator: state.RuleOperator{Type: "simple", Operand: "process.path", Data: "/bin/echo"},
	}})
	model := New(store, theme.New(theme.Options{}), nil).(*Model)
	model.SetSize(80, 10)

	layout := model.tableColumns()
	table := model.renderRulesTable(store.Snapshot().Rules[node.ID])
	lines := strings.Split(table, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected header + at least one row, got: %v", lines)
	}
	row := util.StripANSI(lines[1])
	// name starts after cursor+gap
	nameStart := layout.cursor + columnGap
	nameEnd := nameStart + layout.name
	if nameEnd > len(row) {
		t.Fatalf("row too short: %q", row)
	}
	nameCell := strings.TrimSpace(row[nameStart:nameEnd])
	if !strings.Contains(nameCell, "...") {
		t.Fatalf("expected name column to be truncated with ellipsis, got: %q", nameCell)
	}
}
