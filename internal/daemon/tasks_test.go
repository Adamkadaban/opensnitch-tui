package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
)

func TestTaskStreamReceivesMultipleRepliesWithStartID(t *testing.T) {
	srv, transport, stop := startNotificationTestStream(t, "node-1")
	defer stop()

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := srv.StartTask(ctx, transport.nodeID, controller.NewNodeMonitorTask("node-1", "5s"))
	if err != nil {
		t.Fatalf("StartTask returned error: %v", err)
	}
	start := <-transport.sent

	transport.replies <- &pb.NotificationReply{Id: start.GetId(), Data: `{"load":1}`}
	transport.replies <- &pb.NotificationReply{Id: start.GetId(), Data: `{"load":2}`}

	for index, want := range []string{`{"load":1}`, `{"load":2}`} {
		select {
		case update := <-stream.Updates():
			if string(update.Data) != want {
				t.Fatalf("update %d: expected %s, got %s", index, want, update.Data)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for update %d", index)
		}
	}

	if got := pendingNotificationCount(stream); got != 1 {
		t.Fatalf("expected task stream to remain pending, got %d entries", got)
	}
	cancel()
	waitForTaskDone(t, stream)
	if !errors.Is(stream.Err(), context.Canceled) {
		t.Fatalf("expected cancellation error, got %v", stream.Err())
	}
}

func TestStopTaskAwaitsAcknowledgementAndCleansStartStream(t *testing.T) {
	srv, transport, stop := startNotificationTestStream(t, "node-1")
	defer stop()

	stream, err := srv.StartTask(
		context.Background(),
		transport.nodeID,
		controller.NewPIDMonitorTask(42, "5s"),
	)
	if err != nil {
		t.Fatalf("StartTask returned error: %v", err)
	}
	start := <-transport.sent

	stopped := make(chan error, 1)
	go func() {
		stopped <- srv.StopTask(context.Background(), stream)
	}()

	stopNotification := <-transport.sent
	if stopNotification.GetType() != pb.Action_TASK_STOP {
		t.Fatalf("expected TASK_STOP, got %s", stopNotification.GetType())
	}
	if stopNotification.GetData() != start.GetData() {
		t.Fatalf("expected stop payload %s, got %s", start.GetData(), stopNotification.GetData())
	}
	select {
	case err := <-stopped:
		t.Fatalf("StopTask returned before acknowledgement: %v", err)
	default:
	}

	transport.replies <- &pb.NotificationReply{
		Id:   stopNotification.GetId(),
		Code: pb.NotificationReplyCode_OK,
	}
	if err := <-stopped; err != nil {
		t.Fatalf("StopTask returned error: %v", err)
	}
	waitForTaskDone(t, stream)
	if stream.Err() != nil {
		t.Fatalf("expected clean task stop, got %v", stream.Err())
	}
	if got := pendingNotificationCount(stream); got != 0 {
		t.Fatalf("expected pending cleanup, got %d entries", got)
	}
}

func TestTaskStartErrorFailsStream(t *testing.T) {
	srv, transport, stop := startNotificationTestStream(t, "node-1")
	defer stop()

	stream, err := srv.StartTask(
		context.Background(),
		transport.nodeID,
		controller.NewPIDMonitorTask(42, "bad"),
	)
	if err != nil {
		t.Fatalf("StartTask returned error: %v", err)
	}
	start := <-transport.sent
	transport.replies <- &pb.NotificationReply{
		Id:   start.GetId(),
		Code: pb.NotificationReplyCode_ERROR,
		Data: "invalid interval",
	}

	waitForTaskDone(t, stream)
	var replyErr *NotificationReplyError
	if !errors.As(stream.Err(), &replyErr) {
		t.Fatalf("expected NotificationReplyError, got %v", stream.Err())
	}
	if replyErr.Reply.GetData() != "invalid interval" {
		t.Fatalf("expected daemon error text, got %q", replyErr.Reply.GetData())
	}
	if got := pendingNotificationCount(stream); got != 0 {
		t.Fatalf("expected start error cleanup, got %d entries", got)
	}
}

func TestTaskStreamCallerCancellationCleansPending(t *testing.T) {
	srv, transport, stop := startNotificationTestStream(t, "node-1")
	defer stop()

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := srv.StartTask(ctx, transport.nodeID, controller.NewNodeMonitorTask("node-1", "5s"))
	if err != nil {
		t.Fatalf("StartTask returned error: %v", err)
	}
	<-transport.sent

	cancel()
	waitForTaskDone(t, stream)
	if !errors.Is(stream.Err(), context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", stream.Err())
	}
	if got := pendingNotificationCount(stream); got != 0 {
		t.Fatalf("expected cancellation cleanup, got %d entries", got)
	}
}

func TestTaskStreamDisconnectFailsAndCleansPending(t *testing.T) {
	srv, transport, stop := startNotificationTestStream(t, "node-1")
	defer stop()

	stream, err := srv.StartTask(
		context.Background(),
		transport.nodeID,
		controller.NewSocketsMonitorTask("5s", 1, 6, 2),
	)
	if err != nil {
		t.Fatalf("StartTask returned error: %v", err)
	}
	<-transport.sent

	transport.cancel()
	waitForTaskDone(t, stream)
	if !errors.Is(stream.Err(), ErrNotificationSessionClosed) {
		t.Fatalf("expected session closed error, got %v", stream.Err())
	}
	if got := pendingNotificationCount(stream); got != 0 {
		t.Fatalf("expected disconnect cleanup, got %d entries", got)
	}
}

func TestTaskStreamServerShutdownFailsAndCleansPending(t *testing.T) {
	srv, transport, stop := startNotificationTestStream(t, "node-1")
	defer stop()

	stream, err := srv.StartTask(
		context.Background(),
		transport.nodeID,
		controller.NewNodeMonitorTask("node-1", "5s"),
	)
	if err != nil {
		t.Fatalf("StartTask returned error: %v", err)
	}
	<-transport.sent

	srv.closeSessions(context.Canceled)
	waitForTaskDone(t, stream)
	if !errors.Is(stream.Err(), ErrNotificationSessionClosed) {
		t.Fatalf("expected session closed error, got %v", stream.Err())
	}
	if got := pendingNotificationCount(stream); got != 0 {
		t.Fatalf("expected shutdown cleanup, got %d entries", got)
	}
}

func TestTaskReplyBackpressureFailsStream(t *testing.T) {
	srv, transport, stop := startNotificationTestStream(t, "node-1")
	defer stop()

	stream, err := srv.StartTask(
		context.Background(),
		transport.nodeID,
		controller.NewSocketsMonitorTask("1s", 1, 6, 2),
	)
	if err != nil {
		t.Fatalf("StartTask returned error: %v", err)
	}
	start := <-transport.sent

	for index := range taskReplyBufferSize + 1 {
		transport.replies <- &pb.NotificationReply{
			Id:   start.GetId(),
			Data: fmt.Sprintf(`{"sequence":%d}`, index),
		}
	}

	waitForTaskDone(t, stream)
	if !errors.Is(stream.Err(), ErrTaskReplyBackpressure) {
		t.Fatalf("expected task backpressure error, got %v", stream.Err())
	}
	if got := pendingNotificationCount(stream); got != 0 {
		t.Fatalf("expected backpressure cleanup, got %d entries", got)
	}
}

func TestTaskNotificationPayloads(t *testing.T) {
	tests := []struct {
		name    string
		request controller.TaskRequest
		want    string
	}{
		{
			name:    "pid monitor",
			request: controller.NewPIDMonitorTask(42, "5s"),
			want:    `{"name":"pid-monitor","data":{"interval":"5s","pid":"42"}}`,
		},
		{
			name:    "node monitor",
			request: controller.NewNodeMonitorTask("node-1", "10s"),
			want:    `{"name":"node-monitor","data":{"node":"node-1","interval":"10s"}}`,
		},
		{
			name:    "sockets monitor",
			request: controller.NewSocketsMonitorTask("2s", 3, 17, 2),
			want:    `{"name":"sockets-monitor","data":{"interval":"2s","state":3,"proto":17,"family":2}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srv, transport, stop := startNotificationTestStream(t, "node-1")
			defer stop()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_, err := srv.StartTask(ctx, transport.nodeID, test.request)
			if err != nil {
				t.Fatalf("StartTask returned error: %v", err)
			}
			notification := <-transport.sent
			if notification.GetType() != pb.Action_TASK_START {
				t.Fatalf("expected TASK_START, got %s", notification.GetType())
			}
			assertJSONEqual(t, notification.GetData(), test.want)
		})
	}
}

func TestTaskStreamsSharePendingNotificationBound(t *testing.T) {
	srv, transport, stop := startNotificationTestStream(t, "node-1")
	defer stop()

	for index := range notificationPendingLimit {
		_, err := srv.StartTask(
			context.Background(),
			transport.nodeID,
			controller.NewPIDMonitorTask(index+1, "5s"),
		)
		if err != nil {
			t.Fatalf("StartTask %d returned error: %v", index, err)
		}
		<-transport.sent
	}

	_, err := srv.StartTask(
		context.Background(),
		transport.nodeID,
		controller.NewPIDMonitorTask(notificationPendingLimit+1, "5s"),
	)
	if !errors.Is(err, ErrNotificationBackpressure) {
		t.Fatalf("expected pending notification bound, got %v", err)
	}
}

func TestStopTaskWorksAtPendingNotificationBound(t *testing.T) {
	srv, transport, stop := startNotificationTestStream(t, "node-1")
	defer stop()

	streams := make([]controller.TaskStream, 0, notificationPendingLimit)
	for index := range notificationPendingLimit {
		stream, err := srv.StartTask(
			context.Background(),
			transport.nodeID,
			controller.NewPIDMonitorTask(index+1, "5s"),
		)
		if err != nil {
			t.Fatalf("StartTask %d returned error: %v", index, err)
		}
		streams = append(streams, stream)
		<-transport.sent
	}

	stopped := make(chan error, 1)
	go func() {
		stopped <- srv.StopTask(context.Background(), streams[0])
	}()

	stopNotification := <-transport.sent
	if stopNotification.GetType() != pb.Action_TASK_STOP {
		t.Fatalf("expected TASK_STOP, got %s", stopNotification.GetType())
	}
	transport.replies <- &pb.NotificationReply{
		Id:   stopNotification.GetId(),
		Code: pb.NotificationReplyCode_OK,
	}
	if err := <-stopped; err != nil {
		t.Fatalf("StopTask returned error at pending limit: %v", err)
	}
	waitForTaskDone(t, streams[0])
	if got := pendingNotificationCount(streams[1]); got != notificationPendingLimit-1 {
		t.Fatalf("expected remaining task streams to stay registered, got %d", got)
	}
}

func waitForTaskDone(t *testing.T, stream controller.TaskStream) {
	t.Helper()
	select {
	case <-stream.Done():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for task stream to close")
	}
}

func pendingNotificationCount(stream controller.TaskStream) int {
	task := stream.(*taskStream)
	task.session.mu.Lock()
	defer task.session.mu.Unlock()
	return len(task.session.pending)
}

func assertJSONEqual(t *testing.T, got, want string) {
	t.Helper()
	var gotValue any
	if err := json.Unmarshal([]byte(got), &gotValue); err != nil {
		t.Fatalf("decode actual JSON: %v", err)
	}
	var wantValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("decode expected JSON: %v", err)
	}
	gotJSON, _ := json.Marshal(gotValue)
	wantJSON, _ := json.Marshal(wantValue)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("expected JSON %s, got %s", wantJSON, gotJSON)
	}
}
