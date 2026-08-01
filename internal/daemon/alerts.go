package daemon

import (
	"fmt"
	"time"

	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
)

func convertAlert(alert *pb.Alert, nodeID string) state.Alert {
	if alert == nil {
		return state.Alert{}
	}

	converted := state.Alert{
		ID:        fmt.Sprintf("%d", alert.GetId()),
		NodeID:    nodeID,
		Priority:  alertPriority(alert.GetPriority()),
		Type:      alertType(alert.GetType()),
		Action:    alertAction(alert.GetAction()),
		What:      alertWhat(alert.GetWhat()),
		CreatedAt: time.Now(),
	}
	switch payload := alert.GetData().(type) {
	case *pb.Alert_Text:
		if payload != nil {
			converted.PayloadKind = state.AlertPayloadText
			converted.Text = payload.Text
		}
	case *pb.Alert_Proc:
		if payload != nil && payload.Proc != nil {
			process := convertProcess(payload.Proc)
			converted.PayloadKind = state.AlertPayloadProcess
			converted.Process = &process
		}
	case *pb.Alert_Conn:
		if payload != nil && payload.Conn != nil {
			connection := convertConnection(payload.Conn)
			converted.PayloadKind = state.AlertPayloadConnection
			converted.Connection = &connection
		}
	case *pb.Alert_Rule:
		if payload != nil && payload.Rule != nil {
			rule := convertRule(payload.Rule, nodeID)
			converted.PayloadKind = state.AlertPayloadRule
			converted.Rule = &rule
		}
	case *pb.Alert_Fwrule:
		if payload != nil && payload.Fwrule != nil {
			rule := convertFirewallRule(payload.Fwrule)
			converted.PayloadKind = state.AlertPayloadFirewall
			converted.FirewallRule = &rule
		}
	}
	return converted
}

func convertProcess(process *pb.Process) state.Process {
	if process == nil {
		return state.Process{}
	}
	return state.Process{
		PID:         process.GetPid(),
		PPID:        process.GetPpid(),
		UID:         process.GetUid(),
		Comm:        process.GetComm(),
		Path:        process.GetPath(),
		Args:        append([]string(nil), process.GetArgs()...),
		Env:         cloneStringMap(process.GetEnv()),
		CWD:         process.GetCwd(),
		Checksums:   cloneStringMap(process.GetChecksums()),
		IOReads:     process.GetIoReads(),
		IOWrites:    process.GetIoWrites(),
		NetReads:    process.GetNetReads(),
		NetWrites:   process.GetNetWrites(),
		ProcessTree: convertProcessTree(process.GetProcessTree()),
	}
}

func alertPriority(value pb.Alert_Priority) state.AlertPriority {
	if name, ok := pb.Alert_Priority_name[int32(value)]; ok {
		return state.AlertPriority(name)
	}
	return state.AlertPriority(fmt.Sprintf("UNKNOWN(%d)", value))
}

func alertType(value pb.Alert_Type) state.AlertType {
	if name, ok := pb.Alert_Type_name[int32(value)]; ok {
		return state.AlertType(name)
	}
	return state.AlertType(fmt.Sprintf("UNKNOWN(%d)", value))
}

func alertAction(value pb.Alert_Action) state.AlertAction {
	if name, ok := pb.Alert_Action_name[int32(value)]; ok {
		return state.AlertAction(name)
	}
	return state.AlertAction(fmt.Sprintf("UNKNOWN(%d)", value))
}

func alertWhat(value pb.Alert_What) state.AlertWhat {
	if name, ok := pb.Alert_What_name[int32(value)]; ok {
		return state.AlertWhat(name)
	}
	return state.AlertWhat(fmt.Sprintf("UNKNOWN(%d)", value))
}
