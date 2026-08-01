package daemon

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"github.com/adamkadaban/opensnitch-tui/internal/util"
)

// Options configure the daemon RPC server.
type Options struct {
	ListenAddr    string
	MaxMsgBytes   int
	TLS           TLSOptions
	ServerName    string
	ServerVersion string
}

// TLSOptions describe optional TLS configuration for the RPC server.
type TLSOptions struct {
	CertFile string
	KeyFile  string
	ClientCA string
}

// Server exposes the OpenSnitch UI gRPC service so daemons can connect.
type Server struct {
	pb.UnimplementedUIServer

	store *state.Store
	opts  Options
	grpc  *grpc.Server

	sessions             map[string]*session
	sessionsMu           sync.RWMutex
	sessionsClosed       bool
	notifySeqID          uint64
	identityMu           sync.RWMutex
	transportAliases     map[string]string
	nodeTransports       map[string]string
	supersededTransports map[string]string
	prompts              map[string]*promptRequest
	promptsMu            sync.Mutex
}

type session struct {
	transportID string
	nodeID      string
	send        chan *pb.Notification
	done        chan struct{}
	closeOnce   sync.Once

	mu       sync.Mutex
	closeErr error
	pending  map[uint64]*pendingNotification
}

type promptRequest struct {
	mu        sync.Mutex
	id        string
	prompt    state.Prompt
	response  chan promptResponse
	timer     *time.Timer
	timerC    <-chan time.Time
	remaining time.Duration
	pauseCh   chan struct{}
	resumeCh  chan struct{}
}

func (r *promptRequest) stopTimer() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.timer != nil {
		r.timer.Stop()
	}
}

func (r *promptRequest) snapshot() state.Prompt {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.prompt
}

type promptResponse struct {
	rule *pb.Rule
	err  error
}

const (
	defaultPromptTimeout = 30 * time.Second
	ruleTypeSimple       = "simple"
	ruleTypeList         = "list"
	maxRuleNameLength    = 128
	maxRuleDescription   = 240
)

const (
	operandProcessPath = "process.path"
	operandProcessCmd  = "process.command"
	operandProcessID   = "process.id"
	operandUserID      = "user.id"
	operandDestIP      = "dest.ip"
	operandDestHost    = "dest.host"
	operandDestPort    = "dest.port"
	operandChecksumMD5 = "process.hash.md5"
)

// New creates a new daemon RPC server.
func New(store *state.Store, opts Options) *Server {
	if opts.ListenAddr == "" {
		opts.ListenAddr = "unix:///tmp/osui.sock"
	}
	if opts.MaxMsgBytes == 0 {
		opts.MaxMsgBytes = 32 << 20
	}
	if opts.ServerName == "" {
		opts.ServerName = "opensnitch-tui"
	}
	if opts.ServerVersion == "" {
		opts.ServerVersion = "dev"
	}
	return &Server{
		store:                store,
		opts:                 opts,
		sessions:             make(map[string]*session),
		notifySeqID:          notificationIDFloor,
		transportAliases:     make(map[string]string),
		nodeTransports:       make(map[string]string),
		supersededTransports: make(map[string]string),
		prompts:              make(map[string]*promptRequest),
	}
}

// Start begins listening for daemon connections until the context is cancelled.
func (s *Server) Start(ctx context.Context) error {
	target, err := parseListenAddr(s.opts.ListenAddr)
	if err != nil {
		return err
	}
	if target.network == "unix" {
		if err := removeStaleUnixSocket(target.address); err != nil {
			return err
		}
	}
	lis, err := net.Listen(target.network, target.address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.opts.ListenAddr, err)
	}

	serverOpts, err := s.serverOptions()
	if err != nil {
		return err
	}

	s.grpc = grpc.NewServer(serverOpts...)
	pb.RegisterUIServer(s.grpc, s)

	go func() {
		<-ctx.Done()
		s.closeSessions(ctx.Err())
		s.grpc.GracefulStop()
	}()

	if err := s.grpc.Serve(lis); err != nil && err != grpc.ErrServerStopped {
		return err
	}
	return nil
}

// Subscribe registers a daemon session and returns the UI configuration.
func (s *Server) Subscribe(ctx context.Context, cfg *pb.ClientConfig) (*pb.ClientConfig, error) {
	nodeID, err := s.registerNodeAlias(ctx, cfg.GetName())
	if err != nil {
		switch {
		case errors.Is(err, ErrTransportIdentityConflict):
			return nil, status.Error(codes.AlreadyExists, err.Error())
		case errors.Is(err, ErrSupersededNodeTransport):
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		default:
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	node := s.nodeFromContext(ctx, cfg, nodeID)
	node.Message = "subscribed"
	node.Status = state.NodeStatusReady
	node.LastSeen = time.Now()
	s.store.UpsertNode(node)
	s.store.SetNodeConfig(node.ID, parseNodeConfig(cfg.GetConfig(), cfg.GetLogLevel()))
	s.store.SetRules(node.ID, convertRules(cfg.GetRules(), node.ID))
	if firewall, ok := convertSystemFirewall(cfg.GetSystemFirewall(), node.ID, cfg.GetIsFirewallRunning()); ok {
		s.store.SetSystemFirewall(node.ID, firewall)
	} else {
		s.store.RemoveSystemFirewall(node.ID)
	}

	return &pb.ClientConfig{
		Id:                cfg.GetId(),
		Name:              s.opts.ServerName,
		Version:           s.opts.ServerVersion,
		IsFirewallRunning: cfg.GetIsFirewallRunning(),
		Config:            cfg.GetConfig(),
		LogLevel:          cfg.GetLogLevel(),
		Rules:             cfg.GetRules(),
		SystemFirewall:    cfg.GetSystemFirewall(),
	}, nil
}

// Ping stores the latest daemon statistics for display.
func (s *Server) Ping(ctx context.Context, req *pb.PingRequest) (*pb.PingReply, error) {
	transportID := transportIdentity(ctx)
	s.identityMu.RLock()
	nodeID, err := s.resolveTransportLocked(transportID)
	if err != nil {
		s.identityMu.RUnlock()
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	now := time.Now()
	s.store.UpdateNodeStatus(nodeID, state.NodeStatusReady, "last ping", now)

	nodeName := s.nodeName(nodeID)
	stats := convertStats(req.GetStats(), nodeID, nodeName)
	s.store.SetStats(stats)
	s.identityMu.RUnlock()

	return &pb.PingReply{Id: req.GetId()}, nil
}

// Notifications exchanges daemon replies and server-initiated notifications.
func (s *Server) Notifications(stream pb.UI_NotificationsServer) error {
	transportID := transportIdentity(stream.Context())
	sess, err := s.registerTransportSession(transportID)
	if err != nil {
		switch {
		case errors.Is(err, ErrUnknownNotificationNode):
			return status.Error(codes.InvalidArgument, err.Error())
		case errors.Is(err, ErrDuplicateNotificationSession):
			return status.Error(codes.AlreadyExists, err.Error())
		case errors.Is(err, ErrNotificationSessionClosed):
			return status.Error(codes.Unavailable, err.Error())
		case errors.Is(err, ErrSupersededNodeTransport):
			return status.Error(codes.FailedPrecondition, err.Error())
		default:
			return status.Error(codes.Internal, err.Error())
		}
	}

	closeCause := error(ErrNotificationSessionClosed)
	defer func() {
		s.unregisterSession(sess, closeCause)
	}()

	replies := make(chan notificationReceive, 1)
	go receiveNotificationReplies(stream.Context(), stream, replies)

	for {
		select {
		case <-stream.Context().Done():
			closeCause = stream.Context().Err()
			s.updateSessionNodeStatus(sess, state.NodeStatusDisconnected, closeCause.Error())
			return closeCause
		case <-sess.done:
			closeCause = sess.closedError()
			return nil
		case notif := <-sess.send:
			if err := stream.Send(notif); err != nil {
				closeCause = err
				s.updateSessionNodeStatus(sess, state.NodeStatusError, err.Error())
				return err
			}
		case received := <-replies:
			if received.err == io.EOF {
				closeCause = ErrNotificationSessionClosed
				s.updateSessionNodeStatus(sess, state.NodeStatusDisconnected, "notifications closed")
				return nil
			}
			if received.err != nil {
				closeCause = received.err
				s.updateSessionNodeStatus(sess, state.NodeStatusError, received.err.Error())
				return received.err
			}
			if received.reply != nil {
				sess.complete(received.reply)
			}
		}
	}
}

// PostAlert records alert text for the UI.
func (s *Server) PostAlert(ctx context.Context, alert *pb.Alert) (*pb.MsgResponse, error) {
	if alert == nil {
		return &pb.MsgResponse{}, nil
	}
	transportID := transportIdentity(ctx)
	s.identityMu.RLock()
	nodeID, err := s.resolveTransportLocked(transportID)
	if err != nil {
		s.identityMu.RUnlock()
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	converted := convertAlert(alert, nodeID)
	s.store.AddAlert(converted)
	s.identityMu.RUnlock()
	return &pb.MsgResponse{Id: alert.GetId()}, nil
}

func (s *Server) AskRule(ctx context.Context, conn *pb.Connection) (*pb.Rule, error) {
	transportID := transportIdentity(ctx)
	s.identityMu.RLock()
	nodeID, err := s.resolveTransportLocked(transportID)
	if err != nil {
		s.identityMu.RUnlock()
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	nodeName := s.nodeName(nodeID)
	now := time.Now()
	timeout := s.promptTimeout()
	prompt := state.Prompt{
		ID:          fmt.Sprintf("%s:%d", nodeID, now.UnixNano()),
		NodeID:      nodeID,
		NodeName:    nodeName,
		Connection:  convertConnection(conn),
		RequestedAt: now,
		ExpiresAt:   now.Add(timeout),
	}
	req := &promptRequest{
		id:       prompt.ID,
		prompt:   prompt,
		response: make(chan promptResponse, 1),
	}
	req.timer = time.NewTimer(timeout)
	req.timerC = req.timer.C
	s.registerPrompt(req)
	s.store.AddPrompt(prompt)
	s.identityMu.RUnlock()
	defer s.unregisterPrompt(req.id)
	defer req.stopTimer()

	for {
		req.mu.Lock()
		timerC := req.timerC
		req.mu.Unlock()

		select {
		case resp := <-req.response:
			s.store.RemovePrompt(req.id)
			return resp.rule, resp.err
		case <-timerC:
			s.store.RemovePrompt(req.id)
			currentPrompt := req.snapshot()
			s.store.SetError(fmt.Sprintf("prompt timed out for %s", displayConnectionLabel(currentPrompt.Connection)))
			decision := s.defaultPromptDecision(currentPrompt)
			rule, err := s.buildRuleFromDecision(currentPrompt, decision)
			if err == nil {
				stateRule := convertRule(rule, currentPrompt.NodeID)
				s.store.AddRule(currentPrompt.NodeID, stateRule)
			}
			return rule, err
		case <-req.pauseCh:
			select {
			case <-req.resumeCh:
			case <-ctx.Done():
				s.store.RemovePrompt(req.id)
				return nil, ctx.Err()
			}
		case <-ctx.Done():
			s.store.RemovePrompt(req.id)
			return nil, ctx.Err()
		}
	}
}

func (s *Server) serverOptions() ([]grpc.ServerOption, error) {
	kaParams := keepalive.ServerParameters{
		Time:    30 * time.Second,
		Timeout: 20 * time.Second,
	}
	opts := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(s.opts.MaxMsgBytes),
		grpc.MaxSendMsgSize(s.opts.MaxMsgBytes),
		grpc.KeepaliveParams(kaParams),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             15 * time.Second,
			PermitWithoutStream: true,
		}),
	}
	if s.opts.TLS.CertFile != "" && s.opts.TLS.KeyFile != "" {
		cred, err := s.loadTLSCreds()
		if err != nil {
			return nil, err
		}
		opts = append(opts, grpc.Creds(cred))
	}
	return opts, nil
}

func (s *Server) loadTLSCreds() (credentials.TransportCredentials, error) {
	cert, err := tls.LoadX509KeyPair(s.opts.TLS.CertFile, s.opts.TLS.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load tls keypair: %w", err)
	}
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}}
	if s.opts.TLS.ClientCA != "" {
		caData, err := os.ReadFile(s.opts.TLS.ClientCA)
		if err != nil {
			return nil, fmt.Errorf("read client ca: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caData) {
			return nil, fmt.Errorf("append client ca certs")
		}
		tlsConfig.ClientCAs = pool
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return credentials.NewTLS(tlsConfig), nil
}

func (s *Server) nodeFromContext(ctx context.Context, cfg *pb.ClientConfig, nodeID string) state.Node {
	name := normalizedDaemonName(cfg.GetName())
	return state.Node{
		ID:              nodeID,
		Name:            name,
		Address:         peerAddress(ctx),
		Version:         cfg.GetVersion(),
		FirewallEnabled: cfg.GetIsFirewallRunning(),
		Status:          state.NodeStatusConnecting,
		Message:         "connecting",
		LastSeen:        time.Now(),
	}
}

func (s *Server) nodeName(id string) string {
	snapshot := s.store.Snapshot()
	for _, node := range snapshot.Nodes {
		if node.ID == id {
			if node.Name != "" {
				return node.Name
			}
			return node.Address
		}
	}
	return id
}

func (s *Server) EnableRule(nodeID, ruleName string) error {
	return s.enqueueRuleAction(nodeID, ruleName, pb.Action_ENABLE_RULE, func(rule *state.Rule) {
		rule.Enabled = true
	})
}

func (s *Server) DisableRule(nodeID, ruleName string) error {
	return s.enqueueRuleAction(nodeID, ruleName, pb.Action_DISABLE_RULE, func(rule *state.Rule) {
		rule.Enabled = false
	})
}

func (s *Server) DeleteRule(nodeID, ruleName string) error {
	rule, err := s.lookupRule(nodeID, ruleName)
	if err != nil {
		return err
	}
	notif := s.newNotification(pb.Action_DELETE_RULE, nodeID)
	notif.Rules = []*pb.Rule{serializeRule(rule)}
	if err := s.sendNotification(nodeID, notif); err != nil {
		return err
	}
	s.store.RemoveRule(nodeID, ruleName)
	return nil
}

func (s *Server) ChangeRule(nodeID string, rule state.Rule) error {
	if rule.Name == "" {
		return errors.New("rule name required")
	}
	notif := s.newNotification(pb.Action_CHANGE_RULE, nodeID)
	notif.Rules = []*pb.Rule{serializeRule(rule)}
	if err := s.sendNotification(nodeID, notif); err != nil {
		return err
	}
	s.store.UpdateRule(nodeID, rule.Name, func(r *state.Rule) { *r = rule })
	return nil
}

// ApplyRules sends one acknowledged CHANGE_RULE batch and updates state only after OK.
func (s *Server) ApplyRules(ctx context.Context, nodeID string, rules []state.Rule) error {
	if strings.TrimSpace(nodeID) == "" {
		return errors.New("node id required")
	}
	if len(rules) == 0 {
		return errors.New("at least one rule is required")
	}
	seen := make(map[string]struct{}, len(rules))
	protoRules := make([]*pb.Rule, len(rules))
	applied := make([]state.Rule, len(rules))
	for i, rule := range rules {
		if strings.TrimSpace(rule.Name) == "" {
			return fmt.Errorf("rule %d name required", i+1)
		}
		if _, ok := seen[rule.Name]; ok {
			return fmt.Errorf("duplicate rule name %q", rule.Name)
		}
		seen[rule.Name] = struct{}{}
		rule.NodeID = nodeID
		applied[i] = rule
		protoRules[i] = serializeRule(rule)
	}
	notif := &pb.Notification{
		Type:  pb.Action_CHANGE_RULE,
		Rules: protoRules,
	}
	if _, err := s.SendNotification(ctx, nodeID, notif); err != nil {
		return err
	}
	s.store.ApplyRules(nodeID, applied)
	return nil
}

func (s *Server) enqueueRuleAction(nodeID, ruleName string, action pb.Action, mutate func(*state.Rule)) error {
	rule, err := s.lookupRule(nodeID, ruleName)
	if err != nil {
		return err
	}
	if mutate != nil {
		mutate(&rule)
	}
	notif := s.newNotification(action, nodeID)
	notif.Rules = []*pb.Rule{serializeRule(rule)}
	if err := s.sendNotification(nodeID, notif); err != nil {
		return err
	}
	if mutate != nil {
		s.store.UpdateRule(nodeID, ruleName, mutate)
	}
	return nil
}

func (s *Server) newNotification(action pb.Action, nodeID string) *pb.Notification {
	return &pb.Notification{
		Id:         s.nextNotificationID(),
		Type:       action,
		ServerName: s.opts.ServerName,
		ClientName: nodeID,
	}
}

func (s *Server) sendNotification(nodeID string, notif *pb.Notification) error {
	s.sessionsMu.RLock()
	sess, ok := s.sessions[nodeID]
	s.sessionsMu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotificationNodeDisconnected, nodeID)
	}
	return sess.enqueue(notif)
}

func (s *Server) lookupRule(nodeID, ruleName string) (state.Rule, error) {
	snapshot := s.store.Snapshot()
	for _, rule := range snapshot.Rules[nodeID] {
		if rule.Name == ruleName {
			return rule, nil
		}
	}
	return state.Rule{}, fmt.Errorf("rule %s not found for %s", ruleName, nodeID)
}

// ResolvePrompt implements controller.PromptManager.
func (s *Server) ResolvePrompt(decision controller.PromptDecision) error {
	if decision.PromptID == "" {
		return fmt.Errorf("prompt id required")
	}
	req := s.promptByID(decision.PromptID)
	if req == nil {
		return fmt.Errorf("prompt %s not found", decision.PromptID)
	}
	prompt := req.snapshot()
	rule, err := s.buildRuleFromDecision(prompt, decision)
	if err != nil {
		return err
	}
	stateRule := convertRule(rule, prompt.NodeID)
	s.store.AddRule(prompt.NodeID, stateRule)
	select {
	case req.response <- promptResponse{rule: rule}:
		s.store.RemovePrompt(decision.PromptID)
		return nil
	default:
		return fmt.Errorf("prompt %s already resolved", decision.PromptID)
	}
}

func (s *Server) registerPrompt(req *promptRequest) {
	s.promptsMu.Lock()
	s.prompts[req.id] = req
	s.promptsMu.Unlock()
}

func (s *Server) unregisterPrompt(id string) {
	s.promptsMu.Lock()
	delete(s.prompts, id)
	s.promptsMu.Unlock()
}

// PausePrompt stops the prompt timer and records remaining duration.
func (s *Server) PausePrompt(promptID string) error {
	req := s.promptByID(promptID)
	if req == nil {
		return fmt.Errorf("prompt %s not found", promptID)
	}

	req.mu.Lock()
	if req.timer == nil {
		req.mu.Unlock()
		return fmt.Errorf("prompt %s has no timer", promptID)
	}
	if req.remaining > 0 {
		remaining := req.remaining
		req.mu.Unlock()
		s.store.UpdatePrompt(promptID, func(p *state.Prompt) {
			p.Paused = true
			p.Remaining = remaining
		})
		return nil
	}
	if !req.timer.Stop() {
		req.mu.Unlock()
		return fmt.Errorf("prompt %s timer already expired", promptID)
	}
	req.remaining = time.Until(req.prompt.ExpiresAt)
	if req.remaining < 0 {
		req.remaining = 0
	}
	req.timerC = nil
	remaining := req.remaining
	req.mu.Unlock()

	select {
	case req.pauseCh <- struct{}{}:
	default:
	}
	s.store.UpdatePrompt(promptID, func(p *state.Prompt) {
		p.Paused = true
		p.Remaining = remaining
	})
	return nil
}

// ResumePrompt restarts the prompt timer with the remaining duration.
func (s *Server) ResumePrompt(promptID string) error {
	req := s.promptByID(promptID)
	if req == nil {
		return fmt.Errorf("prompt %s not found", promptID)
	}

	req.mu.Lock()
	if req.remaining <= 0 {
		req.mu.Unlock()
		s.store.UpdatePrompt(promptID, func(p *state.Prompt) {
			p.Paused = false
			p.Remaining = 0
		})
		return nil // not paused or nothing to resume
	}
	req.timer = time.NewTimer(req.remaining)
	req.timerC = req.timer.C
	req.prompt.ExpiresAt = time.Now().Add(req.remaining)
	expiresAt := req.prompt.ExpiresAt
	req.remaining = 0
	req.mu.Unlock()

	s.store.UpdatePrompt(promptID, func(p *state.Prompt) {
		p.Paused = false
		p.Remaining = 0
		p.ExpiresAt = expiresAt
	})
	select {
	case req.resumeCh <- struct{}{}:
	default:
	}
	return nil
}

func (s *Server) promptByID(id string) *promptRequest {
	s.promptsMu.Lock()
	defer s.promptsMu.Unlock()
	return s.prompts[id]
}

func (s *Server) defaultPromptDecision(prompt state.Prompt) controller.PromptDecision {
	decision := controller.PromptDecision{
		PromptID: prompt.ID,
		Action:   controller.PromptActionDeny,
		Duration: controller.PromptDurationOnce,
		Target:   bestAvailableTarget(prompt.Connection),
	}
	settings := s.store.Snapshot().Settings
	if settings.DefaultPromptAction != "" {
		decision.Action = controller.PromptAction(settings.DefaultPromptAction)
	}
	if settings.DefaultPromptDuration != "" {
		decision.Duration = controller.PromptDuration(settings.DefaultPromptDuration)
	}
	if preferred := controller.PromptTarget(settings.DefaultPromptTarget); preferred != "" && targetAvailable(prompt.Connection, preferred) {
		decision.Target = preferred
	}
	decision.Action = normalizePromptAction(decision.Action)
	decision.Duration = normalizePromptDuration(decision.Duration)
	return decision
}

func (s *Server) buildRuleFromDecision(prompt state.Prompt, decision controller.PromptDecision) (*pb.Rule, error) {
	decision.Action = normalizePromptAction(decision.Action)
	decision.Duration = normalizePromptDuration(decision.Duration)
	if decision.Target == "" {
		decision.Target = bestAvailableTarget(prompt.Connection)
	}
	operator, err := operatorForDecision(prompt.Connection, decision)
	if err != nil {
		return nil, err
	}
	name := generateRuleName(prompt, operator, decision.Action, decision.Duration, decision.Target, s.store)
	return &pb.Rule{
		Created:     time.Now().Unix(),
		Name:        name,
		Description: ruleDescription(operator, decision.Action, decision.Duration),
		Enabled:     true,
		Action:      string(decision.Action),
		Duration:    string(decision.Duration),
		Operator:    operator,
	}, nil
}

var nonSlugChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func generateRuleName(prompt state.Prompt, op *pb.Operator, action controller.PromptAction, duration controller.PromptDuration, target controller.PromptTarget, store *state.Store) string {
	parts := []string{}
	if s := slugify(string(action)); s != "" {
		parts = append(parts, s)
	}
	if s := slugify(string(duration)); s != "" {
		parts = append(parts, s)
	}
	if op != nil {
		if t := slugify(op.Type); t != "" {
			parts = append(parts, t)
		}
		if operand := operandSlug(op, prompt.Connection, target); operand != "" {
			parts = append(parts, operand)
		}
	} else {
		if operand := operandSlug(nil, prompt.Connection, target); operand != "" {
			parts = append(parts, operand)
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "rule")
	}
	base := truncateSlug(strings.Join(parts, "-"), maxRuleNameLength)
	return ensureUniqueRuleName(base, prompt.NodeID, store)
}

func operandSlug(op *pb.Operator, conn state.Connection, target controller.PromptTarget) string {
	if op != nil {
		if len(op.List) > 0 {
			operands := make([]string, 0, len(op.List))
			for _, child := range op.List {
				if child == nil {
					continue
				}
				if operand := slugify(child.GetOperand()); operand != "" {
					operands = append(operands, operand)
				}
			}
			return strings.Join(operands, "-")
		}
		if op.Data != "" {
			return slugify(op.Data)
		}
		switch op.Operand {
		case operandProcessPath:
			if conn.ProcessPath != "" {
				return slugify(conn.ProcessPath)
			}
		case operandProcessCmd:
			cmdLine := strings.TrimSpace(strings.Join(conn.ProcessArgs, " "))
			if cmdLine != "" {
				return slugify(cmdLine)
			}
			if conn.ProcessPath != "" {
				return slugify(conn.ProcessPath)
			}
		case operandDestHost:
			if conn.DstHost != "" {
				host := conn.DstHost
				if conn.DstPort != 0 {
					host = fmt.Sprintf("%s-%d", host, conn.DstPort)
				}
				return slugify(host)
			}
		case operandDestIP:
			if conn.DstIP != "" {
				ip := conn.DstIP
				if conn.DstPort != 0 {
					ip = fmt.Sprintf("%s-%d", ip, conn.DstPort)
				}
				return slugify(ip)
			}
		case operandDestPort:
			if conn.DstPort != 0 {
				return slugify(fmt.Sprintf("%d", conn.DstPort))
			}
		}
	}
	switch target {
	case controller.PromptTargetProcessPath:
		return slugify(conn.ProcessPath)
	case controller.PromptTargetProcessCmd:
		cmdLine := strings.TrimSpace(strings.Join(conn.ProcessArgs, " "))
		if cmdLine != "" {
			return slugify(cmdLine)
		}
		return slugify(conn.ProcessPath)
	case controller.PromptTargetDestinationHost:
		host := conn.DstHost
		if conn.DstPort != 0 {
			host = fmt.Sprintf("%s-%d", host, conn.DstPort)
		}
		return slugify(host)
	case controller.PromptTargetDestinationIP:
		ip := conn.DstIP
		if conn.DstPort != 0 {
			ip = fmt.Sprintf("%s-%d", ip, conn.DstPort)
		}
		return slugify(ip)
	case controller.PromptTargetDestinationPort:
		if conn.DstPort != 0 {
			return slugify(fmt.Sprintf("%d", conn.DstPort))
		}
	case controller.PromptTargetUserID:
		return slugify(fmt.Sprintf("uid-%d", conn.UserID))
	case controller.PromptTargetProcessID:
		if conn.ProcessID != 0 {
			return slugify(fmt.Sprintf("pid-%d", conn.ProcessID))
		}
	case controller.PromptTargetChecksumMD5:
		return slugify(operandChecksumMD5)
	}
	return ""
}

func slugify(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = nonSlugChars.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-._")
	return s
}

func truncateSlug(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = strings.Trim(value[:limit], "-._")
	if value == "" {
		return "rule"
	}
	return value
}

func ensureUniqueRuleName(base string, nodeID string, store *state.Store) string {
	base = truncateSlug(base, maxRuleNameLength)
	if store == nil {
		return base
	}
	existing := map[string]struct{}{}
	for _, r := range store.Snapshot().Rules[nodeID] {
		existing[r.Name] = struct{}{}
	}
	if _, ok := existing[base]; !ok {
		return base
	}
	for i := 1; ; i++ {
		suffix := fmt.Sprintf("-%d", i)
		candidate := truncateSlug(base, maxRuleNameLength-len(suffix)) + suffix
		if _, ok := existing[candidate]; !ok {
			return candidate
		}
	}
}

type operatorData struct {
	Type    string `json:"type"`
	Operand string `json:"operand"`
	Data    string `json:"data"`
}

var conditionTargetOrder = []controller.PromptTarget{
	controller.PromptTargetDestinationIP,
	controller.PromptTargetDestinationPort,
	controller.PromptTargetUserID,
	controller.PromptTargetChecksumMD5,
	controller.PromptTargetProcessPath,
	controller.PromptTargetProcessCmd,
	controller.PromptTargetProcessID,
	controller.PromptTargetDestinationHost,
}

func operatorForDecision(conn state.Connection, decision controller.PromptDecision) (*pb.Operator, error) {
	requested := make(map[controller.PromptTarget]bool, len(decision.Conditions))
	for _, condition := range decision.Conditions {
		if condition.Target == "" {
			return nil, fmt.Errorf("condition target required")
		}
		if condition.Target == decision.Target {
			continue
		}
		requested[condition.Target] = true
	}
	if decision.Target == controller.PromptTargetProcessCmd && commandNeedsProcessPath(conn) {
		requested[controller.PromptTargetProcessPath] = true
	}

	operators := make([]*pb.Operator, 0, len(requested)+1)
	for _, target := range conditionTargetOrder {
		if !requested[target] {
			continue
		}
		operator, err := operatorForTarget(conn, target)
		if err != nil {
			return nil, fmt.Errorf("%s condition: %w", target, err)
		}
		operators = append(operators, operator)
		delete(requested, target)
	}
	if len(requested) > 0 {
		for target := range requested {
			return nil, fmt.Errorf("unsupported target %s", target)
		}
	}

	base, err := operatorForTarget(conn, decision.Target)
	if err != nil {
		return nil, fmt.Errorf("%s condition: %w", decision.Target, err)
	}
	operators = append(operators, base)
	if len(operators) == 1 {
		return operators[0], nil
	}

	data := make([]operatorData, len(operators))
	for i, operator := range operators {
		data[i] = operatorData{
			Type:    operator.GetType(),
			Operand: operator.GetOperand(),
			Data:    operator.GetData(),
		}
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("encode composite operator: %w", err)
	}
	return &pb.Operator{
		Type:    ruleTypeList,
		Operand: ruleTypeList,
		Data:    string(encoded),
		List:    operators,
	}, nil
}

func commandNeedsProcessPath(conn state.Connection) bool {
	if len(conn.ProcessArgs) == 0 {
		return false
	}
	command := strings.TrimSpace(conn.ProcessArgs[0])
	return !filepath.IsAbs(command) || strings.HasPrefix(filepath.Clean(command), "/proc/")
}

func operatorForTarget(conn state.Connection, target controller.PromptTarget) (*pb.Operator, error) {
	switch target {
	case controller.PromptTargetProcessPath:
		path := strings.TrimSpace(conn.ProcessPath)
		if path == "" {
			return nil, fmt.Errorf("process path unavailable")
		}
		return simpleOperator(operandProcessPath, path), nil
	case controller.PromptTargetProcessCmd:
		cmdLine := strings.TrimSpace(strings.Join(conn.ProcessArgs, " "))
		if cmdLine == "" {
			return nil, fmt.Errorf("command line unavailable")
		}
		return simpleOperator(operandProcessCmd, cmdLine), nil
	case controller.PromptTargetProcessID:
		if conn.ProcessID == 0 {
			return nil, fmt.Errorf("process id unavailable")
		}
		return simpleOperator(operandProcessID, fmt.Sprintf("%d", conn.ProcessID)), nil
	case controller.PromptTargetUserID:
		return simpleOperator(operandUserID, fmt.Sprintf("%d", conn.UserID)), nil
	case controller.PromptTargetDestinationIP:
		ip := strings.TrimSpace(conn.DstIP)
		if ip == "" {
			return nil, fmt.Errorf("destination ip unavailable")
		}
		return simpleOperator(operandDestIP, ip), nil
	case controller.PromptTargetDestinationHost:
		host := strings.TrimSpace(conn.DstHost)
		if host == "" {
			return nil, fmt.Errorf("destination host unavailable")
		}
		return simpleOperator(operandDestHost, host), nil
	case controller.PromptTargetDestinationPort:
		if conn.DstPort == 0 {
			return nil, fmt.Errorf("destination port unavailable")
		}
		return simpleOperator(operandDestPort, fmt.Sprintf("%d", conn.DstPort)), nil
	case controller.PromptTargetChecksumMD5:
		checksum := strings.TrimSpace(conn.ProcessChecksums[operandChecksumMD5])
		if checksum == "" {
			return nil, fmt.Errorf("md5 checksum unavailable")
		}
		return simpleOperator(operandChecksumMD5, checksum), nil
	default:
		return nil, fmt.Errorf("unsupported target %s", target)
	}
}

func simpleOperator(operand, data string) *pb.Operator {
	return &pb.Operator{
		Type:    ruleTypeSimple,
		Operand: operand,
		Data:    data,
	}
}

func ruleDescription(operator *pb.Operator, action controller.PromptAction, duration controller.PromptDuration) string {
	operands := []string{}
	if operator != nil {
		if len(operator.GetList()) > 0 {
			for _, child := range operator.GetList() {
				if child != nil && child.GetOperand() != "" {
					operands = append(operands, child.GetOperand())
				}
			}
		} else if operator.GetOperand() != "" {
			operands = append(operands, operator.GetOperand())
		}
	}
	description := fmt.Sprintf("%s connection for %s matching %s.",
		strings.ToUpper(string(action[:1]))+string(action[1:]),
		duration,
		strings.Join(operands, " + "),
	)
	return util.TruncateString(description, maxRuleDescription)
}

func displayConnectionLabel(conn state.Connection) string {
	dest := conn.DstHost
	if dest == "" {
		dest = conn.DstIP
	}
	return fmt.Sprintf("%s -> %s:%d", util.Fallback(conn.ProcessPath, "unknown"), util.Fallback(dest, "destination"), conn.DstPort)
}

func normalizePromptAction(action controller.PromptAction) controller.PromptAction {
	switch action {
	case controller.PromptActionAllow, controller.PromptActionDeny, controller.PromptActionReject:
		return action
	default:
		return controller.PromptActionDeny
	}
}

func normalizePromptDuration(duration controller.PromptDuration) controller.PromptDuration {
	switch duration {
	case controller.PromptDurationOnce,
		controller.PromptDuration30Seconds,
		controller.PromptDuration5Minutes,
		controller.PromptDuration15Minutes,
		controller.PromptDuration30Minutes,
		controller.PromptDuration1Hour,
		controller.PromptDuration12Hours,
		controller.PromptDurationUntilRestart,
		controller.PromptDurationAlways:
		return duration
	default:
		return controller.PromptDurationOnce
	}
}

func targetAvailable(conn state.Connection, target controller.PromptTarget) bool {
	switch target {
	case controller.PromptTargetProcessPath:
		return strings.TrimSpace(conn.ProcessPath) != ""
	case controller.PromptTargetProcessCmd:
		return strings.TrimSpace(strings.Join(conn.ProcessArgs, " ")) != ""
	case controller.PromptTargetDestinationHost:
		return strings.TrimSpace(conn.DstHost) != ""
	case controller.PromptTargetDestinationIP:
		return strings.TrimSpace(conn.DstIP) != ""
	case controller.PromptTargetDestinationPort:
		return conn.DstPort != 0
	case controller.PromptTargetProcessID:
		return conn.ProcessID != 0
	case controller.PromptTargetUserID:
		return true
	case controller.PromptTargetChecksumMD5:
		return strings.TrimSpace(conn.ProcessChecksums[operandChecksumMD5]) != ""
	default:
		return false
	}
}

func bestAvailableTarget(conn state.Connection) controller.PromptTarget {
	switch {
	case strings.TrimSpace(conn.ProcessPath) != "":
		return controller.PromptTargetProcessPath
	case strings.TrimSpace(strings.Join(conn.ProcessArgs, " ")) != "":
		return controller.PromptTargetProcessCmd
	case strings.TrimSpace(conn.DstHost) != "":
		return controller.PromptTargetDestinationHost
	case strings.TrimSpace(conn.DstIP) != "":
		return controller.PromptTargetDestinationIP
	case conn.DstPort != 0:
		return controller.PromptTargetDestinationPort
	case conn.ProcessID != 0:
		return controller.PromptTargetProcessID
	default:
		return controller.PromptTargetUserID
	}
}

func peerAddress(ctx context.Context) string {
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		switch p.Addr.Network() {
		case "tcp", "tcp4", "tcp6":
			host, _, err := net.SplitHostPort(p.Addr.String())
			if err == nil {
				return host
			}
		case "unix", "unixpacket":
			return "/local"
		}
		return p.Addr.String()
	}
	return "unknown"
}

func (s *Server) promptTimeout() time.Duration {
	if s == nil || s.store == nil {
		return defaultPromptTimeout
	}
	timeout := s.store.Snapshot().Settings.PromptTimeout
	if timeout <= 0 {
		return defaultPromptTimeout
	}
	return timeout
}
