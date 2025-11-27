package prompt

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
)

type PathHighlighter func(string) string

func hasPrefix(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func matchedPrefix(path string, prefixes []string) string {
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return p
		}
	}
	return ""
}

func buildProcessInspect(conn state.Connection, hl PathHighlighter) processInspect {
	lines := []string{}
	maxWidth := 0
	track := func(s string) {
		lines = append(lines, s)
		if w := runeWidth(s); w > maxWidth {
			maxWidth = w
		}
	}
	pid := int(conn.ProcessID)
	if pid > 0 {
		track(fmt.Sprintf("PID: %d", pid))
	}
	if conn.ProcessPath != "" {
		path := conn.ProcessPath
		if hl != nil {
			path = hl(path)
		}
		track(fmt.Sprintf("Executable: %s", path))
	}
	if len(conn.ProcessArgs) > 0 {
		track(fmt.Sprintf("Args: %s", strings.Join(conn.ProcessArgs, " ")))
	}
	if conn.ProcessCWD != "" {
		cwd := conn.ProcessCWD
		if hl != nil {
			cwd = hl(cwd)
		}
		track(fmt.Sprintf("CWD: %s", cwd))
	}
	if conn.UserID != 0 {
		track(fmt.Sprintf("User: %s", resolveUser(uint32(conn.UserID))))
	}

	// Best-effort /proc inspection (only works if TUI host == process host)
	if pid > 0 {
		uids, gids := readProcIDs(pid)
		if gids[0] != "" {
			track(fmt.Sprintf("Group: %s", resolveGroup(gids[0])))
		}
		if uids[1] != "" {
			track(fmt.Sprintf("User (effective): %s", resolveUserString(uids[1])))
		}
		if gids[1] != "" {
			track(fmt.Sprintf("Group (effective): %s", resolveGroup(gids[1])))
		}

		if tree := readProcessTree(pid, hl); len(tree) > 0 {
			track("")
			track("Process Tree:")
			for _, line := range tree {
				track(line)
			}
		}
	}

	if len(lines) == 0 {
		track("No additional process info available")
	}
	return processInspect{Lines: lines, MaxWidth: maxWidth}
}

// (YARA status is rendered in the header; content remains plain process info.)

// renderInspectContent slices lines horizontally by offset and clips to width.
func renderInspectContent(info processInspect, offset, width int) string {
	if width <= 0 {
		return ""
	}
	rows := make([]string, len(info.Lines))
	for i, line := range info.Lines {
		raw := stripANSI(line)
		if offset >= len([]rune(raw)) {
			rows[i] = ""
			continue
		}
		rows[i] = ansiSlice(line, offset, width)
	}
	return strings.Join(rows, "\n")
}

// runeWidth returns the number of runes in s.
func runeWidth(s string) int { return len([]rune(stripANSI(s))) }

// stripANSI removes ANSI escape sequences from s.
func stripANSI(s string) string {
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

// ansiSlice returns the substring of s corresponding to visible runes [offset, offset+width), preserving ANSI codes.
func ansiSlice(s string, offset, width int) string {
	var b strings.Builder
	visible := 0
	started := false
	lastSGR := ""
	for i := 0; i < len(s); {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !((s[j] >= 'A' && s[j] <= 'Z') || (s[j] >= 'a' && s[j] <= 'z')) {
				j++
			}
			if j < len(s) {
				esc := s[i : j+1]
				if esc[len(esc)-1] == 'm' {
					lastSGR = esc
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
				if lastSGR != "" {
					b.WriteString(lastSGR)
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
	return b.String()
}

type procNode struct {
	PID      int
	Comm     string
	Cmdline  string
	Path     string
	Children []*procNode
}

func readProcessTree(pid int, hl PathHighlighter) []string {
	root := buildTree(pid, map[int]bool{}, 0, hl)
	if root == nil {
		return nil
	}
	var lines []string
	formatTree(root, "", true, &lines)
	return lines
}

func readProcStat(pid int) (comm string, ppid int) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return "?", 0
	}
	// /proc/<pid>/stat format: pid (comm) state ppid ...
	content := string(data)
	// comm may contain spaces; it's between first '(' and last ')'
	l := strings.IndexRune(content, '(')
	r := strings.LastIndex(content, ")")
	if l >= 0 && r > l {
		comm = content[l+1 : r]
	}
	// split after comm
	rest := strings.Fields(content[r+1:])
	if len(rest) >= 2 {
		if v, err := strconv.Atoi(rest[1]); err == nil { // rest[0]=state, rest[1]=ppid
			ppid = v
		}
	}
	if comm == "" {
		comm = "?"
	}
	return comm, ppid
}

func buildTree(pid int, visited map[int]bool, depth int, hl PathHighlighter) *procNode {
	if depth > 128 {
		return nil
	}
	if visited[pid] {
		return nil
	}
	visited[pid] = true
	comm, _ := readProcStat(pid)
	cmdline := readProcCmdline(pid)
	path := readProcExe(pid)
	if hl != nil {
		path = hl(path)
	}
	node := &procNode{PID: pid, Comm: comm, Cmdline: cmdline, Path: path}
	for _, child := range readProcChildren(pid) {
		if cnode := buildTree(child, visited, depth+1, hl); cnode != nil {
			node.Children = append(node.Children, cnode)
		}
	}
	return node
}

func formatTree(node *procNode, prefix string, last bool, out *[]string) {
	if node == nil {
		return
	}
	connector := "├──"
	childPrefix := "│   "
	if last {
		connector = "└──"
		childPrefix = "    "
	}
	line := fmt.Sprintf("%s%s%d %s", prefix, connector, node.PID, node.Comm)
	if node.Cmdline != "" {
		line += fmt.Sprintf(" %s", node.Cmdline)
	}
	if node.Path != "" {
		line += fmt.Sprintf(" (%s)", node.Path)
	}
	*out = append(*out, line)
	for i, c := range node.Children {
		formatTree(c, prefix+childPrefix, i == len(node.Children)-1, out)
	}
}

func readProcCmdline(pid int) string {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return ""
	}
	parts := strings.Split(string(data), "\x00")
	return strings.TrimSpace(strings.Join(parts, " "))
}

func readProcExe(pid int) string {
	path, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	if err != nil {
		return ""
	}
	return path
}

func readProcChildren(pid int) []int {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "task", strconv.Itoa(pid), "children"))
	if err != nil {
		return nil
	}
	fields := strings.Fields(string(data))
	res := make([]int, 0, len(fields))
	for _, f := range fields {
		if v, err := strconv.Atoi(f); err == nil {
			res = append(res, v)
		}
	}
	return res
}

func resolveUser(uid uint32) string {
	if u, err := user.LookupId(strconv.FormatUint(uint64(uid), 10)); err == nil {
		return fmt.Sprintf("%s (%d)", u.Username, uid)
	}
	return fmt.Sprintf("%d", uid)
}

func resolveGroup(gid string) string {
	fields := strings.Fields(gid)
	if len(fields) == 0 {
		return gid
	}
	first := fields[0]
	if g, err := lookupGroup(first); err == nil && g != "" {
		return fmt.Sprintf("%s (%s)", g, first)
	}
	return gid
}

func lookupGroup(gid string) (string, error) {
	data, err := os.ReadFile("/etc/group")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) >= 3 && parts[2] == gid {
			return parts[0], nil
		}
	}
	return "", fmt.Errorf("group %s not found", gid)
}

// readProcIDs parses /proc/<pid>/status for Uid and Gid lines.
// Returns arrays [real, effective, saved set, fs].
func readProcIDs(pid int) ([4]string, [4]string) {
	var uids, gids [4]string
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
	if err != nil {
		return uids, gids
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "Uid:") {
			fields := strings.Fields(strings.TrimPrefix(line, "Uid:"))
			for i := 0; i < len(fields) && i < 4; i++ {
				uids[i] = fields[i]
			}
		}
		if strings.HasPrefix(line, "Gid:") {
			fields := strings.Fields(strings.TrimPrefix(line, "Gid:"))
			for i := 0; i < len(fields) && i < 4; i++ {
				gids[i] = fields[i]
			}
		}
	}
	return uids, gids
}

func resolveUserString(id string) string {
	if id == "" {
		return ""
	}
	if v, err := strconv.ParseUint(id, 10, 32); err == nil {
		return resolveUser(uint32(v))
	}
	return id
}
