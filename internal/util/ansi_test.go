package util

import "testing"

func TestAnsiSliceColoredPrefix(t *testing.T) {
	red := "\x1b[31m"
	reset := "\x1b[0m"
	s := red + "ABC" + reset + "DEF"
	// Slice inside colored region
	out := AnsiSlice(s, 1, 2) // should return colored "BC" and reset
	expected := red + "BC" + reset
	if out != expected {
		t.Fatalf("expected %q, got %q", expected, out)
	}

	// Slice starting after colored region, should not carry color
	out2 := AnsiSlice(s, 3, 2) // "DE"
	expected2 := "DE"
	if out2 != expected2 {
		t.Fatalf("expected %q, got %q", expected2, out2)
	}
}

func TestAnsiSliceWithResetInsideSlice(t *testing.T) {
	red := "\x1b[31m"
	reset := "\x1b[0m"
	s := red + "AB" + reset + "CDEF"
	// Slice covering reset boundary
	out := AnsiSlice(s, 1, 3) // "B C"
	expected := red + "B" + reset + "CD"
	if out != expected {
		t.Fatalf("expected %q, got %q", expected, out)
	}
}
