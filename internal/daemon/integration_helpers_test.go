package daemon

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
)

func integrationGateReason(getenv func(string) string) string {
	if getenv("OPENSNITCH_INTEGRATION") != "1" {
		return "set OPENSNITCH_INTEGRATION=1 via the integration harness"
	}
	if getenv("OPENSNITCH_INTEGRATION_ORCHESTRATED") != "1" {
		return "run through scripts/test-opensnitch-integration.sh"
	}
	return ""
}

func executablePathsMatch(got, want string) bool {
	if got == "" || want == "" {
		return false
	}
	resolve := func(path string) string {
		path = filepath.Clean(path)
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			return resolved
		}
		return path
	}
	return resolve(got) == resolve(want)
}

func validateNodeMonitorUpdate(data []byte) error {
	var update map[string]any
	if err := json.Unmarshal(data, &update); err != nil {
		return err
	}
	if _, ok := update["Uptime"].(float64); !ok {
		return errInvalidNodeMonitorUpdate("missing numeric Uptime")
	}
	loads, ok := update["Loads"].([]any)
	if !ok || len(loads) != 3 {
		return errInvalidNodeMonitorUpdate("missing three-value Loads")
	}
	for _, load := range loads {
		if _, ok := load.(float64); !ok {
			return errInvalidNodeMonitorUpdate("Loads contains a non-numeric value")
		}
	}
	return nil
}

type errInvalidNodeMonitorUpdate string

func (e errInvalidNodeMonitorUpdate) Error() string {
	return string(e)
}

func readyNodeWithFirewall(snapshot state.Snapshot) (state.Node, state.SystemFirewall, bool) {
	for _, node := range snapshot.Nodes {
		if node.Status != state.NodeStatusReady {
			continue
		}
		firewall, ok := snapshot.SystemFirewalls[node.ID]
		if ok {
			return node, firewall, true
		}
	}
	return state.Node{}, state.SystemFirewall{}, false
}

func TestIntegrationGateReason(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "disabled", env: map[string]string{}, want: "OPENSNITCH_INTEGRATION=1"},
		{
			name: "not orchestrated",
			env:  map[string]string{"OPENSNITCH_INTEGRATION": "1"},
			want: "scripts/test-opensnitch-integration.sh",
		},
		{
			name: "enabled",
			env: map[string]string{
				"OPENSNITCH_INTEGRATION":              "1",
				"OPENSNITCH_INTEGRATION_ORCHESTRATED": "1",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := integrationGateReason(func(key string) string {
				return test.env[key]
			})
			if test.want == "" && got != "" {
				t.Fatalf("expected enabled gate, got %q", got)
			}
			if test.want != "" && !strings.Contains(got, test.want) {
				t.Fatalf("expected %q in gate reason, got %q", test.want, got)
			}
		})
	}
}

func TestValidateNodeMonitorUpdate(t *testing.T) {
	if err := validateNodeMonitorUpdate([]byte(`{"Uptime":42,"Loads":[1,2,3]}`)); err != nil {
		t.Fatalf("expected valid update: %v", err)
	}
	for _, data := range [][]byte{
		[]byte(`not-json`),
		[]byte(`{"Loads":[1,2,3]}`),
		[]byte(`{"Uptime":42,"Loads":[1,2]}`),
		[]byte(`{"Uptime":42,"Loads":[1,2,"busy"]}`),
	} {
		if err := validateNodeMonitorUpdate(data); err == nil {
			t.Fatalf("expected invalid update for %s", data)
		}
	}
}

func TestExecutablePathsMatch(t *testing.T) {
	if !executablePathsMatch("/usr/bin/../bin/curl", "/usr/bin/curl") {
		t.Fatal("expected cleaned paths to match")
	}
	if executablePathsMatch("/usr/bin/curl", "/usr/bin/wget") {
		t.Fatal("did not expect different paths to match")
	}
	if executablePathsMatch("", "/usr/bin/curl") {
		t.Fatal("did not expect an empty path to match")
	}
}

func TestReadyNodeWithFirewall(t *testing.T) {
	snapshot := state.Snapshot{
		Nodes: []state.Node{
			{ID: "connecting", Status: state.NodeStatusConnecting},
			{ID: "ready", Name: "test-node", Status: state.NodeStatusReady},
		},
		SystemFirewalls: map[string]state.SystemFirewall{
			"ready": {NodeID: "ready", Enabled: true, Running: true},
		},
	}
	node, firewall, ok := readyNodeWithFirewall(snapshot)
	if !ok {
		t.Fatal("expected ready node with firewall")
	}
	if node.ID != "ready" || firewall.NodeID != "ready" {
		t.Fatalf("unexpected node/firewall: %+v %+v", node, firewall)
	}
}
