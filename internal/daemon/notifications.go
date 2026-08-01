package daemon

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
	"google.golang.org/protobuf/proto"
)

const (
	notificationQueueSize    = 8
	notificationPendingLimit = 8
	notificationIDFloor      = 10_000
)

var (
	ErrUnknownNotificationNode      = errors.New("unknown notification node")
	ErrNotificationNodeDisconnected = errors.New("notification node disconnected")
	ErrDuplicateNotificationSession = errors.New("notification session already connected")
	ErrDuplicateNotificationID      = errors.New("duplicate notification id")
	ErrNotificationQueueFull        = errors.New("notification queue full")
	ErrNotificationBackpressure     = errors.New("too many pending notifications")
	ErrNotificationSessionClosed    = errors.New("notification session closed")
	ErrNilNotification              = errors.New("notification is nil")
)

// NotificationReplyError reports a daemon-side notification failure.
type NotificationReplyError struct {
	NodeID string
	Reply  *pb.NotificationReply
}

func (e *NotificationReplyError) Error() string {
	if e.Reply.GetData() != "" {
		return fmt.Sprintf("notification %d failed for %s: %s", e.Reply.GetId(), e.NodeID, e.Reply.GetData())
	}
	return fmt.Sprintf("notification %d failed for %s", e.Reply.GetId(), e.NodeID)
}

type notificationReceive struct {
	reply *pb.NotificationReply
	err   error
}

func newNotificationSession(nodeID string, transportIDs ...string) *session {
	transportID := nodeID
	if len(transportIDs) > 0 {
		transportID = transportIDs[0]
	}
	return &session{
		transportID: transportID,
		nodeID:      nodeID,
		send:        make(chan *pb.Notification, notificationQueueSize),
		done:        make(chan struct{}),
		pending:     make(map[uint64]*pendingNotification),
	}
}

func (s *Server) registerSession(nodeID string) (*session, error) {
	if nodeID == "" || nodeID == "unknown" {
		return nil, ErrUnknownNotificationNode
	}
	sess := newNotificationSession(nodeID)
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	if s.sessionsClosed {
		return nil, ErrNotificationSessionClosed
	}
	if _, ok := s.sessions[nodeID]; ok {
		return nil, fmt.Errorf("%w: %s", ErrDuplicateNotificationSession, nodeID)
	}
	s.sessions[nodeID] = sess
	return sess, nil
}

type pendingNotification struct {
	replyCh chan *pb.NotificationReply
	deliver func(*pb.NotificationReply) bool
	close   func(error)
	migrate func(string)
}

// SendNotification sends a notification to one connected node and waits for its reply.
func (s *Server) SendNotification(
	ctx context.Context,
	nodeID string,
	notification *pb.Notification,
) (*pb.NotificationReply, error) {
	if notification == nil {
		return nil, ErrNilNotification
	}
	if nodeID == "" || nodeID == "unknown" {
		return nil, ErrUnknownNotificationNode
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.sessionsMu.RLock()
	sess, ok := s.sessions[nodeID]
	s.sessionsMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotificationNodeDisconnected, nodeID)
	}

	outbound := proto.Clone(notification).(*pb.Notification)
	outbound.Id = s.nextNotificationID()
	outbound.ClientName = nodeID
	outbound.ServerName = s.opts.ServerName

	replyCh := make(chan *pb.NotificationReply, 1)
	if err := sess.addPending(outbound.Id, replyCh); err != nil {
		return nil, err
	}
	if err := sess.enqueue(outbound); err != nil {
		sess.removePending(outbound.Id, replyCh)
		return nil, err
	}

	select {
	case reply := <-replyCh:
		if reply.GetCode() == pb.NotificationReplyCode_ERROR {
			return reply, &NotificationReplyError{NodeID: nodeID, Reply: reply}
		}
		return reply, nil
	case <-ctx.Done():
		sess.removePending(outbound.Id, replyCh)
		return nil, ctx.Err()
	case <-sess.done:
		sess.removePending(outbound.Id, replyCh)
		return nil, sess.closedError()
	}
}

func (s *Server) nextNotificationID() uint64 {
	for {
		if id := atomic.AddUint64(&s.notifySeqID, 1); id != 0 {
			return id
		}
	}
}

func (s *Server) unregisterSession(sess *session, cause error) {
	s.sessionsMu.Lock()
	for nodeID, current := range s.sessions {
		if current == sess {
			delete(s.sessions, nodeID)
			break
		}
	}
	s.sessionsMu.Unlock()
	sess.close(cause)
}

func (s *Server) closeSessions(cause error) {
	s.sessionsMu.Lock()
	s.sessionsClosed = true
	sessions := make([]*session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.sessionsMu.Unlock()

	for _, sess := range sessions {
		sess.close(cause)
	}
}

func (sess *session) addPending(id uint64, replyCh chan *pb.NotificationReply) error {
	return sess.addPendingNotification(id, &pendingNotification{replyCh: replyCh})
}

func (sess *session) migrateNodeID(nodeID string) {
	sess.mu.Lock()
	sess.nodeID = nodeID
	callbacks := make([]func(string), 0, len(sess.pending))
	for _, pending := range sess.pending {
		if pending.migrate != nil {
			callbacks = append(callbacks, pending.migrate)
		}
	}
	sess.mu.Unlock()
	for _, migrate := range callbacks {
		migrate(nodeID)
	}
}

func (sess *session) currentNodeID() string {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.nodeID
}

func (sess *session) addPendingNotification(id uint64, pending *pendingNotification) error {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.closeErr != nil {
		return sess.closedErrorLocked()
	}
	if _, ok := sess.pending[id]; ok {
		return fmt.Errorf("%w: %d", ErrDuplicateNotificationID, id)
	}
	if len(sess.pending) >= notificationPendingLimit {
		return fmt.Errorf("%w for %s", ErrNotificationBackpressure, sess.nodeID)
	}
	sess.pending[id] = pending
	return nil
}

func (sess *session) removePending(id uint64, replyCh chan *pb.NotificationReply) {
	sess.mu.Lock()
	pending, ok := sess.pending[id]
	if ok && pending.replyCh == replyCh {
		delete(sess.pending, id)
	}
	sess.mu.Unlock()
}

func (sess *session) removePendingNotification(id uint64, pending *pendingNotification) {
	sess.mu.Lock()
	if current, ok := sess.pending[id]; ok && current == pending {
		delete(sess.pending, id)
	}
	sess.mu.Unlock()
}

func (sess *session) complete(reply *pb.NotificationReply) bool {
	sess.mu.Lock()
	pending, ok := sess.pending[reply.GetId()]
	if ok && pending.replyCh != nil {
		delete(sess.pending, reply.GetId())
	}
	sess.mu.Unlock()
	if !ok {
		return false
	}
	if pending.replyCh != nil {
		pending.replyCh <- reply
		return true
	}
	if pending.deliver != nil && !pending.deliver(reply) {
		sess.removePendingNotification(reply.GetId(), pending)
	}
	return true
}

func (sess *session) enqueue(notification *pb.Notification) error {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.closeErr != nil {
		return sess.closedErrorLocked()
	}
	select {
	case sess.send <- notification:
		return nil
	default:
		return fmt.Errorf("%w for %s", ErrNotificationQueueFull, sess.nodeID)
	}
}

func (sess *session) close(cause error) {
	sess.closeOnce.Do(func() {
		if cause == nil {
			cause = ErrNotificationSessionClosed
		}
		sess.mu.Lock()
		sess.closeErr = cause
		pending := make([]*pendingNotification, 0, len(sess.pending))
		for _, request := range sess.pending {
			pending = append(pending, request)
		}
		clear(sess.pending)
		sess.mu.Unlock()
		close(sess.done)
		for _, request := range pending {
			if request.close != nil {
				request.close(cause)
			}
		}
	})
}

func (sess *session) closedError() error {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.closedErrorLocked()
}

func (sess *session) closedErrorLocked() error {
	if sess.closeErr == nil || errors.Is(sess.closeErr, ErrNotificationSessionClosed) {
		return fmt.Errorf("%w for %s", ErrNotificationSessionClosed, sess.nodeID)
	}
	return fmt.Errorf("%w for %s: %v", ErrNotificationSessionClosed, sess.nodeID, sess.closeErr)
}

func receiveNotificationReplies(
	ctx context.Context,
	stream pb.UI_NotificationsServer,
	replies chan<- notificationReceive,
) {
	for {
		reply, err := stream.Recv()
		select {
		case replies <- notificationReceive{reply: reply, err: err}:
		case <-ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
}
