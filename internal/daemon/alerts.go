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

	return state.Alert{
		ID:        fmt.Sprintf("%d", alert.GetId()),
		NodeID:    nodeID,
		Text:      alert.GetText(),
		Priority:  alert.GetPriority().String(),
		Type:      alert.GetType().String(),
		Action:    alert.GetAction().String(),
		CreatedAt: time.Now(),
	}
}
