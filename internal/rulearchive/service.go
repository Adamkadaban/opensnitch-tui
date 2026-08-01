package rulearchive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
)

const (
	dirMode  = 0o700
	fileMode = 0o600
)

var ErrNoArchiveFiles = errors.New("no rule archive files found")

type Limits struct {
	MaxFiles         int
	MaxFileBytes     int64
	MaxTotalBytes    int64
	MaxOperatorDepth int
	MaxOperators     int
	MaxChildren      int
	MaxNameBytes     int
	MaxTextBytes     int
	MaxDataBytes     int
}

func DefaultLimits() Limits {
	return Limits{
		MaxFiles:         1000,
		MaxFileBytes:     1 << 20,
		MaxTotalBytes:    16 << 20,
		MaxOperatorDepth: 32,
		MaxOperators:     4096,
		MaxChildren:      1024,
		MaxNameBytes:     255,
		MaxTextBytes:     16 << 10,
		MaxDataBytes:     256 << 10,
	}
}

type Service struct {
	root   string
	limits Limits
}

func New(root string, limits Limits) (*Service, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("rule archive root is required")
	}
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("rule archive root must be absolute: %s", root)
	}
	if limits.MaxFiles <= 0 || limits.MaxFileBytes <= 0 || limits.MaxTotalBytes <= 0 ||
		limits.MaxOperatorDepth <= 0 || limits.MaxOperators <= 0 || limits.MaxChildren <= 0 ||
		limits.MaxNameBytes <= 0 || limits.MaxTextBytes <= 0 || limits.MaxDataBytes <= 0 {
		return nil, errors.New("rule archive limits must be positive")
	}
	return &Service{root: filepath.Clean(root), limits: limits}, nil
}

func NewDefault() (*Service, error) {
	root, err := DefaultRoot()
	if err != nil {
		return nil, err
	}
	return New(root, DefaultLimits())
}

func DefaultRoot() (string, error) {
	if dataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); dataHome != "" {
		if !filepath.IsAbs(dataHome) {
			return "", fmt.Errorf("XDG_DATA_HOME must be absolute: %s", dataHome)
		}
		return filepath.Join(dataHome, "opensnitch-tui", "rules"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "opensnitch-tui", "rules"), nil
}

func (s *Service) Directory(node state.Node) string {
	key := node.ID
	if key == "" {
		key = node.Name
	}
	label := node.Name
	if label == "" {
		label = node.ID
	}
	return filepath.Join(s.root, sanitizedComponent(label, "node")+"-"+shortHash(key))
}

func (s *Service) Export(ctx context.Context, node state.Node, rules []state.Rule) (string, error) {
	if err := ctx.Err(); err != nil {
		return s.Directory(node), err
	}
	if len(rules) == 0 {
		return s.Directory(node), errors.New("no rules to export")
	}
	if len(rules) > s.limits.MaxFiles {
		return s.Directory(node), fmt.Errorf("cannot export %d rules: limit is %d", len(rules), s.limits.MaxFiles)
	}
	if err := validateBatch(rules, s.limits); err != nil {
		return s.Directory(node), fmt.Errorf("cannot export rules: %w", err)
	}

	dir := s.Directory(node)
	if err := ensurePrivateDir(s.root); err != nil {
		return dir, err
	}
	if err := ensurePrivateDir(dir); err != nil {
		return dir, err
	}

	expected := make(map[string]struct{}, len(rules))
	var total int64
	for _, rule := range rules {
		if err := ctx.Err(); err != nil {
			return dir, err
		}
		name := archiveFilename(rule.Name)
		expected[name] = struct{}{}
		data, err := marshalRule(rule)
		if err != nil {
			return dir, fmt.Errorf("encode rule %q: %w", rule.Name, err)
		}
		if int64(len(data)) > s.limits.MaxFileBytes {
			return dir, fmt.Errorf("rule %q archive is %d bytes: per-file limit is %d", rule.Name, len(data), s.limits.MaxFileBytes)
		}
		total += int64(len(data))
		if total > s.limits.MaxTotalBytes {
			return dir, fmt.Errorf("rule archives exceed total size limit of %d bytes", s.limits.MaxTotalBytes)
		}
		if err := atomicWrite(filepath.Join(dir, name), data); err != nil {
			return dir, fmt.Errorf("write rule %q: %w", rule.Name, err)
		}
	}
	if err := removeStaleArchives(dir, expected); err != nil {
		return dir, err
	}
	return dir, nil
}

func (s *Service) Import(ctx context.Context, node state.Node) ([]state.Rule, string, error) {
	dir := s.Directory(node)
	if err := ctx.Err(); err != nil {
		return nil, dir, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, dir, fmt.Errorf("%w in %s", ErrNoArchiveFiles, dir)
		}
		return nil, dir, fmt.Errorf("inspect archive directory %s: %w", dir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, dir, fmt.Errorf("archive path is not a regular directory: %s", dir)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, dir, fmt.Errorf("read archive directory %s: %w", dir, err)
	}
	jsonEntries := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, dir, fmt.Errorf("refusing symlink archive file %s", entry.Name())
		}
		info, err := entry.Info()
		if err != nil {
			return nil, dir, fmt.Errorf("inspect archive file %s: %w", entry.Name(), err)
		}
		if !info.Mode().IsRegular() {
			return nil, dir, fmt.Errorf("refusing non-regular archive file %s", entry.Name())
		}
		jsonEntries = append(jsonEntries, entry)
	}
	if len(jsonEntries) == 0 {
		return nil, dir, fmt.Errorf("%w in %s", ErrNoArchiveFiles, dir)
	}
	if len(jsonEntries) > s.limits.MaxFiles {
		return nil, dir, fmt.Errorf("archive contains %d JSON files: limit is %d", len(jsonEntries), s.limits.MaxFiles)
	}
	sort.Slice(jsonEntries, func(i, j int) bool { return jsonEntries[i].Name() < jsonEntries[j].Name() })

	rules := make([]state.Rule, 0, len(jsonEntries))
	names := make(map[string]string, len(jsonEntries))
	var total int64
	for _, entry := range jsonEntries {
		if err := ctx.Err(); err != nil {
			return nil, dir, err
		}
		path := filepath.Join(dir, entry.Name())
		data, err := readBoundedRegular(path, s.limits.MaxFileBytes)
		if err != nil {
			return nil, dir, err
		}
		total += int64(len(data))
		if total > s.limits.MaxTotalBytes {
			return nil, dir, fmt.Errorf("archive JSON data exceeds total size limit of %d bytes", s.limits.MaxTotalBytes)
		}
		rule, err := unmarshalRule(data)
		if err != nil {
			return nil, dir, fmt.Errorf("parse %s: %w", entry.Name(), err)
		}
		rule.NodeID = node.ID
		if err := validateRule(rule, s.limits); err != nil {
			return nil, dir, fmt.Errorf("invalid rule in %s: %w", entry.Name(), err)
		}
		if previous, ok := names[rule.Name]; ok {
			return nil, dir, fmt.Errorf("duplicate rule name %q in %s and %s", rule.Name, previous, entry.Name())
		}
		names[rule.Name] = entry.Name()
		rules = append(rules, rule)
	}
	return rules, dir, nil
}

type archiveRule struct {
	Created     *archiveTime    `json:"created,omitempty"`
	Updated     *archiveTime    `json:"updated,omitempty"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Action      string          `json:"action"`
	Duration    string          `json:"duration"`
	Operator    archiveOperator `json:"operator"`
	Enabled     bool            `json:"enabled"`
	Precedence  bool            `json:"precedence"`
	NoLog       bool            `json:"nolog"`
}

type archiveOperator struct {
	Type      string            `json:"type"`
	Operand   string            `json:"operand,omitempty"`
	Data      string            `json:"data,omitempty"`
	Sensitive bool              `json:"sensitive,omitempty"`
	List      []archiveOperator `json:"list,omitempty"`
}

type archiveTime struct {
	time.Time
}

func (t archiveTime) IsZero() bool {
	return t.Time.IsZero()
}

func (t archiveTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.UTC().Format(time.RFC3339Nano))
}

func (t *archiveTime) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) || bytes.Equal(data, []byte(`""`)) {
		t.Time = time.Time{}
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		parsed, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			return fmt.Errorf("timestamp %q must be RFC3339: %w", text, err)
		}
		t.Time = parsed
		return nil
	}
	var seconds int64
	if err := json.Unmarshal(data, &seconds); err != nil {
		return errors.New("timestamp must be RFC3339 text or legacy Unix seconds")
	}
	t.Time = time.Unix(seconds, 0).UTC()
	return nil
}

func marshalRule(rule state.Rule) ([]byte, error) {
	data, err := json.MarshalIndent(toArchiveRule(rule), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func unmarshalRule(data []byte) (state.Rule, error) {
	var archived archiveRule
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&archived); err != nil {
		return state.Rule{}, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return state.Rule{}, err
	}
	return fromArchiveRule(archived), nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func toArchiveRule(rule state.Rule) archiveRule {
	archived := archiveRule{
		Name:        rule.Name,
		Description: rule.Description,
		Action:      rule.Action,
		Duration:    rule.Duration,
		Operator:    toArchiveOperator(rule.Operator),
		Enabled:     rule.Enabled,
		Precedence:  rule.Precedence,
		NoLog:       rule.NoLog,
	}
	if !rule.CreatedAt.IsZero() {
		archived.Created = &archiveTime{Time: rule.CreatedAt}
	}
	if !rule.UpdatedAt.IsZero() {
		archived.Updated = &archiveTime{Time: rule.UpdatedAt}
	}
	return archived
}

func fromArchiveRule(rule archiveRule) state.Rule {
	converted := state.Rule{
		Name:        rule.Name,
		Description: rule.Description,
		Action:      rule.Action,
		Duration:    rule.Duration,
		Operator:    fromArchiveOperator(rule.Operator),
		Enabled:     rule.Enabled,
		Precedence:  rule.Precedence,
		NoLog:       rule.NoLog,
	}
	if rule.Created != nil {
		converted.CreatedAt = rule.Created.Time
	}
	if rule.Updated != nil {
		converted.UpdatedAt = rule.Updated.Time
	}
	return converted
}

func toArchiveOperator(op state.RuleOperator) archiveOperator {
	archived := archiveOperator{
		Type:      op.Type,
		Operand:   op.Operand,
		Data:      op.Data,
		Sensitive: op.Sensitive,
	}
	if len(op.Children) > 0 {
		archived.List = make([]archiveOperator, len(op.Children))
		for i, child := range op.Children {
			archived.List[i] = toArchiveOperator(child)
		}
	}
	return archived
}

func fromArchiveOperator(op archiveOperator) state.RuleOperator {
	ruleOp := state.RuleOperator{
		Type:      op.Type,
		Operand:   op.Operand,
		Data:      op.Data,
		Sensitive: op.Sensitive,
	}
	if len(op.List) > 0 {
		ruleOp.Children = make([]state.RuleOperator, len(op.List))
		for i, child := range op.List {
			ruleOp.Children[i] = fromArchiveOperator(child)
		}
	}
	return ruleOp
}

func validateBatch(rules []state.Rule, limits Limits) error {
	names := make(map[string]struct{}, len(rules))
	for i, rule := range rules {
		if err := validateRule(rule, limits); err != nil {
			return fmt.Errorf("rule %d: %w", i+1, err)
		}
		if _, ok := names[rule.Name]; ok {
			return fmt.Errorf("duplicate rule name %q", rule.Name)
		}
		names[rule.Name] = struct{}{}
	}
	return nil
}

func validateRule(rule state.Rule, limits Limits) error {
	if strings.TrimSpace(rule.Name) == "" {
		return errors.New("rule name is empty")
	}
	if err := validateText("rule name", rule.Name, limits.MaxNameBytes, false); err != nil {
		return err
	}
	if strings.TrimSpace(rule.Action) == "" {
		return fmt.Errorf("rule %q action is empty", rule.Name)
	}
	if strings.TrimSpace(rule.Duration) == "" {
		return fmt.Errorf("rule %q duration is empty", rule.Name)
	}
	for label, value := range map[string]string{
		"description": rule.Description,
		"action":      rule.Action,
		"duration":    rule.Duration,
	} {
		if err := validateText(label, value, limits.MaxTextBytes, true); err != nil {
			return fmt.Errorf("rule %q: %w", rule.Name, err)
		}
	}
	count := 0
	if err := validateOperator(rule.Operator, limits, 1, &count); err != nil {
		return fmt.Errorf("rule %q operator: %w", rule.Name, err)
	}
	return nil
}

func validateOperator(op state.RuleOperator, limits Limits, depth int, count *int) error {
	if depth > limits.MaxOperatorDepth {
		return fmt.Errorf("tree depth exceeds limit of %d", limits.MaxOperatorDepth)
	}
	*count++
	if *count > limits.MaxOperators {
		return fmt.Errorf("tree contains more than %d operators", limits.MaxOperators)
	}
	if strings.TrimSpace(op.Type) == "" {
		return errors.New("type is empty")
	}
	if err := validateText("type", op.Type, limits.MaxTextBytes, false); err != nil {
		return err
	}
	if err := validateText("operand", op.Operand, limits.MaxTextBytes, false); err != nil {
		return err
	}
	if err := validateText("data", op.Data, limits.MaxDataBytes, false); err != nil {
		return err
	}
	if len(op.Children) > limits.MaxChildren {
		return fmt.Errorf("list contains %d children: limit is %d", len(op.Children), limits.MaxChildren)
	}
	for i, child := range op.Children {
		if err := validateOperator(child, limits, depth+1, count); err != nil {
			return fmt.Errorf("list item %d: %w", i+1, err)
		}
	}
	return nil
}

func validateText(label, value string, maxBytes int, allowNewline bool) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s is not valid UTF-8", label)
	}
	if len(value) > maxBytes {
		return fmt.Errorf("%s is %d bytes: limit is %d", label, len(value), maxBytes)
	}
	for _, r := range value {
		if r == 0 || (r < 0x20 && r != '\t' && (r != '\n' || !allowNewline)) || r == 0x7f {
			return fmt.Errorf("%s contains unsafe control data", label)
		}
	}
	return nil
}

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, dirMode); err != nil {
		return fmt.Errorf("create archive directory %s: %w", path, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect archive directory %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("archive path is not a regular directory: %s", path)
	}
	if err := os.Chmod(path, dirMode); err != nil {
		return fmt.Errorf("secure archive directory %s: %w", path, err)
	}
	return nil
}

func atomicWrite(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".rule-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer func() {
		if err != nil {
			_ = temp.Close()
			_ = os.Remove(tempName)
		}
	}()
	if err = temp.Chmod(fileMode); err != nil {
		return err
	}
	if _, err = temp.Write(data); err != nil {
		return err
	}
	if err = temp.Sync(); err != nil {
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tempName, path); err != nil {
		return err
	}
	if err = os.Chmod(path, fileMode); err != nil {
		return err
	}
	dirHandle, openErr := os.Open(dir)
	if openErr != nil {
		return openErr
	}
	defer dirHandle.Close()
	return dirHandle.Sync()
}

func removeStaleArchives(dir string, expected map[string]struct{}) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read archive directory %s: %w", dir, err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink archive file %s", entry.Name())
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect archive file %s: %w", entry.Name(), err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing non-regular archive file %s", entry.Name())
		}
		if _, ok := expected[entry.Name()]; ok {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			return fmt.Errorf("remove stale archive file %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func readBoundedRegular(path string, maxBytes int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect archive file %s: %w", filepath.Base(path), err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("refusing symlink archive file %s", filepath.Base(path))
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("refusing non-regular archive file %s", filepath.Base(path))
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("archive file %s is %d bytes: limit is %d", filepath.Base(path), info.Size(), maxBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open archive file %s: %w", filepath.Base(path), err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read archive file %s: %w", filepath.Base(path), err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("archive file %s exceeds limit of %d bytes", filepath.Base(path), maxBytes)
	}
	return data, nil
}

func archiveFilename(name string) string {
	return sanitizedComponent(name, "rule") + "-" + shortHash(name) + ".json"
}

func sanitizedComponent(value, fallback string) string {
	var out strings.Builder
	lastDash := false
	for _, r := range strings.TrimSpace(value) {
		safe := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.'
		if safe {
			out.WriteRune(r)
			lastDash = false
		} else if !lastDash {
			out.WriteByte('-')
			lastDash = true
		}
		if out.Len() >= 80 {
			break
		}
	}
	result := strings.Trim(out.String(), ".-_")
	if result == "" {
		result = fallback
	}
	return result
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:5])
}
