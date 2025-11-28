package util

import (
	"strings"
	"unicode/utf8"
)

// StripANSI removes ANSI escape sequences from s.
func StripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !((s[j] >= 'A' && s[j] <= 'Z') || (s[j] >= 'a' && s[j] <= 'z')) {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		b.WriteRune(r)
		i += size
	}
	return b.String()
}

// AnsiSlice returns the substring of s corresponding to visible runes [offset, offset+width), preserving ANSI codes.
func AnsiSlice(s string, offset, width int) string {
	var b strings.Builder
	visible := 0
	started := false
	activeSGR := ""
	for i := 0; i < len(s); {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !((s[j] >= 'A' && s[j] <= 'Z') || (s[j] >= 'a' && s[j] <= 'z')) {
				j++
			}
			if j < len(s) {
				esc := s[i : j+1]
				if esc[len(esc)-1] == 'm' {
					if isResetSGR(esc) {
						activeSGR = ""
					} else {
						activeSGR = esc
					}
				}
				if started {
					b.WriteString(esc)
				}
				i = j + 1
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if size == 0 {
			break
		}
		if visible >= offset && visible < offset+width {
			if !started {
				started = true
				if activeSGR != "" {
					b.WriteString(activeSGR)
				}
			}
			b.WriteRune(r)
		}
		visible++
		i += size
		if visible >= offset+width {
			break
		}
	}
	if started && activeSGR != "" {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}

// RuneWidth returns the number of runes in s excluding ANSI sequences.
func RuneWidth(s string) int { return len([]rune(StripANSI(s))) }

func isResetSGR(esc string) bool {
	// esc is like "\x1b[...m"
	if len(esc) < 3 || esc[len(esc)-1] != 'm' {
		return false
	}
	contents := esc[2 : len(esc)-1] // between '[' and 'm'
	if contents == "" {
		return true
	}
	for _, part := range strings.Split(contents, ";") {
		if part == "0" || part == "" {
			return true
		}
	}
	return false
}
