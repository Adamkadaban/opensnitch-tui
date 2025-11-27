//go:build !cgo || no_yara

package yara

import "testing"

func TestStubIsAvailable(t *testing.T) {
	if IsAvailable() {
		t.Fatalf("expected IsAvailable false for stub build")
	}
	if _, err := ScanFile("/bin/echo", "/tmp"); err == nil {
		t.Fatalf("expected error from stub ScanFile")
	}
}
