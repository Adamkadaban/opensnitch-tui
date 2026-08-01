package alerts

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/theme"
)

type fakeArchive struct {
	path     string
	err      error
	exported state.Alert
}

func (f *fakeArchive) Directory() string { return "/fixed/alerts" }

func (f *fakeArchive) Export(_ context.Context, alert state.Alert) (string, error) {
	f.exported = alert
	return f.path, f.err
}

func TestAlertsViewEmptyAndInvalidPayload(t *testing.T) {
	store := state.NewStore()
	model := newModel(store, nil)
	output := model.View()
	if !strings.Contains(output, "No alerts yet") {
		t.Fatalf("expected empty copy, got %q", output)
	}
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if cmd != nil || !strings.Contains(model.View(), "No alert selected") {
		t.Fatalf("expected safe empty export feedback, cmd=%v output=%q", cmd, model.View())
	}

	store.AddAlert(state.Alert{ID: "1", PayloadKind: state.AlertPayloadProcess})
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	output = model.View()
	if !strings.Contains(output, "Process payload unavailable") {
		t.Fatalf("expected safe nil payload copy, got %q", output)
	}
}

func TestAlertsSelectionDetailBackAndNoViBindings(t *testing.T) {
	store := state.NewStore()
	store.AddAlert(textAlert("older", "older text"))
	store.AddAlert(textAlert("newer", "newer text"))
	model := newModel(store, nil)

	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if model.rowIdx != 0 {
		t.Fatalf("vi key changed selection to %d", model.rowIdx)
	}
	model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if model.rowIdx != 1 {
		t.Fatalf("down did not select second alert: %d", model.rowIdx)
	}
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.detail || !strings.Contains(model.View(), "older text") {
		t.Fatalf("expected selected alert detail, got %q", model.View())
	}
	model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.detail {
		t.Fatal("escape did not return to list")
	}
}

func TestAlertsDeleteSelectedLocally(t *testing.T) {
	store := state.NewStore()
	store.AddAlert(textAlert("1", "one"))
	store.AddAlert(textAlert("2", "two"))
	model := newModel(store, nil)

	model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	alerts := store.Snapshot().Alerts
	if len(alerts) != 1 || alerts[0].ID != "2" {
		t.Fatalf("unexpected alerts after delete: %#v", alerts)
	}
	if !strings.Contains(model.View(), "Deleted selected alert locally") {
		t.Fatalf("missing delete feedback: %q", model.View())
	}
}

func TestAlertsExportSelected(t *testing.T) {
	store := state.NewStore()
	store.AddAlert(textAlert("1", "one"))
	archive := &fakeArchive{path: "/fixed/alerts/one.json"}
	model := newModel(store, archive)

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if cmd == nil {
		t.Fatal("expected export command")
	}
	model.Update(cmd())
	if archive.exported.ID != "1" {
		t.Fatalf("exported wrong alert: %#v", archive.exported)
	}
	if !strings.Contains(model.View(), archive.path) {
		t.Fatalf("missing export feedback: %q", model.View())
	}
}

func TestAlertsDetailRedactsEnvironmentAndAuthData(t *testing.T) {
	store := state.NewStore()
	store.AddAlert(state.Alert{
		ID: "1", PayloadKind: state.AlertPayloadProcess,
		Process: &state.Process{
			PID: 7, Comm: "curl",
			Args: []string{"curl", "--token=argument-secret"},
			Env:  map[string]string{"PASSWORD": "environment-secret"},
		},
	})
	model := newModel(store, nil)
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	output := model.View()
	for _, secret := range []string{"argument-secret", "environment-secret"} {
		if strings.Contains(output, secret) {
			t.Fatalf("detail leaked %q: %q", secret, output)
		}
	}
	if !strings.Contains(output, "[redacted]") || !strings.Contains(output, "values redacted") {
		t.Fatalf("expected redaction notices: %q", output)
	}
}

func TestAlertsRendersStructuredDetails(t *testing.T) {
	tests := []struct {
		name  string
		alert state.Alert
		want  []string
	}{
		{
			name: "connection",
			alert: state.Alert{PayloadKind: state.AlertPayloadConnection, Connection: &state.Connection{
				Protocol: "tcp", DstIP: "1.1.1.1", DstPort: 443, ProcessPath: "/usr/bin/curl",
			}},
			want: []string{"Protocol: tcp", "1.1.1.1:443"},
		},
		{
			name: "rule",
			alert: state.Alert{PayloadKind: state.AlertPayloadRule, Rule: &state.Rule{
				Name: "allow-web", Operator: state.RuleOperator{
					Type: "simple", Operand: "password", Data: "rule-secret", Sensitive: true,
				},
			}},
			want: []string{"allow-web", "[redacted]"},
		},
		{
			name: "firewall",
			alert: state.Alert{PayloadKind: state.AlertPayloadFirewall, FirewallRule: &state.FirewallRule{
				Table: "filter", Chain: "output", Target: "accept",
				Expressions: []state.FirewallExpression{{Statement: &state.FirewallStatement{
					Op: "match", Name: "tcp", Values: []state.FirewallStatementValue{{Key: "dport", Value: "443"}},
				}}},
			}},
			want: []string{"filter / output", "dport=443"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := state.NewStore()
			test.alert.ID = test.name
			store.AddAlert(test.alert)
			model := newModel(store, nil)
			model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			output := model.View()
			for _, want := range test.want {
				if !strings.Contains(output, want) {
					t.Fatalf("expected %q in %q", want, output)
				}
			}
			if strings.Contains(output, "rule-secret") {
				t.Fatalf("sensitive operator leaked: %q", output)
			}
		})
	}
}

func TestAlertsTerminalBounds(t *testing.T) {
	store := state.NewStore()
	long := strings.Repeat("wide payload ", 200)
	for i := 0; i < 50; i++ {
		store.AddAlert(state.Alert{
			ID: "alert", NodeID: long, Text: long,
			Priority: state.AlertPriorityHigh, Type: state.AlertTypeWarning, What: state.AlertWhatGeneric,
			PayloadKind: state.AlertPayloadText,
		})
	}
	for _, size := range []struct{ width, height int }{{120, 40}, {80, 40}} {
		model := newModel(store, nil)
		model.SetSize(size.width, size.height)
		for _, detail := range []bool{false, true} {
			model.detail = detail
			output := model.View()
			if got := lipgloss.Width(output); got > size.width {
				t.Fatalf("%dx%d detail=%t width=%d", size.width, size.height, detail, got)
			}
			if got := lipgloss.Height(output); got > size.height {
				t.Fatalf("%dx%d detail=%t height=%d", size.width, size.height, detail, got)
			}
		}
	}
}

func TestRuleDetailPreservesCaseSensitiveData(t *testing.T) {
	store := state.NewStore()
	store.AddAlert(state.Alert{
		ID: "case-sensitive", PayloadKind: state.AlertPayloadRule,
		Rule: &state.Rule{
			Name: "case-sensitive-path",
			Operator: state.RuleOperator{
				Type: "simple", Operand: "process.path", Data: "/Opt/Case/Sensitive/App", Sensitive: true,
			},
		},
	})
	model := newModel(store, nil)
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if output := model.View(); !strings.Contains(output, "/Opt/Case/Sensitive/App") {
		t.Fatalf("case-sensitive rule data was redacted: %q", output)
	}
}

func newModel(store *state.Store, archive *fakeArchive) *Model {
	th := theme.New(theme.Options{})
	var model *Model
	if archive == nil {
		model = New(store, th).(*Model)
	} else {
		model = New(store, th, archive).(*Model)
	}
	model.SetSize(80, 40)
	return model
}

func textAlert(id, text string) state.Alert {
	return state.Alert{
		ID: id, NodeID: "node-1", Text: text,
		Priority: state.AlertPriorityHigh, Type: state.AlertTypeWarning,
		Action: state.AlertActionShowAlert, What: state.AlertWhatGeneric,
		PayloadKind: state.AlertPayloadText, CreatedAt: time.Now(),
	}
}
