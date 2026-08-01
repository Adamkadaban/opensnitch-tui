package daemon

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
)

func completePromptConnection() state.Connection {
	return state.Connection{
		ProcessPath: "/usr/bin/curl",
		ProcessArgs: []string{"/usr/bin/curl", "https://example.com"},
		ProcessID:   4321,
		UserID:      0,
		DstHost:     "example.com",
		DstIP:       "203.0.113.10",
		DstPort:     443,
		ProcessChecksums: map[string]string{
			operandChecksumMD5: "d41d8cd98f00b204e9800998ecf8427e",
		},
	}
}

func TestOperatorForTarget(t *testing.T) {
	conn := completePromptConnection()
	tests := []struct {
		target  controller.PromptTarget
		operand string
		data    string
	}{
		{controller.PromptTargetProcessPath, operandProcessPath, "/usr/bin/curl"},
		{controller.PromptTargetProcessCmd, operandProcessCmd, "/usr/bin/curl https://example.com"},
		{controller.PromptTargetProcessID, operandProcessID, "4321"},
		{controller.PromptTargetUserID, operandUserID, "0"},
		{controller.PromptTargetDestinationHost, operandDestHost, "example.com"},
		{controller.PromptTargetDestinationIP, operandDestIP, "203.0.113.10"},
		{controller.PromptTargetDestinationPort, operandDestPort, "443"},
		{controller.PromptTargetChecksumMD5, operandChecksumMD5, "d41d8cd98f00b204e9800998ecf8427e"},
	}
	for _, test := range tests {
		t.Run(string(test.target), func(t *testing.T) {
			operator, err := operatorForTarget(conn, test.target)
			if err != nil {
				t.Fatalf("operatorForTarget error: %v", err)
			}
			if operator.GetType() != ruleTypeSimple || operator.GetOperand() != test.operand || operator.GetData() != test.data {
				t.Fatalf("unexpected operator: %+v", operator)
			}
		})
	}
}

func TestOperatorForDecisionBuildsDeterministicList(t *testing.T) {
	conn := completePromptConnection()
	decision := controller.PromptDecision{
		Target: controller.PromptTargetProcessPath,
		Conditions: []controller.PromptCondition{
			{Target: controller.PromptTargetChecksumMD5},
			{Target: controller.PromptTargetDestinationPort},
			{Target: controller.PromptTargetDestinationIP},
			{Target: controller.PromptTargetUserID},
		},
	}
	operator, err := operatorForDecision(conn, decision)
	if err != nil {
		t.Fatalf("operatorForDecision error: %v", err)
	}
	if operator.GetType() != ruleTypeList || operator.GetOperand() != ruleTypeList {
		t.Fatalf("expected list operator, got %+v", operator)
	}
	wantOperands := []string{operandDestIP, operandDestPort, operandUserID, operandChecksumMD5, operandProcessPath}
	gotOperands := make([]string, len(operator.GetList()))
	for i, child := range operator.GetList() {
		gotOperands[i] = child.GetOperand()
	}
	if !reflect.DeepEqual(gotOperands, wantOperands) {
		t.Fatalf("unexpected child order: got %v want %v", gotOperands, wantOperands)
	}
	var encoded []operatorData
	if err := json.Unmarshal([]byte(operator.GetData()), &encoded); err != nil {
		t.Fatalf("invalid list data JSON: %v", err)
	}
	if len(encoded) != len(operator.GetList()) {
		t.Fatalf("data/list length mismatch: %d != %d", len(encoded), len(operator.GetList()))
	}
	for i, child := range operator.GetList() {
		if encoded[i].Type != child.GetType() || encoded[i].Operand != child.GetOperand() || encoded[i].Data != child.GetData() {
			t.Fatalf("data child %d mismatch: %+v != %+v", i, encoded[i], child)
		}
	}
	const wantData = `[{"type":"simple","operand":"dest.ip","data":"203.0.113.10"},{"type":"simple","operand":"dest.port","data":"443"},{"type":"simple","operand":"user.id","data":"0"},{"type":"simple","operand":"process.hash.md5","data":"d41d8cd98f00b204e9800998ecf8427e"},{"type":"simple","operand":"process.path","data":"/usr/bin/curl"}]`
	if operator.GetData() != wantData {
		t.Fatalf("unexpected deterministic data:\n got %s\nwant %s", operator.GetData(), wantData)
	}
}

func TestOperatorForDecisionDeduplicatesConditions(t *testing.T) {
	operator, err := operatorForDecision(completePromptConnection(), controller.PromptDecision{
		Target: controller.PromptTargetDestinationIP,
		Conditions: []controller.PromptCondition{
			{Target: controller.PromptTargetDestinationIP},
			{Target: controller.PromptTargetDestinationIP},
		},
	})
	if err != nil {
		t.Fatalf("operatorForDecision error: %v", err)
	}
	if operator.GetType() != ruleTypeSimple || len(operator.GetList()) != 0 || operator.GetOperand() != operandDestIP {
		t.Fatalf("expected one simple destination IP operator, got %+v", operator)
	}
}

func TestCommandDecisionIncludesPathWhenCommandIsNotAbsolute(t *testing.T) {
	conn := completePromptConnection()
	conn.ProcessArgs = []string{"curl", "https://example.com"}
	operator, err := operatorForDecision(conn, controller.PromptDecision{Target: controller.PromptTargetProcessCmd})
	if err != nil {
		t.Fatalf("operatorForDecision error: %v", err)
	}
	if operator.GetType() != ruleTypeList || len(operator.GetList()) != 2 {
		t.Fatalf("expected command/path list, got %+v", operator)
	}
	if operator.GetList()[0].GetOperand() != operandProcessPath || operator.GetList()[1].GetOperand() != operandProcessCmd {
		t.Fatalf("unexpected command/path order: %+v", operator.GetList())
	}

	conn.ProcessPath = ""
	if _, err := operatorForDecision(conn, controller.PromptDecision{Target: controller.PromptTargetProcessCmd}); err == nil {
		t.Fatalf("expected untrusted command without process path to fail")
	}
}

func TestOperatorForDecisionRejectsEmptyCondition(t *testing.T) {
	_, err := operatorForDecision(completePromptConnection(), controller.PromptDecision{
		Target:     controller.PromptTargetProcessPath,
		Conditions: []controller.PromptCondition{{}},
	})
	if err == nil || !strings.Contains(err.Error(), "condition target required") {
		t.Fatalf("expected empty condition error, got %v", err)
	}
}

func TestOperatorForTargetRejectsUnavailableValues(t *testing.T) {
	tests := []struct {
		name   string
		target controller.PromptTarget
		conn   state.Connection
	}{
		{"path", controller.PromptTargetProcessPath, state.Connection{ProcessPath: "  "}},
		{"command", controller.PromptTargetProcessCmd, state.Connection{ProcessPath: "/usr/bin/curl"}},
		{"pid", controller.PromptTargetProcessID, state.Connection{}},
		{"host", controller.PromptTargetDestinationHost, state.Connection{DstHost: " "}},
		{"ip", controller.PromptTargetDestinationIP, state.Connection{DstIP: " "}},
		{"port", controller.PromptTargetDestinationPort, state.Connection{}},
		{"checksum missing", controller.PromptTargetChecksumMD5, state.Connection{}},
		{"checksum empty", controller.PromptTargetChecksumMD5, state.Connection{ProcessChecksums: map[string]string{operandChecksumMD5: " "}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := operatorForTarget(test.conn, test.target); err == nil {
				t.Fatalf("expected unavailable %s to fail", test.target)
			}
		})
	}
	if _, err := operatorForTarget(state.Connection{UserID: 0}, controller.PromptTargetUserID); err != nil {
		t.Fatalf("root user ID must remain a valid target: %v", err)
	}
}

func TestOperatorForDecisionDoesNotWeakenUnavailableCondition(t *testing.T) {
	conn := completePromptConnection()
	conn.ProcessChecksums = nil
	_, err := operatorForDecision(conn, controller.PromptDecision{
		Target:     controller.PromptTargetProcessPath,
		Conditions: []controller.PromptCondition{{Target: controller.PromptTargetChecksumMD5}},
	})
	if err == nil || !strings.Contains(err.Error(), "checksum unavailable") {
		t.Fatalf("expected unavailable checksum error, got %v", err)
	}
}

func TestBuildRuleActionDurationMatrix(t *testing.T) {
	actions := []controller.PromptAction{
		controller.PromptActionAllow,
		controller.PromptActionDeny,
		controller.PromptActionReject,
	}
	durations := []controller.PromptDuration{
		controller.PromptDurationOnce,
		controller.PromptDuration30Seconds,
		controller.PromptDuration5Minutes,
		controller.PromptDuration15Minutes,
		controller.PromptDuration30Minutes,
		controller.PromptDuration1Hour,
		controller.PromptDuration12Hours,
		controller.PromptDurationUntilRestart,
		controller.PromptDurationAlways,
	}
	server := New(state.NewStore(), Options{})
	prompt := state.Prompt{ID: "prompt", NodeID: "node", Connection: completePromptConnection()}
	for _, action := range actions {
		for _, duration := range durations {
			t.Run(string(action)+"/"+string(duration), func(t *testing.T) {
				rule, err := server.buildRuleFromDecision(prompt, controller.PromptDecision{
					PromptID: prompt.ID,
					Action:   action,
					Duration: duration,
					Target:   controller.PromptTargetProcessPath,
				})
				if err != nil {
					t.Fatalf("buildRuleFromDecision error: %v", err)
				}
				if rule.GetAction() != string(action) || rule.GetDuration() != string(duration) {
					t.Fatalf("unexpected action/duration: %s/%s", rule.GetAction(), rule.GetDuration())
				}
			})
		}
	}
}

func TestCompositePromptRuleRoundTripAndBoundedMetadata(t *testing.T) {
	store := state.NewStore()
	server := New(store, Options{})
	conn := completePromptConnection()
	conn.ProcessPath = "/" + strings.Repeat("unsafe path ", 40)
	prompt := state.Prompt{ID: "prompt", NodeID: "node", Connection: conn}
	rule, err := server.buildRuleFromDecision(prompt, controller.PromptDecision{
		PromptID: prompt.ID,
		Action:   controller.PromptActionAllow,
		Duration: controller.PromptDuration12Hours,
		Target:   controller.PromptTargetProcessPath,
		Conditions: []controller.PromptCondition{
			{Target: controller.PromptTargetDestinationIP},
			{Target: controller.PromptTargetDestinationPort},
		},
	})
	if err != nil {
		t.Fatalf("buildRuleFromDecision error: %v", err)
	}
	if len(rule.GetName()) > maxRuleNameLength || len(rule.GetDescription()) > maxRuleDescription {
		t.Fatalf("unbounded rule metadata: name=%d description=%d", len(rule.GetName()), len(rule.GetDescription()))
	}
	if strings.Contains(rule.GetDescription(), conn.ProcessPath) {
		t.Fatalf("description exposed unbounded process content: %q", rule.GetDescription())
	}
	converted := convertRule(rule, prompt.NodeID)
	roundTrip := serializeRule(converted)
	if !reflect.DeepEqual(rule.GetOperator(), roundTrip.GetOperator()) {
		t.Fatalf("operator changed after round trip:\n got %+v\nwant %+v", roundTrip.GetOperator(), rule.GetOperator())
	}
	if roundTrip.GetDescription() != rule.GetDescription() {
		t.Fatalf("description changed after round trip")
	}
}

func TestCompositeDataMatchesProtocolChildren(t *testing.T) {
	operator, err := operatorForDecision(completePromptConnection(), controller.PromptDecision{
		Target:     controller.PromptTargetDestinationHost,
		Conditions: []controller.PromptCondition{{Target: controller.PromptTargetDestinationPort}},
	})
	if err != nil {
		t.Fatalf("operatorForDecision error: %v", err)
	}
	protoRule := &pb.Rule{Operator: operator}
	roundTrip := serializeRule(convertRule(protoRule, "node"))
	if !reflect.DeepEqual(protoRule.GetOperator(), roundTrip.GetOperator()) {
		t.Fatalf("list operator did not survive conversion")
	}
}
