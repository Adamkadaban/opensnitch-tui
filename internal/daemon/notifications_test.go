package daemon

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

func TestSendNotificationReturnsMatchingReply(t *testing.T) {
	srv, stream, stop := startNotificationTestStream(t, "node-1")
	defer stop()

	original := &pb.Notification{Id: 99, Type: pb.Action_CHANGE_CONFIG, Data: `{"log_level": 2}`}
	result := make(chan notificationResult, 1)
	go func() {
		reply, err := srv.SendNotification(context.Background(), stream.nodeID, original)
		result <- notificationResult{reply: reply, err: err}
	}()

	sent := <-stream.sent
	if sent.GetId() == 0 || sent.GetId() == original.GetId() {
		t.Fatalf("expected a newly assigned notification id, got %d", sent.GetId())
	}

	if sent.GetClientName() != stream.nodeID {
		t.Fatalf("expected client name %q, got %q", stream.nodeID, sent.GetClientName())
	}
	if sent.GetServerName() != "opensnitch-tui" {
		t.Fatalf("expected server name opensnitch-tui, got %q", sent.GetServerName())
	}
	if original.GetId() != 99 {
		t.Fatalf("expected caller notification to remain unchanged, got id %d", original.GetId())
	}

	stream.replies <- &pb.NotificationReply{
		Id:   sent.GetId() + 1,
		Code: pb.NotificationReplyCode_OK,
		Data: "unrelated",
	}
	stream.replies <- &pb.NotificationReply{
		Id:   sent.GetId(),
		Code: pb.NotificationReplyCode_OK,
		Data: "applied",
	}
	got := <-result
	if got.err != nil {
		t.Fatalf("SendNotification returned error: %v", got.err)
	}
	if got.reply.GetData() != "applied" {
		t.Fatalf("expected matching reply, got %+v", got.reply)
	}
}

func TestNotificationIDsExceedTaskStreamingFloor(t *testing.T) {
	srv := New(state.NewStore(), Options{})
	if id := srv.nextNotificationID(); id <= notificationIDFloor {
		t.Fatalf("notification id %d must exceed streaming floor %d", id, notificationIDFloor)
	}
	if srv.opts.ListenAddr != "unix:///tmp/osui.sock" {
		t.Fatalf("unexpected default listen address %q", srv.opts.ListenAddr)
	}
}

func TestOneShotNotificationUnregistersAfterReply(t *testing.T) {
	srv, stream, stop := startNotificationTestStream(t, "node-1")
	defer stop()

	result := make(chan notificationResult, 1)
	go func() {
		reply, err := srv.SendNotification(
			context.Background(),
			stream.nodeID,
			&pb.Notification{Type: pb.Action_CHANGE_CONFIG},
		)
		result <- notificationResult{reply: reply, err: err}
	}()

	sent := <-stream.sent
	stream.replies <- &pb.NotificationReply{Id: sent.GetId(), Data: "first"}
	got := <-result
	if got.err != nil || got.reply.GetData() != "first" {
		t.Fatalf("expected first reply, got reply=%v err=%v", got.reply, got.err)
	}

	srv.sessionsMu.RLock()
	sess := srv.sessions[stream.nodeID]
	srv.sessionsMu.RUnlock()
	sess.mu.Lock()
	pending := len(sess.pending)
	sess.mu.Unlock()
	if pending != 0 {
		t.Fatalf("expected one-shot pending cleanup, got %d entries", pending)
	}
}

func TestSendNotificationReturnsReplyError(t *testing.T) {
	srv, stream, stop := startNotificationTestStream(t, "node-1")
	defer stop()

	result := make(chan notificationResult, 1)
	go func() {
		reply, err := srv.SendNotification(
			context.Background(),
			stream.nodeID,
			&pb.Notification{Type: pb.Action_CHANGE_CONFIG},
		)
		result <- notificationResult{reply: reply, err: err}
	}()

	sent := <-stream.sent
	stream.replies <- &pb.NotificationReply{
		Id:   sent.GetId(),
		Code: pb.NotificationReplyCode_ERROR,
		Data: "invalid configuration",
	}
	got := <-result
	var replyErr *NotificationReplyError
	if !errors.As(got.err, &replyErr) {
		t.Fatalf("expected NotificationReplyError, got %v", got.err)
	}
	if got.reply.GetData() != "invalid configuration" {
		t.Fatalf("expected error reply to be returned, got %+v", got.reply)
	}
}

func TestSendNotificationRespectsCallerCancellation(t *testing.T) {
	srv, stream, stop := startNotificationTestStream(t, "node-1")
	defer stop()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := srv.SendNotification(ctx, stream.nodeID, &pb.Notification{Type: pb.Action_CHANGE_CONFIG})
		result <- err
	}()

	<-stream.sent
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestSendNotificationRejectsDisconnectedNode(t *testing.T) {
	srv := New(state.NewStore(), Options{})
	_, err := srv.SendNotification(
		context.Background(),
		"node-1",
		&pb.Notification{Type: pb.Action_CHANGE_CONFIG},
	)
	if !errors.Is(err, ErrNotificationNodeDisconnected) {
		t.Fatalf("expected disconnected node error, got %v", err)
	}
}

func TestSendNotificationRejectsUnknownNode(t *testing.T) {
	srv := New(state.NewStore(), Options{})
	_, err := srv.SendNotification(
		context.Background(),
		"",
		&pb.Notification{Type: pb.Action_CHANGE_CONFIG},
	)
	if !errors.Is(err, ErrUnknownNotificationNode) {
		t.Fatalf("expected unknown node error, got %v", err)
	}
}

func TestNotificationStreamCleanupFailsPendingSend(t *testing.T) {
	srv, stream, stop := startNotificationTestStream(t, "node-1")
	defer stop()

	result := make(chan error, 1)
	go func() {
		_, err := srv.SendNotification(
			context.Background(),
			stream.nodeID,
			&pb.Notification{Type: pb.Action_CHANGE_CONFIG},
		)
		result <- err
	}()

	<-stream.sent
	stream.cancel()
	if err := <-result; !errors.Is(err, ErrNotificationSessionClosed) {
		t.Fatalf("expected session closed error, got %v", err)
	}
	if err := <-stream.done; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected stream cancellation, got %v", err)
	}

	srv.sessionsMu.RLock()
	_, connected := srv.sessions[stream.nodeID]
	srv.sessionsMu.RUnlock()
	if connected {
		t.Fatal("expected disconnected session to be removed")
	}
}

func TestSendNotificationRejectsFullQueue(t *testing.T) {
	srv := New(state.NewStore(), Options{})
	sess := newNotificationSession("node-1")
	srv.sessions["node-1"] = sess
	for range notificationQueueSize {
		sess.send <- &pb.Notification{}
	}

	_, err := srv.SendNotification(
		context.Background(),
		"node-1",
		&pb.Notification{Type: pb.Action_CHANGE_CONFIG},
	)
	if !errors.Is(err, ErrNotificationQueueFull) {
		t.Fatalf("expected queue full error, got %v", err)
	}

	sess.mu.Lock()
	pending := len(sess.pending)
	sess.mu.Unlock()
	if pending != 0 {
		t.Fatalf("expected pending request cleanup, got %d requests", pending)
	}
}

func TestSendNotificationBoundsPendingRequests(t *testing.T) {
	srv := New(state.NewStore(), Options{})
	sess := newNotificationSession("node-1")
	srv.sessions["node-1"] = sess
	for id := uint64(100); id < 100+notificationPendingLimit; id++ {
		if err := sess.addPending(id, make(chan *pb.NotificationReply, 1)); err != nil {
			t.Fatalf("add pending notification %d: %v", id, err)
		}
	}

	_, err := srv.SendNotification(
		context.Background(),
		"node-1",
		&pb.Notification{Type: pb.Action_CHANGE_CONFIG},
	)
	if !errors.Is(err, ErrNotificationBackpressure) {
		t.Fatalf("expected pending notification backpressure, got %v", err)
	}
}

func TestSessionRejectsDuplicateNotificationID(t *testing.T) {
	sess := newNotificationSession("node-1")
	if err := sess.addPending(1, make(chan *pb.NotificationReply, 1)); err != nil {
		t.Fatalf("add first notification: %v", err)
	}
	if err := sess.addPending(1, make(chan *pb.NotificationReply, 1)); !errors.Is(err, ErrDuplicateNotificationID) {
		t.Fatalf("expected duplicate notification id error, got %v", err)
	}
}

func TestRegisterSessionRejectsDuplicateNode(t *testing.T) {
	srv := New(state.NewStore(), Options{})
	if _, err := srv.registerSession("node-1"); err != nil {
		t.Fatalf("register first session: %v", err)
	}
	if _, err := srv.registerSession("node-1"); !errors.Is(err, ErrDuplicateNotificationSession) {
		t.Fatalf("expected duplicate session error, got %v", err)
	}
}

type notificationResult struct {
	reply *pb.NotificationReply
	err   error
}

type notificationTestStream struct {
	ctx         context.Context
	cancel      context.CancelFunc
	replies     chan *pb.NotificationReply
	sent        chan *pb.Notification
	recvStarted chan struct{}
	recvOnce    sync.Once
	done        chan error
	finished    chan struct{}
	nodeID      string
}

func startNotificationTestStream(
	t *testing.T,
	nodeID string,
) (*Server, *notificationTestStream, func()) {
	t.Helper()

	store := state.NewStore()
	srv := New(store, Options{})
	baseCtx, cancel := context.WithCancel(context.Background())
	ctx := peer.NewContext(baseCtx, &peer.Peer{
		Addr: &testAddr{network: "test", value: nodeID},
	})
	stream := &notificationTestStream{
		ctx:         ctx,
		cancel:      cancel,
		replies:     make(chan *pb.NotificationReply),
		sent:        make(chan *pb.Notification, notificationQueueSize),
		recvStarted: make(chan struct{}),
		done:        make(chan error, 1),
		finished:    make(chan struct{}),
		nodeID:      transportIdentity(ctx),
	}
	go func() {
		stream.done <- srv.Notifications(stream)
		close(stream.finished)
	}()
	<-stream.recvStarted

	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			cancel()
			<-stream.finished
		})
	}
	return srv, stream, stop
}

func (s *notificationTestStream) Send(notification *pb.Notification) error {
	select {
	case s.sent <- notification:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

func (s *notificationTestStream) Recv() (*pb.NotificationReply, error) {
	s.recvOnce.Do(func() {
		close(s.recvStarted)
	})
	select {
	case reply, ok := <-s.replies:
		if !ok {
			return nil, io.EOF
		}
		return reply, nil
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

func (s *notificationTestStream) SetHeader(metadata.MD) error  { return nil }
func (s *notificationTestStream) SendHeader(metadata.MD) error { return nil }
func (s *notificationTestStream) SetTrailer(metadata.MD)       {}
func (s *notificationTestStream) Context() context.Context     { return s.ctx }
func (s *notificationTestStream) SendMsg(any) error            { return nil }
func (s *notificationTestStream) RecvMsg(any) error            { return nil }
