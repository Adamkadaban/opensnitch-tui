package daemon

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"google.golang.org/grpc/peer"
)

var (
	ErrTransportIdentityConflict = errors.New("transport already registered to another node")
	ErrSupersededNodeTransport   = errors.New("node transport superseded by a newer connection")
)

const unnamedDaemon = "unnamed"

func transportIdentity(ctx context.Context) string {
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		return transportPeerIdentity(p.Addr)
	}
	return "unknown"
}

func transportPeerIdentity(addr net.Addr) string {
	network := normalizedNetwork(addr.Network())
	return fmt.Sprintf("%s://%s", network, addr.String())
}

func stableNodeIdentity(ctx context.Context, daemonName string) string {
	name := normalizedDaemonName(daemonName)
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		network := normalizedNetwork(p.Addr.Network())
		host := stablePeerHost(p.Addr)
		return fmt.Sprintf("%s://%s/name/%s", network, host, encodedDaemonName(name))
	}
	return fmt.Sprintf("unknown://local/name/%s", encodedDaemonName(name))
}

func normalizedNetwork(network string) string {
	switch network {
	case "tcp4", "tcp6":
		return "tcp"
	case "unixpacket":
		return "unix"
	default:
		return network
	}
}

func stablePeerHost(addr net.Addr) string {
	switch normalizedNetwork(addr.Network()) {
	case "tcp":
		host, _, err := net.SplitHostPort(addr.String())
		if err == nil {
			if strings.Contains(host, ":") {
				return "[" + host + "]"
			}
			return host
		}
	case "unix":
		return "local"
	}
	return addr.String()
}

func normalizedDaemonName(name string) string {
	name = strings.TrimSpace(strings.ToValidUTF8(name, "�"))
	if name == "" {
		return unnamedDaemon
	}
	return name
}

func encodedDaemonName(name string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(name))
}

func (s *Server) resolveTransportLocked(transportID string) (string, error) {
	if nodeID, superseded := s.supersededTransports[transportID]; superseded {
		return "", fmt.Errorf("%w: %s now owns %s", ErrSupersededNodeTransport, s.nodeTransports[nodeID], nodeID)
	}
	if nodeID, ok := s.transportAliases[transportID]; ok {
		return nodeID, nil
	}
	return transportID, nil
}

func (s *Server) registerNodeAlias(ctx context.Context, daemonName string) (string, error) {
	transportID := transportIdentity(ctx)
	stableID := stableNodeIdentity(ctx, daemonName)
	var replaced *session

	s.identityMu.Lock()
	s.sessionsMu.Lock()
	if existing, ok := s.transportAliases[transportID]; ok && existing != stableID {
		s.sessionsMu.Unlock()
		s.identityMu.Unlock()
		return "", fmt.Errorf("%w: %s is already %s, cannot become %s", ErrTransportIdentityConflict, transportID, existing, stableID)
	}
	if _, superseded := s.supersededTransports[transportID]; superseded {
		s.sessionsMu.Unlock()
		s.identityMu.Unlock()
		return "", fmt.Errorf("%w: %s", ErrSupersededNodeTransport, transportID)
	}

	if previousTransport := s.nodeTransports[stableID]; previousTransport != "" && previousTransport != transportID {
		s.supersededTransports[previousTransport] = stableID
		delete(s.transportAliases, previousTransport)
	}
	s.transportAliases[transportID] = stableID
	s.nodeTransports[stableID] = transportID

	provisional := s.sessions[transportID]
	current := s.sessions[stableID]
	if provisional != nil {
		delete(s.sessions, transportID)
		provisional.migrateNodeID(stableID)
		s.sessions[stableID] = provisional
		if current != nil && current != provisional {
			replaced = current
		}
	} else if current != nil && current.transportID != transportID {
		delete(s.sessions, stableID)
		replaced = current
	}
	s.sessionsMu.Unlock()

	s.migratePromptsAndState(transportID, stableID, normalizedDaemonName(daemonName))
	s.identityMu.Unlock()

	if replaced != nil {
		replaced.close(ErrSupersededNodeTransport)
	}
	return stableID, nil
}

func (s *Server) registerTransportSession(transportID string) (*session, error) {
	if transportID == "" || transportID == "unknown" {
		return nil, ErrUnknownNotificationNode
	}

	var replaced *session
	s.identityMu.RLock()
	s.sessionsMu.Lock()
	if s.sessionsClosed {
		s.sessionsMu.Unlock()
		s.identityMu.RUnlock()
		return nil, ErrNotificationSessionClosed
	}
	if _, superseded := s.supersededTransports[transportID]; superseded {
		s.sessionsMu.Unlock()
		s.identityMu.RUnlock()
		return nil, fmt.Errorf("%w: %s", ErrSupersededNodeTransport, transportID)
	}
	nodeID := transportID
	if stableID, ok := s.transportAliases[transportID]; ok {
		nodeID = stableID
	}
	sess := newNotificationSession(nodeID, transportID)
	if current := s.sessions[nodeID]; current != nil {
		owner := s.nodeTransports[nodeID]
		if owner == transportID && current.transportID != transportID {
			replaced = current
			s.sessions[nodeID] = sess
		} else {
			s.sessionsMu.Unlock()
			s.identityMu.RUnlock()
			return nil, fmt.Errorf("%w: %s", ErrDuplicateNotificationSession, nodeID)
		}
	} else {
		s.sessions[nodeID] = sess
	}
	s.sessionsMu.Unlock()
	s.identityMu.RUnlock()

	if replaced != nil {
		replaced.close(ErrSupersededNodeTransport)
	}
	return sess, nil
}

func (s *Server) migratePromptsAndState(fromID, toID, nodeName string) {
	if fromID == toID {
		return
	}
	s.promptsMu.Lock()
	defer s.promptsMu.Unlock()
	for _, req := range s.prompts {
		req.mu.Lock()
		if req.prompt.NodeID == fromID {
			req.prompt.NodeID = toID
			req.prompt.NodeName = nodeName
		}
		req.mu.Unlock()
	}
	s.store.MigrateNodeIdentity(fromID, toID, nodeName)
}

func (s *Server) updateSessionNodeStatus(sess *session, status state.NodeStatus, message string) {
	s.sessionsMu.RLock()
	nodeID := ""
	for id, current := range s.sessions {
		if current == sess {
			nodeID = id
			break
		}
	}
	s.sessionsMu.RUnlock()
	if nodeID != "" {
		s.store.UpdateNodeStatus(nodeID, status, message, time.Now())
	}
}
