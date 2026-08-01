package alertarchive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/adamkadaban/opensnitch-tui/internal/alertsafety"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
)

const (
	dirMode  = 0o700
	fileMode = 0o600
)

type Limits struct {
	MaxFileBytes     int
	MaxTextBytes     int
	MaxListItems     int
	MaxMapItems      int
	MaxOperatorDepth int
	MaxOperators     int
}

func DefaultLimits() Limits {
	return Limits{
		MaxFileBytes:     256 << 10,
		MaxTextBytes:     1024,
		MaxListItems:     32,
		MaxMapItems:      32,
		MaxOperatorDepth: 16,
		MaxOperators:     48,
	}
}

type Service struct {
	root   string
	limits Limits
}

func New(root string, limits Limits) (*Service, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("alert archive root is required")
	}
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("alert archive root must be absolute: %s", root)
	}
	if limits.MaxFileBytes <= 0 || limits.MaxTextBytes <= 0 || limits.MaxListItems <= 0 ||
		limits.MaxMapItems <= 0 || limits.MaxOperatorDepth <= 0 || limits.MaxOperators <= 0 {
		return nil, errors.New("alert archive limits must be positive")
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
		return filepath.Join(dataHome, "opensnitch-tui", "alerts"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "opensnitch-tui", "alerts"), nil
}

func (s *Service) Directory() string {
	return s.root
}

func (s *Service) Export(ctx context.Context, alert state.Alert) (string, error) {
	path := filepath.Join(s.root, archiveFilename(alert))
	if err := ctx.Err(); err != nil {
		return path, err
	}
	data, err := s.marshal(alert)
	if err != nil {
		return path, err
	}
	if len(data) > s.limits.MaxFileBytes {
		return path, fmt.Errorf("alert archive is %d bytes: limit is %d", len(data), s.limits.MaxFileBytes)
	}
	if err := ensureSafeParent(filepath.Dir(s.root)); err != nil {
		return path, err
	}
	if err := ensurePrivateDir(s.root); err != nil {
		return path, err
	}
	if err := rejectUnsafeTarget(path); err != nil {
		return path, err
	}
	if err := atomicWrite(path, data); err != nil {
		return path, fmt.Errorf("write alert archive: %w", err)
	}
	return path, nil
}

type archiveAlert struct {
	ID          string                 `json:"id"`
	NodeID      string                 `json:"node_id,omitempty"`
	CreatedAt   string                 `json:"created_at,omitempty"`
	Priority    state.AlertPriority    `json:"priority"`
	Type        state.AlertType        `json:"type"`
	Action      state.AlertAction      `json:"action"`
	What        state.AlertWhat        `json:"what"`
	PayloadKind state.AlertPayloadKind `json:"payload_kind,omitempty"`
	Payload     any                    `json:"payload,omitempty"`
}

func (s *Service) marshal(alert state.Alert) ([]byte, error) {
	archived := archiveAlert{
		ID:          alertsafety.LimitText(alert.ID, s.limits.MaxTextBytes),
		NodeID:      alertsafety.LimitText(alert.NodeID, s.limits.MaxTextBytes),
		Priority:    alert.Priority,
		Type:        alert.Type,
		Action:      alert.Action,
		What:        alert.What,
		PayloadKind: alert.PayloadKind,
	}
	if !alert.CreatedAt.IsZero() {
		archived.CreatedAt = alert.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	switch alert.PayloadKind {
	case state.AlertPayloadText:
		archived.Payload = struct {
			Text string `json:"text"`
		}{Text: s.text(alert.Text)}
	case state.AlertPayloadProcess:
		if alert.Process != nil {
			archived.Payload = s.archiveProcess(*alert.Process)
		}
	case state.AlertPayloadConnection:
		if alert.Connection != nil {
			archived.Payload = s.archiveConnection(*alert.Connection)
		}
	case state.AlertPayloadRule:
		if alert.Rule != nil {
			budget := s.limits.MaxOperators
			archived.Payload = archiveRule(*alert.Rule, s.limits, 0, &budget)
		}
	case state.AlertPayloadFirewall:
		if alert.FirewallRule != nil {
			archived.Payload = s.archiveFirewallRule(*alert.FirewallRule)
		}
	}
	data, err := json.MarshalIndent(archived, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode alert archive: %w", err)
	}
	return append(data, '\n'), nil
}

func (s *Service) archiveProcess(process state.Process) any {
	return struct {
		PID         uint64                   `json:"pid"`
		PPID        uint64                   `json:"ppid"`
		UID         uint64                   `json:"uid"`
		Comm        string                   `json:"comm,omitempty"`
		Path        string                   `json:"path,omitempty"`
		Args        []string                 `json:"args,omitempty"`
		Environment map[string]string        `json:"environment,omitempty"`
		CWD         string                   `json:"cwd,omitempty"`
		Checksums   map[string]string        `json:"checksums,omitempty"`
		IOReads     uint64                   `json:"io_reads,omitempty"`
		IOWrites    uint64                   `json:"io_writes,omitempty"`
		NetReads    uint64                   `json:"net_reads,omitempty"`
		NetWrites   uint64                   `json:"net_writes,omitempty"`
		ProcessTree []state.ProcessTreeEntry `json:"process_tree,omitempty"`
	}{
		PID:         process.PID,
		PPID:        process.PPID,
		UID:         process.UID,
		Comm:        s.text(process.Comm),
		Path:        s.text(process.Path),
		Args:        alertsafety.RedactArgs(process.Args, s.limits.MaxListItems, s.limits.MaxTextBytes),
		Environment: s.redactedEnvironment(process.Env),
		CWD:         s.text(process.CWD),
		Checksums:   s.stringMap(process.Checksums, false),
		IOReads:     process.IOReads,
		IOWrites:    process.IOWrites,
		NetReads:    process.NetReads,
		NetWrites:   process.NetWrites,
		ProcessTree: s.processTree(process.ProcessTree),
	}
}

func (s *Service) archiveConnection(connection state.Connection) any {
	return struct {
		Protocol         string                   `json:"protocol,omitempty"`
		SrcIP            string                   `json:"src_ip,omitempty"`
		SrcPort          uint32                   `json:"src_port,omitempty"`
		DstIP            string                   `json:"dst_ip,omitempty"`
		DstHost          string                   `json:"dst_host,omitempty"`
		DstPort          uint32                   `json:"dst_port,omitempty"`
		UserID           uint32                   `json:"user_id,omitempty"`
		ProcessID        uint32                   `json:"process_id,omitempty"`
		ProcessPath      string                   `json:"process_path,omitempty"`
		ProcessCWD       string                   `json:"process_cwd,omitempty"`
		ProcessArgs      []string                 `json:"process_args,omitempty"`
		ProcessEnv       map[string]string        `json:"process_environment,omitempty"`
		ProcessChecksums map[string]string        `json:"process_checksums,omitempty"`
		ProcessTree      []state.ProcessTreeEntry `json:"process_tree,omitempty"`
	}{
		Protocol:         s.text(connection.Protocol),
		SrcIP:            s.text(connection.SrcIP),
		SrcPort:          connection.SrcPort,
		DstIP:            s.text(connection.DstIP),
		DstHost:          s.text(connection.DstHost),
		DstPort:          connection.DstPort,
		UserID:           connection.UserID,
		ProcessID:        connection.ProcessID,
		ProcessPath:      s.text(connection.ProcessPath),
		ProcessCWD:       s.text(connection.ProcessCWD),
		ProcessArgs:      alertsafety.RedactArgs(connection.ProcessArgs, s.limits.MaxListItems, s.limits.MaxTextBytes),
		ProcessEnv:       s.redactedEnvironment(connection.ProcessEnv),
		ProcessChecksums: s.stringMap(connection.ProcessChecksums, false),
		ProcessTree:      s.processTree(connection.ProcessTree),
	}
}

type archivedRule struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Action      string           `json:"action,omitempty"`
	Duration    string           `json:"duration,omitempty"`
	Enabled     bool             `json:"enabled"`
	Precedence  bool             `json:"precedence"`
	NoLog       bool             `json:"nolog"`
	Operator    archivedOperator `json:"operator"`
	CreatedAt   string           `json:"created_at,omitempty"`
	UpdatedAt   string           `json:"updated_at,omitempty"`
}

type archivedOperator struct {
	Type      string             `json:"type,omitempty"`
	Operand   string             `json:"operand,omitempty"`
	Data      string             `json:"data,omitempty"`
	Sensitive bool               `json:"sensitive,omitempty"`
	Children  []archivedOperator `json:"children,omitempty"`
}

func archiveRule(rule state.Rule, limits Limits, depth int, budget *int) archivedRule {
	archived := archivedRule{
		Name:        archiveText(rule.Name, limits),
		Description: archiveText(rule.Description, limits),
		Action:      archiveText(rule.Action, limits),
		Duration:    archiveText(rule.Duration, limits),
		Enabled:     rule.Enabled,
		Precedence:  rule.Precedence,
		NoLog:       rule.NoLog,
		Operator:    archiveOperator(rule.Operator, limits, depth, budget),
	}
	if !rule.CreatedAt.IsZero() {
		archived.CreatedAt = rule.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	if !rule.UpdatedAt.IsZero() {
		archived.UpdatedAt = rule.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	return archived
}

func archiveOperator(operator state.RuleOperator, limits Limits, depth int, budget *int) archivedOperator {
	if *budget <= 0 || depth >= limits.MaxOperatorDepth {
		return archivedOperator{Type: "truncated"}
	}
	*budget = *budget - 1
	data := archiveText(operator.Data, limits)
	if alertsafety.SensitiveName(operator.Operand) {
		data = alertsafety.Redacted
	}
	archived := archivedOperator{
		Type:      archiveText(operator.Type, limits),
		Operand:   archiveText(operator.Operand, limits),
		Data:      data,
		Sensitive: operator.Sensitive,
	}
	count := min(len(operator.Children), limits.MaxListItems)
	if count > 0 {
		archived.Children = make([]archivedOperator, 0, count)
		for _, child := range operator.Children[:count] {
			if *budget <= 0 {
				break
			}
			archived.Children = append(archived.Children, archiveOperator(child, limits, depth+1, budget))
		}
	}
	return archived
}

func archiveText(value string, limits Limits) string {
	return alertsafety.LimitText(alertsafety.RedactInline(value), limits.MaxTextBytes)
}

func (s *Service) archiveFirewallRule(rule state.FirewallRule) any {
	type value struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	type statement struct {
		Op     string  `json:"op,omitempty"`
		Name   string  `json:"name,omitempty"`
		Values []value `json:"values,omitempty"`
	}
	type expression struct {
		Statement *statement `json:"statement,omitempty"`
	}
	expressions := make([]expression, 0, min(len(rule.Expressions), s.limits.MaxListItems))
	remainingValues := s.limits.MaxListItems
	for _, item := range rule.Expressions[:min(len(rule.Expressions), s.limits.MaxListItems)] {
		if item.Statement == nil {
			expressions = append(expressions, expression{})
			continue
		}
		stmt := &statement{Op: s.text(item.Statement.Op), Name: s.text(item.Statement.Name)}
		valueCount := min(len(item.Statement.Values), remainingValues)
		for _, itemValue := range item.Statement.Values[:valueCount] {
			redacted := s.text(itemValue.Value)
			if alertsafety.SensitiveName(itemValue.Key) {
				redacted = alertsafety.Redacted
			}
			stmt.Values = append(stmt.Values, value{Key: s.text(itemValue.Key), Value: redacted})
		}
		remainingValues -= valueCount
		expressions = append(expressions, expression{Statement: stmt})
		if remainingValues == 0 {
			break
		}
	}
	return struct {
		Table            string       `json:"table,omitempty"`
		Chain            string       `json:"chain,omitempty"`
		UUID             string       `json:"uuid,omitempty"`
		Enabled          bool         `json:"enabled"`
		Position         uint64       `json:"position,omitempty"`
		Description      string       `json:"description,omitempty"`
		Parameters       string       `json:"parameters,omitempty"`
		Expressions      []expression `json:"expressions,omitempty"`
		Target           string       `json:"target,omitempty"`
		TargetParameters string       `json:"target_parameters,omitempty"`
	}{
		Table:            s.text(rule.Table),
		Chain:            s.text(rule.Chain),
		UUID:             s.text(rule.UUID),
		Enabled:          rule.Enabled,
		Position:         rule.Position,
		Description:      s.text(rule.Description),
		Parameters:       s.text(rule.Parameters),
		Expressions:      expressions,
		Target:           s.text(rule.Target),
		TargetParameters: s.text(rule.TargetParameters),
	}
}

func (s *Service) text(value string) string {
	return alertsafety.LimitText(alertsafety.RedactInline(value), s.limits.MaxTextBytes)
}

func (s *Service) processTree(entries []state.ProcessTreeEntry) []state.ProcessTreeEntry {
	count := min(len(entries), s.limits.MaxListItems)
	if count == 0 {
		return nil
	}
	result := make([]state.ProcessTreeEntry, count)
	for i := 0; i < count; i++ {
		result[i] = state.ProcessTreeEntry{Path: s.text(entries[i].Path), PID: entries[i].PID}
	}
	return result
}

func (s *Service) redactedEnvironment(values map[string]string) map[string]string {
	return s.stringMap(values, true)
}

func (s *Service) stringMap(values map[string]string, redactValues bool) map[string]string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, min(len(values), s.limits.MaxMapItems))
	for key := range values {
		keys = append(keys, key)
		if len(keys) == s.limits.MaxMapItems {
			break
		}
	}
	sort.Strings(keys)
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		value := values[key]
		if redactValues || alertsafety.SensitiveName(key) {
			value = alertsafety.Redacted
		}
		result[s.text(key)] = s.text(value)
	}
	return result
}

func ensurePrivateDir(path string) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("alert archive path is not a regular directory: %s", path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect alert archive directory %s: %w", path, err)
	}
	if err := os.MkdirAll(path, dirMode); err != nil {
		return fmt.Errorf("create alert archive directory %s: %w", path, err)
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("alert archive path is not a regular directory: %s", path)
	}
	if err := os.Chmod(path, dirMode); err != nil {
		return fmt.Errorf("secure alert archive directory %s: %w", path, err)
	}
	return nil
}

func ensureSafeParent(path string) error {
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("alert archive path is not a regular directory: %s", path)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect alert archive directory %s: %w", path, err)
	}
	if err := os.MkdirAll(path, dirMode); err != nil {
		return fmt.Errorf("create alert archive directory %s: %w", path, err)
	}
	info, err = os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("alert archive path is not a regular directory: %s", path)
	}
	return os.Chmod(path, dirMode)
}

func rejectUnsafeTarget(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect alert archive file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("refusing non-regular alert archive file %s", filepath.Base(path))
	}
	return nil
}

func atomicWrite(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".alert-*.tmp")
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
	handle, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer handle.Close()
	return handle.Sync()
}

func archiveFilename(alert state.Alert) string {
	timePart := "unknown-time"
	if !alert.CreatedAt.IsZero() {
		timePart = alert.CreatedAt.UTC().Format("20060102T150405.000000000Z")
	}
	source := strings.Join([]string{timePart, alert.NodeID, string(alert.What), alert.ID}, "-")
	return sanitizedComponent(source, "alert") + "-" + shortHash(source) + ".json"
}

func sanitizedComponent(value, fallback string) string {
	var out strings.Builder
	lastDash := false
	for _, r := range strings.TrimSpace(value) {
		safe := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_'
		if safe {
			out.WriteRune(r)
			lastDash = false
		} else if !lastDash {
			out.WriteByte('-')
			lastDash = true
		}
		if out.Len() >= 120 {
			break
		}
	}
	result := strings.Trim(out.String(), ".-_")
	if result == "" {
		return fallback
	}
	return result
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:5])
}
