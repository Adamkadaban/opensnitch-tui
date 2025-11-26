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
	if checksums := conn.GetProcessChecksums(); len(checksums) > 0 {
		converted.ProcessChecksums = make(map[string]string, len(checksums))
		for key, value := range checksums {
			converted.ProcessChecksums[key] = value
		}
	}
	return converted
}
