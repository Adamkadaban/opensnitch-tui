//go:build cgo && !no_yara

package yara

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	gyara "github.com/hillu/go-yara/v4"
)

var (
	compiledMu   sync.Mutex
	compiledDirs = make(map[string]*gyara.Rules)
)

// IsAvailable reports whether YARA support is built in.
func IsAvailable() bool { return true }

// ScanFile scans a file at path using rules compiled from rulesDir.
func ScanFile(path, rulesDir string) (Result, error) {
	if rulesDir == "" {
		return Result{}, ErrNoRules
	}
	rules, err := getOrCompile(rulesDir)
	if err != nil {
		return Result{}, err
	}
	scanner, err := gyara.NewScanner(rules)
	if err != nil {
		return Result{}, err
	}
	var matches gyara.MatchRules
	if err := scanner.SetCallback(&matches).ScanFile(path); err != nil {
		return Result{}, err
	}
	res := Result{Matches: make([]Match, len(matches))}
	for i, m := range matches {
		res.Matches[i] = Match{Rule: m.Rule}
	}
	return res, nil
}

func getOrCompile(dir string) (*gyara.Rules, error) {
	compiledMu.Lock()
	defer compiledMu.Unlock()
	if r, ok := compiledDirs[dir]; ok {
		return r, nil
	}
	compiler, err := gyara.NewCompiler()
	if err != nil {
		return nil, err
	}
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yar" && ext != ".yara" {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		return compiler.AddFile(f, "")
	})
	if err != nil {
		return nil, err
	}
	rules, err := compiler.GetRules()
	if err != nil {
		return nil, err
	}
	if len(rules.GetRules()) == 0 {
		return nil, fmt.Errorf("no yara rules found in %s", dir)
	}
	compiledDirs[dir] = rules
	return rules, nil
}
