package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
)

const taskReplyBufferSize = 8

var (
	ErrInvalidTaskRequest    = errors.New("invalid task request")
	ErrInvalidTaskStream     = errors.New("invalid task stream")
	ErrTaskReplyBackpressure = errors.New("task reply buffer full")
)

type taskStream struct {
	server  *Server
	session *session
	pending *pendingNotification
	id      uint64
	nodeID  string
	name    controller.TaskName
	payload string
	updates chan controller.TaskUpdate
	done    chan struct{}

	mu          sync.Mutex
	err         error
	stopErr     error
	closed      bool
	terminating bool
	stopCancel  func() bool
	stopDone    chan struct{}
}

var _ controller.TaskManager = (*Server)(nil)
var _ controller.TaskStream = (*taskStream)(nil)

// StartTask starts a bounded stream for one daemon background task.
func (s *Server) StartTask(
	ctx context.Context,
	nodeID string,
	request controller.TaskRequest,
) (controller.TaskStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if nodeID == "" || nodeID == "unknown" {
		return nil, ErrUnknownNotificationNode
	}
	if !validTaskName(request.Name) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidTaskRequest, request.Name)
	}

	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidTaskRequest, err)
	}

	s.sessionsMu.RLock()
	sess, ok := s.sessions[nodeID]
	if !ok {
		s.sessionsMu.RUnlock()
		return nil, fmt.Errorf("%w: %s", ErrNotificationNodeDisconnected, nodeID)
	}

	task := &taskStream{
		server:   s,
		session:  sess,
		id:       s.nextNotificationID(),
		nodeID:   nodeID,
		name:     request.Name,
		payload:  string(payload),
		updates:  make(chan controller.TaskUpdate, taskReplyBufferSize),
		done:     make(chan struct{}),
		stopDone: make(chan struct{}),
	}
	task.pending = &pendingNotification{
		deliver: task.deliver,
		close: func(error) {
			_ = task.terminate(sess.closedError(), false)
		},
		migrate: task.migrateNodeID,
	}
	if err := sess.addPendingNotification(task.id, task.pending); err != nil {
		s.sessionsMu.RUnlock()
		return nil, err
	}

	if err := sess.enqueue(&pb.Notification{
		Id:         task.id,
		ClientName: nodeID,
		ServerName: s.opts.ServerName,
		Type:       pb.Action_TASK_START,
		Data:       task.payload,
	}); err != nil {
		sess.removePendingNotification(task.id, task.pending)
		s.sessionsMu.RUnlock()
		return nil, err
	}
	s.sessionsMu.RUnlock()
	task.setCancelWatch(context.AfterFunc(ctx, func() {
		_ = task.terminate(ctx.Err(), true)
	}))
	return task, nil
}

// StopTask stops a daemon task and closes its original update stream.
func (s *Server) StopTask(ctx context.Context, stream controller.TaskStream) error {
	task, ok := stream.(*taskStream)
	if !ok || task == nil || task.server != s {
		return ErrInvalidTaskStream
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// OpenSnitch v1.8 does not send a reply for TASK_STOP.
	return task.terminate(nil, true)
}

func validTaskName(name controller.TaskName) bool {
	switch name {
	case controller.TaskPIDMonitor, controller.TaskNodeMonitor, controller.TaskSocketsMonitor:
		return true
	default:
		return false
	}
}

func (t *taskStream) ID() uint64 {
	return t.id
}

func (t *taskStream) NodeID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.nodeID
}

func (t *taskStream) migrateNodeID(nodeID string) {
	t.mu.Lock()
	t.nodeID = nodeID
	t.mu.Unlock()
}

func (t *taskStream) Name() controller.TaskName {
	return t.name
}

func (t *taskStream) Updates() <-chan controller.TaskUpdate {
	return t.updates
}

func (t *taskStream) Done() <-chan struct{} {
	return t.done
}

func (t *taskStream) Err() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.err
}

func (t *taskStream) deliver(reply *pb.NotificationReply) bool {
	t.mu.Lock()
	if t.closed || t.terminating {
		t.mu.Unlock()
		return false
	}
	if reply.GetCode() == pb.NotificationReplyCode_ERROR {
		err := &NotificationReplyError{NodeID: t.nodeID, Reply: reply}
		t.mu.Unlock()
		_ = t.terminate(err, true)
		return false
	}

	update := controller.TaskUpdate{Data: append(json.RawMessage(nil), reply.GetData()...)}
	select {
	case t.updates <- update:
		t.mu.Unlock()
		return true
	default:
		err := fmt.Errorf("%w for %s", ErrTaskReplyBackpressure, t.name)
		t.mu.Unlock()
		_ = t.terminate(err, true)
		return false
	}
}

func (t *taskStream) unregister() {
	t.session.removePendingNotification(t.id, t.pending)
}

func (t *taskStream) terminate(err error, emitStop bool) error {
	t.mu.Lock()
	if t.closed || t.terminating {
		stopDone := t.stopDone
		t.mu.Unlock()
		<-stopDone
		<-t.done
		t.mu.Lock()
		stopErr := t.stopErr
		t.mu.Unlock()
		return stopErr
	}
	t.terminating = true
	if !emitStop {
		t.stopErr = err
		close(t.stopDone)
	}
	stopCancel := t.stopCancel
	t.stopCancel = nil
	t.mu.Unlock()

	t.unregister()
	if emitStop {
		stopErr := t.enqueueStop()
		t.mu.Lock()
		t.stopErr = stopErr
		close(t.stopDone)
		t.mu.Unlock()
		if err != nil && stopErr != nil {
			err = errors.Join(err, fmt.Errorf("stop task: %w", stopErr))
		} else if err == nil {
			err = stopErr
		}
	}

	t.mu.Lock()
	t.err = err
	t.closed = true
	close(t.updates)
	close(t.done)
	t.mu.Unlock()
	if stopCancel != nil {
		stopCancel()
	}
	t.mu.Lock()
	stopErr := t.stopErr
	t.mu.Unlock()
	return stopErr
}

func (t *taskStream) enqueueStop() error {
	t.server.sessionsMu.RLock()
	defer t.server.sessionsMu.RUnlock()
	nodeID := t.session.currentNodeID()
	if t.server.sessionsClosed {
		return fmt.Errorf("%w for %s", ErrNotificationSessionClosed, nodeID)
	}
	if t.server.sessions[nodeID] != t.session {
		return fmt.Errorf("%w: %s", ErrNotificationNodeDisconnected, nodeID)
	}
	notification := t.server.newNotification(pb.Action_TASK_STOP, nodeID)
	notification.Data = t.payload
	return t.session.enqueue(notification)
}

func (t *taskStream) setCancelWatch(stop func() bool) {
	t.mu.Lock()
	if t.closed || t.terminating {
		t.mu.Unlock()
		stop()
		return
	}
	t.stopCancel = stop
	t.mu.Unlock()
}
