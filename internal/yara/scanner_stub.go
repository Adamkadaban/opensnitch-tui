//go:build !cgo || no_yara

package yara

// IsAvailable reports whether YARA support is built in.
func IsAvailable() bool { return false }

// ScanFile is a stub when built without the `yara` tag.
func ScanFile(_, _ string) (Result, error) {
	return Result{}, ErrUnavailable
}
