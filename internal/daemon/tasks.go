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

	mu         sync.Mutex
	err        error
	closed     bool
	stopCancel func() bool
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
	s.sessionsMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotificationNodeDisconnected, nodeID)
	}

	task := &taskStream{
		server:  s,
		session: sess,
		id:      s.nextNotificationID(),
		nodeID:  nodeID,
		name:    request.Name,
		payload: string(payload),
		updates: make(chan controller.TaskUpdate, taskReplyBufferSize),
		done:    make(chan struct{}),
	}
	task.pending = &pendingNotification{
		deliver: task.deliver,
		close: func(error) {
			task.finish(sess.closedError())
		},
	}
	if err := sess.addPendingNotification(task.id, task.pending); err != nil {
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
		return nil, err
	}

	task.setCancelWatch(context.AfterFunc(ctx, func() {
		task.unregister()
		task.finish(ctx.Err())
	}))
	return task, nil
}

// StopTask stops a daemon task and closes its original update stream.
func (s *Server) StopTask(ctx context.Context, stream controller.TaskStream) error {
	task, ok := stream.(*taskStream)
	if !ok || task == nil || task.server != s {
		return ErrInvalidTaskStream
	}

	task.unregister()
	_, err := s.SendNotification(ctx, task.nodeID, &pb.Notification{
		Type: pb.Action_TASK_STOP,
		Data: task.payload,
	})
	task.finish(err)
	return err
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
	return t.nodeID
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
	var stopCancel func() bool

	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return false
	}
	if reply.GetCode() == pb.NotificationReplyCode_ERROR {
		t.unregister()
		stopCancel = t.finishLocked(&NotificationReplyError{NodeID: t.nodeID, Reply: reply})
		t.mu.Unlock()
		if stopCancel != nil {
			stopCancel()
		}
		return false
	}

	update := controller.TaskUpdate{Data: append(json.RawMessage(nil), reply.GetData()...)}
	select {
	case t.updates <- update:
		t.mu.Unlock()
		return true
	default:
		t.unregister()
		stopCancel = t.finishLocked(fmt.Errorf("%w for %s", ErrTaskReplyBackpressure, t.name))
		t.mu.Unlock()
		if stopCancel != nil {
			stopCancel()
		}
		return false
	}
}

func (t *taskStream) unregister() {
	t.session.removePendingNotification(t.id, t.pending)
}

func (t *taskStream) finish(err error) {
	t.mu.Lock()
	stopCancel := t.finishLocked(err)
	t.mu.Unlock()
	if stopCancel != nil {
		stopCancel()
	}
}

func (t *taskStream) finishLocked(err error) func() bool {
	if t.closed {
		return nil
	}
	t.closed = true
	t.err = err
	close(t.updates)
	close(t.done)
	stopCancel := t.stopCancel
	t.stopCancel = nil
	return stopCancel
}

func (t *taskStream) setCancelWatch(stop func() bool) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		stop()
		return
	}
	t.stopCancel = stop
	t.mu.Unlock()
}
