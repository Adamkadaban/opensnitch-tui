package daemon

import "testing"

func TestParseListenAddr(t *testing.T) {
	tests := []struct {
		input   string
		network string
		address string
		wantErr bool
	}{
		{"127.0.0.1:50051", "tcp", "127.0.0.1:50051", false},
		{"unix:///tmp/osui.sock", "unix", "/tmp/osui.sock", false},
		{"unix://", "", "", true},
		{"", "", "", true},
	}

	for _, tt := range tests {
		target, err := parseListenAddr(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("expected error for input %q", tt.input)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tt.input, err)
		}
		if target.network != tt.network || target.address != tt.address {
			t.Fatalf("unexpected target for %q: %+v", tt.input, target)
		}
	}
}
