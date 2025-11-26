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
	return &Server{store: store, opts: opts}
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
	for {
		_, err := stream.Recv()
		if err == io.EOF {
			s.store.UpdateNodeStatus(nodeID, state.NodeStatusDisconnected, "notifications closed", time.Now())
			return nil
		}
		if err != nil {
			s.store.UpdateNodeStatus(nodeID, state.NodeStatusError, err.Error(), time.Now())
			return err
		}
	}
}

// PostAlert records alert text for the UI.
func (s *Server) PostAlert(ctx context.Context, alert *pb.Alert) (*pb.MsgResponse, error) {
	if alert != nil {
		s.store.SetError(alert.GetText())
	}
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
