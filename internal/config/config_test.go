package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateAcceptsValidConfig(t *testing.T) {
	dir := t.TempDir()
	cert := filepath.Join(dir, "cert.pem")
	key := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(cert, []byte("cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		Nodes: []Node{
			{Address: "127.0.0.1:50051"},
			{Address: "tcp://example.com:8443", CertPath: cert, KeyPath: key},
		},
	}

	if err := Validate(cfg); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
}

func TestValidateRejectsBadAddress(t *testing.T) {
	cases := []struct {
		name string
		node Node
	}{
		{"empty", Node{}},
		{"missing port", Node{Address: "localhost"}},
		{"bad port", Node{Address: "localhost:abc"}},
		{"zero port", Node{Address: "localhost:0"}},
		{"negative port", Node{Address: "localhost:-1"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{Nodes: []Node{tc.node}}
			if err := Validate(cfg); err == nil {
				t.Fatalf("expected error for %v", tc.node)
			}
		})
	}
}

func TestValidateRejectsMissingTLSFiles(t *testing.T) {
	cfg := Config{Nodes: []Node{{Address: "127.0.0.1:50051", CertPath: "/nonexistent/cert", KeyPath: "/nonexistent/key"}}}

	if err := Validate(cfg); err == nil {
		t.Fatalf("expected error for missing TLS files")
	}
}
