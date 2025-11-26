package daemon

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/peer"

	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
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

	sessions    map[string]*session
	sessionsMu  sync.Mutex
	notifySeqID uint64
	prompts     map[string]*promptRequest
	promptsMu   sync.Mutex
}

type session struct {
	nodeID string
	send   chan *pb.Notification
}

type promptRequest struct {
	id       string
	prompt   state.Prompt
	response chan promptResponse
}

type promptResponse struct {
	rule *pb.Rule
	err  error
}

const (
	promptTimeout  = 30 * time.Second
	ruleTypeSimple = "simple"
)

const (
	operandProcessPath = "process.path"
	operandProcessCmd  = "process.command"
	operandProcessID   = "process.id"
	operandUserID      = "user.id"
	operandDestIP      = "dest.ip"
	operandDestHost    = "dest.host"
	operandDestPort    = "dest.port"
)

// New creates a new daemon RPC server.
func New(store *state.Store, opts Options) *Server {
	if opts.ListenAddr == "" {
		opts.ListenAddr = "127.0.0.1:50051"
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
	return &Server{store: store, opts: opts, sessions: make(map[string]*session), prompts: make(map[string]*promptRequest)}
}

// Start begins listening for daemon connections until the context is cancelled.
func (s *Server) Start(ctx context.Context) error {
	target, err := parseListenAddr(s.opts.ListenAddr)
	if err != nil {
		return err
	}
	if target.network == "unix" {
		if err := os.Remove(target.address); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove stale socket: %w", err)
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
		s.grpc.GracefulStop()
	}()

	if err := s.grpc.Serve(lis); err != nil && err != grpc.ErrServerStopped {
		return err
	}
	return nil
}

// Subscribe registers a daemon session and returns the UI configuration.
func (s *Server) Subscribe(ctx context.Context, cfg *pb.ClientConfig) (*pb.ClientConfig, error) {
	node := s.nodeFromContext(ctx, cfg)
	node.Message = "subscribed"
	node.Status = state.NodeStatusReady
	node.LastSeen = time.Now()
	s.store.UpsertNode(node)
	s.store.SetRules(node.ID, convertRules(cfg.GetRules(), node.ID))

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
	nodeID := peerKey(ctx)
	now := time.Now()
	s.store.UpdateNodeStatus(nodeID, state.NodeStatusReady, "last ping", now)

	nodeName := s.nodeName(nodeID)
	stats := convertStats(req.GetStats(), nodeID, nodeName)
	s.store.SetStats(stats)

	return &pb.PingReply{Id: req.GetId()}, nil
}

// Notifications drains the streaming channel to keep the daemon connected.
func (s *Server) Notifications(stream pb.UI_NotificationsServer) error {
	nodeID := peerKey(stream.Context())
	sess := s.registerSession(nodeID)
	defer s.unregisterSession(nodeID, sess)

	sendErr := make(chan error, 1)
	go s.dispatchNotifications(stream, sess, sendErr)

	for {
		select {
		case err := <-sendErr:
			if err != nil {
				s.store.UpdateNodeStatus(nodeID, state.NodeStatusError, err.Error(), time.Now())
				return err
			}
			return nil
		default:
		}

		reply, err := stream.Recv()
		if err == io.EOF {
			s.store.UpdateNodeStatus(nodeID, state.NodeStatusDisconnected, "notifications closed", time.Now())
			return nil
		}
		if err != nil {
			s.store.UpdateNodeStatus(nodeID, state.NodeStatusError, err.Error(), time.Now())
			return err
		}
		_ = reply
	}
}

// PostAlert records alert text for the UI.
func (s *Server) PostAlert(ctx context.Context, alert *pb.Alert) (*pb.MsgResponse, error) {
	if alert == nil {
		return &pb.MsgResponse{}, nil
	}
	nodeID := peerKey(ctx)
	converted := convertAlert(alert, nodeID)
	s.store.AddAlert(converted)
	return &pb.MsgResponse{Id: alert.GetId()}, nil
}

func (s *Server) AskRule(ctx context.Context, conn *pb.Connection) (*pb.Rule, error) {
	nodeID := peerKey(ctx)
	nodeName := s.nodeName(nodeID)
	now := time.Now()
	prompt := state.Prompt{
		ID:          fmt.Sprintf("%s:%d", nodeID, now.UnixNano()),
		NodeID:      nodeID,
		NodeName:    nodeName,
		Connection:  convertConnection(conn),
		RequestedAt: now,
		ExpiresAt:   now.Add(promptTimeout),
	}
	req := &promptRequest{
		id:       prompt.ID,
		prompt:   prompt,
		response: make(chan promptResponse, 1),
	}
	s.registerPrompt(req)
	defer s.unregisterPrompt(req.id)

	s.store.AddPrompt(prompt)
	timer := time.NewTimer(promptTimeout)
	defer timer.Stop()

	select {
	case resp := <-req.response:
		s.store.RemovePrompt(req.id)
		return resp.rule, resp.err
	case <-timer.C:
		s.store.RemovePrompt(req.id)
		s.store.SetError(fmt.Sprintf("prompt timed out for %s", displayConnectionLabel(prompt.Connection)))
		decision := s.defaultPromptDecision(prompt)
		rule, err := buildRuleFromDecision(prompt, decision)
		return rule, err
	case <-ctx.Done():
		s.store.RemovePrompt(req.id)
		return nil, ctx.Err()
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

func (s *Server) nodeFromContext(ctx context.Context, cfg *pb.ClientConfig) state.Node {
	nodeID := peerKey(ctx)
	name := cfg.GetName()
	if name == "" {
		name = nodeID
	}
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

func (s *Server) dispatchNotifications(stream pb.UI_NotificationsServer, sess *session, errCh chan<- error) {
	for notif := range sess.send {
		if err := stream.Send(notif); err != nil {
			errCh <- err
			return
		}
	}
	errCh <- nil
}

func (s *Server) registerSession(nodeID string) *session {
	sess := &session{nodeID: nodeID, send: make(chan *pb.Notification, 8)}
	s.sessionsMu.Lock()
	if existing, ok := s.sessions[nodeID]; ok {
		if existing.send != nil {
			close(existing.send)
			existing.send = nil
		}
	}
	s.sessions[nodeID] = sess
	s.sessionsMu.Unlock()
	return sess
}

func (s *Server) unregisterSession(nodeID string, sess *session) {
	s.sessionsMu.Lock()
	if current, ok := s.sessions[nodeID]; ok && current == sess {
		delete(s.sessions, nodeID)
	}
	s.sessionsMu.Unlock()
	if sess.send != nil {
		close(sess.send)
		sess.send = nil
	}
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
	id := atomic.AddUint64(&s.notifySeqID, 1)
	return &pb.Notification{
		Id:         id,
		Type:       action,
		ServerName: s.opts.ServerName,
		ClientName: nodeID,
	}
}

func (s *Server) sendNotification(nodeID string, notif *pb.Notification) error {
	s.sessionsMu.Lock()
	sess, ok := s.sessions[nodeID]
	s.sessionsMu.Unlock()
	if !ok {
		return fmt.Errorf("node %s not connected", nodeID)
	}
	select {
	case sess.send <- notif:
		return nil
	default:
		return fmt.Errorf("notification buffer full for %s", nodeID)
	}
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
	rule, err := buildRuleFromDecision(req.prompt, decision)
	if err != nil {
		return err
	}
	stateRule := convertRule(rule, req.prompt.NodeID)
	s.store.AddRule(req.prompt.NodeID, stateRule)
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

func buildRuleFromDecision(prompt state.Prompt, decision controller.PromptDecision) (*pb.Rule, error) {
	decision.Action = normalizePromptAction(decision.Action)
	decision.Duration = normalizePromptDuration(decision.Duration)
	if decision.Target == "" {
		decision.Target = bestAvailableTarget(prompt.Connection)
	}
	operator, err := operatorForTarget(prompt.Connection, decision.Target)
	if err != nil {
		return nil, err
	}
	return &pb.Rule{
		Created:  time.Now().Unix(),
		Name:     fmt.Sprintf("user-%d", time.Now().UnixNano()),
		Enabled:  true,
		Action:   string(decision.Action),
		Duration: string(decision.Duration),
		Operator: operator,
	}, nil
}

func operatorForTarget(conn state.Connection, target controller.PromptTarget) (*pb.Operator, error) {
	switch target {
	case controller.PromptTargetProcessPath:
		if conn.ProcessPath == "" {
			return nil, fmt.Errorf("process path unavailable")
		}
		return simpleOperator(operandProcessPath, conn.ProcessPath), nil
	case controller.PromptTargetProcessCmd:
		cmdLine := strings.TrimSpace(strings.Join(conn.ProcessArgs, " "))
		if cmdLine == "" {
			if conn.ProcessPath == "" {
				return nil, fmt.Errorf("command line unavailable")
			}
			return simpleOperator(operandProcessPath, conn.ProcessPath), nil
		}
		return simpleOperator(operandProcessCmd, cmdLine), nil
	case controller.PromptTargetProcessID:
		return simpleOperator(operandProcessID, fmt.Sprintf("%d", conn.ProcessID)), nil
	case controller.PromptTargetUserID:
		return simpleOperator(operandUserID, fmt.Sprintf("%d", conn.UserID)), nil
	case controller.PromptTargetDestinationIP:
		if conn.DstIP == "" {
			return nil, fmt.Errorf("destination ip unavailable")
		}
		return simpleOperator(operandDestIP, conn.DstIP), nil
	case controller.PromptTargetDestinationHost:
		if conn.DstHost == "" {
			return nil, fmt.Errorf("destination host unavailable")
		}
		return simpleOperator(operandDestHost, conn.DstHost), nil
	case controller.PromptTargetDestinationPort:
		if conn.DstPort == 0 {
			return nil, fmt.Errorf("destination port unavailable")
		}
		return simpleOperator(operandDestPort, fmt.Sprintf("%d", conn.DstPort)), nil
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

func displayConnectionLabel(conn state.Connection) string {
	dest := conn.DstHost
	if dest == "" {
		dest = conn.DstIP
	}
	return fmt.Sprintf("%s -> %s:%d", fallbackString(conn.ProcessPath, "unknown"), fallbackString(dest, "destination"), conn.DstPort)
}

func fallbackString(value, def string) string {
	if value == "" {
		return def
	}
	return value
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
	case controller.PromptDurationOnce, controller.PromptDurationUntilRestart, controller.PromptDurationAlways:
		return duration
	default:
		return controller.PromptDurationOnce
	}
}

func targetAvailable(conn state.Connection, target controller.PromptTarget) bool {
	switch target {
	case controller.PromptTargetProcessPath:
		return conn.ProcessPath != ""
	case controller.PromptTargetProcessCmd:
		return len(conn.ProcessArgs) > 0 || conn.ProcessPath != ""
	case controller.PromptTargetDestinationHost:
		return conn.DstHost != ""
	case controller.PromptTargetDestinationIP:
		return conn.DstIP != ""
	case controller.PromptTargetDestinationPort:
		return conn.DstPort != 0
	case controller.PromptTargetProcessID, controller.PromptTargetUserID:
		return true
	default:
		return false
	}
}

func bestAvailableTarget(conn state.Connection) controller.PromptTarget {
	switch {
	case conn.ProcessPath != "":
		return controller.PromptTargetProcessPath
	case len(conn.ProcessArgs) > 0:
		return controller.PromptTargetProcessCmd
	case conn.DstHost != "":
		return controller.PromptTargetDestinationHost
	case conn.DstIP != "":
		return controller.PromptTargetDestinationIP
	case conn.DstPort != 0:
		return controller.PromptTargetDestinationPort
	default:
		return controller.PromptTargetProcessID
	}
}

func peerKey(ctx context.Context) string {
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		return fmt.Sprintf("%s://%s", p.Addr.Network(), p.Addr.String())
	}
	return "unknown"
}

func peerAddress(ctx context.Context) string {
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		return p.Addr.String()
	}
	return "unknown"
}
