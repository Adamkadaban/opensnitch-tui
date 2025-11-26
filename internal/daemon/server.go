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
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/peer"

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
}

type session struct {
	nodeID string
	send   chan *pb.Notification
}

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
	return &Server{store: store, opts: opts, sessions: make(map[string]*session)}
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

// AskRule currently auto-allows the connection until interactive prompts are implemented.
func (s *Server) AskRule(ctx context.Context, conn *pb.Connection) (*pb.Rule, error) {
	name := fmt.Sprintf("auto-%d", time.Now().UnixNano())
	s.store.SetError(fmt.Sprintf("auto-allow %s:%d (%s)", conn.GetDstHost(), conn.GetDstPort(), conn.GetProtocol()))
	return &pb.Rule{
		Created:  time.Now().Unix(),
		Name:     name,
		Enabled:  true,
		Action:   "allow",
		Duration: "once",
	}, nil
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
