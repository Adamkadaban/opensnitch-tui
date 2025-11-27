package prompt

import (
	"testing"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
)

func TestIsLocalAddress(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"", true},
		{"unix:///tmp/opensnitch.sock", true},
		{"unix://@:1764271078117941612", true},
		{"localhost:50051", true},
		{"127.0.0.1:50051", true},
		{"[::1]:50051", true},
		{"::1", true},
		{"192.168.1.10:50051", false},
		{"example.com:443", false},
	}
	for _, tt := range cases {
		if got := isLocalAddress(tt.addr); got != tt.want {
			t.Errorf("isLocalAddress(%q) = %v, want %v", tt.addr, got, tt.want)
		}
	}
}

func TestIsLocalNode(t *testing.T) {
	nodes := []state.Node{
		{ID: "a", Address: "localhost:50051"},
		{ID: "b", Address: "192.168.1.10:50051"},
	}
	if !isLocalNode(nodes, "a") {
		t.Fatalf("expected node a to be local")
	}
	if isLocalNode(nodes, "b") {
		t.Fatalf("expected node b to be remote")
	}
	if !isLocalNode(nodes, "") {
		t.Fatalf("expected empty node ID to be treated as local")
	}
	if !isLocalNode(nodes, "unix://@:1764271078117941612") {
		t.Fatalf("expected peerKey-style unix node ID to be treated as local")
	}
	if isLocalNode(nodes, "missing") {
		t.Fatalf("expected missing node to be non-local")
	}
}
