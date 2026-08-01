package daemon

import (
	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
)

func convertConnection(conn *pb.Connection) state.Connection {
	if conn == nil {
		return state.Connection{}
	}
	converted := state.Connection{
		Protocol:    conn.GetProtocol(),
		SrcIP:       conn.GetSrcIp(),
		SrcPort:     conn.GetSrcPort(),
		DstIP:       conn.GetDstIp(),
		DstHost:     conn.GetDstHost(),
		DstPort:     conn.GetDstPort(),
		UserID:      conn.GetUserId(),
		ProcessID:   conn.GetProcessId(),
		ProcessPath: conn.GetProcessPath(),
		ProcessCWD:  conn.GetProcessCwd(),
	}
	if args := conn.GetProcessArgs(); len(args) > 0 {
		converted.ProcessArgs = append([]string{}, args...)
	}
	if env := conn.GetProcessEnv(); len(env) > 0 {
		converted.ProcessEnv = cloneStringMap(env)
	}
	if checksums := conn.GetProcessChecksums(); len(checksums) > 0 {
		converted.ProcessChecksums = cloneStringMap(checksums)
	}
	if tree := conn.GetProcessTree(); len(tree) > 0 {
		converted.ProcessTree = convertProcessTree(tree)
	}
	return converted
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func convertProcessTree(entries []*pb.StringInt) []state.ProcessTreeEntry {
	if len(entries) == 0 {
		return nil
	}
	converted := make([]state.ProcessTreeEntry, len(entries))
	for i, entry := range entries {
		if entry == nil {
			continue
		}
		converted[i] = state.ProcessTreeEntry{Path: entry.GetKey(), PID: entry.GetValue()}
	}
	return converted
}
