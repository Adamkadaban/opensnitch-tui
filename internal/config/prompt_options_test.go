package config

import "testing"

func TestNormalizeAdvancedPromptOptions(t *testing.T) {
	for _, duration := range []string{"once", "30s", "5m", "15m", "30m", "1h", "12h", "until restart", "always"} {
		if got := NormalizePromptDuration(duration); got != duration {
			t.Fatalf("NormalizePromptDuration(%q) = %q", duration, got)
		}
	}
	if got := NormalizePromptDuration("invalid"); got != DefaultPromptDuration {
		t.Fatalf("invalid duration normalized to %q", got)
	}
}
