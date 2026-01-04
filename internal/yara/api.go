// Package yara provides YARA rule scanning capabilities with optional
// support via cgo bindings to libyara.
package yara

import "errors"

var (
	ErrUnavailable = errors.New("yara not available; ensure cgo+libyara or remove `-tags no_yara`")
	ErrNoRules     = errors.New("yara rule directory not configured")
)

type Match struct {
	Rule string
}

type Result struct {
	Matches []Match
}
