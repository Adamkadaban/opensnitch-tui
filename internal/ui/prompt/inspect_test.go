package prompt

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
)

func TestBuildProcessInspect_IncludesRealGroup(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/proc not available on non-Linux platforms")
	}
	if _, err := os.Stat("/proc/self/status"); err != nil {
		t.Skip("/proc not accessible: " + err.Error())
	}

	pid := os.Getpid()
	info := buildProcessInspect(state.Connection{ProcessID: uint32(pid)}, nil)

	hasRealGroup := false
	for _, line := range info.Lines {
		if strings.HasPrefix(line, "Group: ") {
			hasRealGroup = true
			break
		}
	}

	if !hasRealGroup {
		t.Fatalf("expected real group line in inspect info; got lines: %v", info.Lines)
	}
}
