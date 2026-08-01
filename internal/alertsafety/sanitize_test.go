package alertsafety

import (
	"strings"
	"testing"
)

func TestRedactionRemovesCommonAuthenticationData(t *testing.T) {
	inputs := []string{
		"Authorization: Bearer top-secret",
		"token=top-secret",
		"password top-secret",
		"https://user:top-secret@example.com/path",
	}
	for _, input := range inputs {
		output := RedactInline(input)
		if strings.Contains(output, "top-secret") || !strings.Contains(output, Redacted) {
			t.Fatalf("RedactInline(%q) = %q", input, output)
		}
	}

	args := RedactArgs([]string{"Authorization:", "Bearer", "top-secret", "safe"}, 8, 128)
	if strings.Contains(strings.Join(args, " "), "top-secret") || args[3] != "safe" {
		t.Fatalf("unexpected argument redaction: %#v", args)
	}
}
