package daemon

import (
	"testing"
	"time"

	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
)

func TestConvertAlert(t *testing.T) {
	now := time.Now()
	alert := convertAlert(&pb.Alert{Id: 42, Data: &pb.Alert_Text{Text: "disk full"}}, "node-1")

	if alert.ID != "42" {
		t.Fatalf("expected id 42, got %s", alert.ID)
	}
	if alert.NodeID != "node-1" {
		t.Fatalf("expected node id node-1, got %s", alert.NodeID)
	}
	if alert.Text != "disk full" {
		t.Fatalf("expected text disk full, got %s", alert.Text)
	}
	if alert.CreatedAt.Before(now) {
		t.Fatalf("expected CreatedAt to be set")
	}
}

func TestConvertAlertNil(t *testing.T) {
	alert := convertAlert(nil, "node-1")
	if alert != (state.Alert{}) {
		t.Fatalf("expected zero alert, got %#v", alert)
	}
}
