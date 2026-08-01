package alertsafety

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const Redacted = "[redacted]"

var (
	authorizationValue = regexp.MustCompile(`(?i)(authorization\s*[:=]\s*(?:bearer|basic)?\s*)[^\s,;]+`)
	authSchemeValue    = regexp.MustCompile(`(?i)\b(bearer|basic)(\s+)[a-z0-9._~+/=-]+`)
	inlineSecret       = regexp.MustCompile(`(?i)(password|passwd|token|secret|api[-_]?key|cookie|credential)(\s*[:=]\s*)([^\s,;]+)`)
	spacedSecret       = regexp.MustCompile(`(?i)(password|passwd|token|secret|api[-_]?key|cookie|credential)(\s+)([^\s,;]+)`)
	urlCredentials     = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/\s:@]+:[^/\s@]+@`)
)

func LimitText(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	value = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r >= 0x20 && r != 0x7f {
			return r
		}
		return ' '
	}, value)
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value + "…"
}

func SensitiveName(value string) bool {
	normalized := strings.ToLower(strings.NewReplacer("-", "", "_", "", ".", "").Replace(value))
	for _, marker := range []string{"password", "passwd", "token", "secret", "authorization", "apikey", "cookie", "credential"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func RedactInline(value string) string {
	value = urlCredentials.ReplaceAllString(value, `${1}`+Redacted+"@")
	value = authorizationValue.ReplaceAllString(value, `${1}`+Redacted)
	value = authSchemeValue.ReplaceAllString(value, `${1}${2}`+Redacted)
	value = inlineSecret.ReplaceAllString(value, `${1}${2}`+Redacted)
	return spacedSecret.ReplaceAllString(value, `${1}${2}`+Redacted)
}

func RedactArgs(args []string, limit int, maxBytes int) []string {
	if limit <= 0 || len(args) == 0 {
		return nil
	}
	count := min(len(args), limit)
	redacted := make([]string, count)
	redactNext := 0
	for i := 0; i < count; i++ {
		arg := args[i]
		if redactNext > 0 {
			redacted[i] = Redacted
			redactNext--
			continue
		}
		if key, _, ok := strings.Cut(arg, "="); ok && SensitiveName(key) {
			redacted[i] = key + "=" + Redacted
			continue
		}
		redacted[i] = LimitText(RedactInline(arg), maxBytes)
		if SensitiveName(arg) {
			redactNext = 1
			if strings.Contains(strings.ToLower(arg), "authorization") {
				redactNext = 2
			}
		}
	}
	return redacted
}
