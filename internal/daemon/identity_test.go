package daemon

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func TestSameIPDifferentDaemonNamesRemainIndependent(t *testing.T) {
	store := state.NewStore()
	srv := New(store, Options{})
	alphaCtx := tcpPeerContext("10.0.0.8:41001")
	betaCtx := tcpPeerContext("10.0.0.8:41002")

	subscribeNode(alphaCtx, t, srv, "alpha")
	subscribeNode(betaCtx, t, srv, "beta")

	alphaID := stableNodeIdentity(alphaCtx, "alpha")
	betaID := stableNodeIdentity(betaCtx, "beta")
	if alphaID == betaID {
		t.Fatal("different daemon names produced the same stable identity")
	}
	if got := len(store.Snapshot().Nodes); got != 2 {
		t.Fatalf("expected two same-IP nodes, got %d", got)
	}

	alphaSession, err := srv.registerTransportSession(transportIdentity(alphaCtx))
	if err != nil {
		t.Fatalf("register alpha session: %v", err)
	}
	betaSession, err := srv.registerTransportSession(transportIdentity(betaCtx))
	if err != nil {
		t.Fatalf("register beta session: %v", err)
	}
	store.SetRules(alphaID, []state.Rule{{Name: "alpha-rule"}})
	store.SetRules(betaID, []state.Rule{{Name: "beta-rule"}})

	if err := srv.EnableRule(alphaID, "alpha-rule"); err != nil {
		t.Fatalf("enable alpha rule: %v", err)
	}
	select {
	case notification := <-alphaSession.send:
		if notification.GetClientName() != alphaID {
			t.Fatalf("alpha action routed with client %q", notification.GetClientName())
		}
	case <-time.After(time.Second):
		t.Fatal("alpha action was not routed")
	}
	select {
	case notification := <-betaSession.send:
		t.Fatalf("alpha action leaked to beta: %+v", notification)
	default:
	}
}

func TestReconnectNewPortReusesStableNode(t *testing.T) {
	store := state.NewStore()
	srv := New(store, Options{})
	oldCtx := tcpPeerContext("10.0.0.9:42001")
	newCtx := tcpPeerContext("10.0.0.9:42002")

	subscribeNode(oldCtx, t, srv, "gateway")
	oldSession, err := srv.registerTransportSession(transportIdentity(oldCtx))
	if err != nil {
		t.Fatalf("register old session: %v", err)
	}
	subscribeNode(newCtx, t, srv, "gateway")

	stableID := stableNodeIdentity(newCtx, "gateway")
	if got := len(store.Snapshot().Nodes); got != 1 {
		t.Fatalf("expected reconnect to retain one node, got %d", got)
	}
	select {
	case <-oldSession.done:
	default:
		t.Fatal("old notification session was not replaced")
	}
	if _, err := srv.registerTransportSession(transportIdentity(newCtx)); err != nil {
		t.Fatalf("register replacement session: %v", err)
	}
	srv.sessionsMu.RLock()
	current := srv.sessions[stableID]
	srv.sessionsMu.RUnlock()
	if current == nil || current.transportID != transportIdentity(newCtx) {
		t.Fatalf("replacement session not installed for %s", stableID)
	}
}

func TestStaleNotificationTeardownDoesNotDisconnectReplacement(t *testing.T) {
	store := state.NewStore()
	srv := New(store, Options{})
	oldCtx := tcpPeerContext("10.0.0.10:43001")
	newCtx := tcpPeerContext("10.0.0.10:43002")

	subscribeNode(oldCtx, t, srv, "edge")
	oldStream := startNotificationStreamForContext(oldCtx, t, srv)
	newStream := startNotificationStreamForContext(newCtx, t, srv)
	subscribeNode(newCtx, t, srv, "edge")

	select {
	case <-oldStream.finished:
	case <-time.After(time.Second):
		t.Fatal("superseded notification stream did not stop")
	}
	stableID := stableNodeIdentity(newCtx, "edge")
	snapshot := store.Snapshot()
	if len(snapshot.Nodes) != 1 || snapshot.Nodes[0].ID != stableID {
		t.Fatalf("unexpected node state after reconnect: %+v", snapshot.Nodes)
	}
	if snapshot.Nodes[0].Status != state.NodeStatusReady {
		t.Fatalf("stale stream marked replacement %s", snapshot.Nodes[0].Status)
	}

	newStream.cancel()
	<-newStream.finished
}

func TestPingBeforeSubscribeMigratesProvisionalState(t *testing.T) {
	store := state.NewStore()
	srv := New(store, Options{})
	ctx := tcpPeerContext("10.0.0.11:44001")
	transportID := transportIdentity(ctx)

	if _, err := srv.Ping(ctx, &pb.PingRequest{Stats: &pb.Statistics{Connections: 17}}); err != nil {
		t.Fatalf("pre-Subscribe Ping: %v", err)
	}
	if _, err := srv.PostAlert(ctx, &pb.Alert{Id: 1, Data: &pb.Alert_Text{Text: "before subscribe"}}); err != nil {
		t.Fatalf("pre-Subscribe PostAlert: %v", err)
	}
	subscribeNode(ctx, t, srv, "sensor")

	stableID := stableNodeIdentity(ctx, "sensor")
	snapshot := store.Snapshot()
	if len(snapshot.Nodes) != 1 || snapshot.Nodes[0].ID != stableID {
		t.Fatalf("provisional node was not migrated: %+v", snapshot.Nodes)
	}
	if _, ok := snapshot.StatsByNode[transportID]; ok {
		t.Fatalf("provisional stats remain under %s", transportID)
	}
	if got := snapshot.StatsByNode[stableID].Connections; got != 17 {
		t.Fatalf("expected migrated connection count 17, got %d", got)
	}
	if len(snapshot.Alerts) != 1 || snapshot.Alerts[0].NodeID != stableID {
		t.Fatalf("provisional alert was not migrated: %+v", snapshot.Alerts)
	}
}

func TestAskRuleBeforeSubscribeMigratesPrompt(t *testing.T) {
	store := state.NewStore()
	settings := store.Snapshot().Settings
	settings.PromptTimeout = 5 * time.Second
	store.SetSettings(settings)
	srv := New(store, Options{})
	ctx := tcpPeerContext("10.0.0.15:48001")
	result := make(chan error, 1)

	go func() {
		_, err := srv.AskRule(ctx, &pb.Connection{ProcessPath: "/usr/bin/curl"})
		result <- err
	}()

	var prompt state.Prompt
	deadline := time.Now().Add(time.Second)
	for prompt.ID == "" && time.Now().Before(deadline) {
		prompts := store.Snapshot().Prompts
		if len(prompts) > 0 {
			prompt = prompts[0]
			break
		}
		time.Sleep(time.Millisecond)
	}
	if prompt.ID == "" {
		t.Fatal("pre-Subscribe prompt was not registered")
	}

	subscribeNode(ctx, t, srv, "prompt-node")
	stableID := stableNodeIdentity(ctx, "prompt-node")
	prompts := store.Snapshot().Prompts
	if len(prompts) != 1 || prompts[0].NodeID != stableID || prompts[0].NodeName != "prompt-node" {
		t.Fatalf("prompt was not migrated to stable identity: %+v", prompts)
	}
	if err := srv.ResolvePrompt(controller.PromptDecision{
		PromptID: prompt.ID,
		Action:   controller.PromptActionAllow,
		Duration: controller.PromptDurationOnce,
		Target:   controller.PromptTargetProcessPath,
	}); err != nil {
		t.Fatalf("resolve migrated prompt: %v", err)
	}
	if err := <-result; err != nil {
		t.Fatalf("AskRule returned error: %v", err)
	}
	if len(store.Snapshot().Rules[stableID]) != 1 {
		t.Fatalf("resolved prompt rule was not stored for %s", stableID)
	}
}

func TestUnixStableIdentityIncludesDaemonName(t *testing.T) {
	first := unixPeerContext("@daemon-a")
	second := unixPeerContext("@daemon-b")
	firstID := stableNodeIdentity(first, "alpha host")
	secondID := stableNodeIdentity(second, "beta/host")
	if firstID == secondID {
		t.Fatal("Unix daemon names collapsed to one identity")
	}
	if !strings.HasPrefix(firstID, "unix://local/name/") || strings.Contains(firstID, "alpha host") {
		t.Fatalf("Unix name was not encoded in %q", firstID)
	}
	store := state.NewStore()
	srv := New(store, Options{})
	subscribeNode(first, t, srv, "alpha host")
	subscribeNode(second, t, srv, "beta/host")
	if got := len(store.Snapshot().Nodes); got != 2 {
		t.Fatalf("expected two named Unix nodes, got %d", got)
	}

	emptyFirst := stableNodeIdentity(tcpPeerContext("10.0.0.12:45001"), "")
	emptySecond := stableNodeIdentity(tcpPeerContext("10.0.0.12:45002"), "  ")
	if emptyFirst != emptySecond {
		t.Fatalf("empty-name fallback was not stable: %q != %q", emptyFirst, emptySecond)
	}
}

func TestDuplicateSameNameSupersedesOldTransportWithoutDuplicateState(t *testing.T) {
	store := state.NewStore()
	srv := New(store, Options{})
	oldCtx := tcpPeerContext("10.0.0.13:46001")
	newCtx := tcpPeerContext("10.0.0.13:46002")

	subscribeNode(oldCtx, t, srv, "duplicate")
	subscribeNode(newCtx, t, srv, "duplicate")

	if got := len(store.Snapshot().Nodes); got != 1 {
		t.Fatalf("latest-wins duplicate policy corrupted node state: %d nodes", got)
	}
	_, err := srv.Ping(oldCtx, &pb.PingRequest{})
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), "superseded") {
		t.Fatalf("expected old duplicate transport rejection, got %v", err)
	}
	if _, err := srv.Ping(newCtx, &pb.PingRequest{}); err != nil {
		t.Fatalf("latest duplicate transport should own the node: %v", err)
	}
}

func TestConcurrentSameIPNodeTrafficRemainsIsolated(t *testing.T) {
	store := state.NewStore()
	srv := New(store, Options{})
	alphaCtx := tcpPeerContext("10.0.0.14:47001")
	betaCtx := tcpPeerContext("10.0.0.14:47002")
	subscribeNode(alphaCtx, t, srv, "alpha")
	subscribeNode(betaCtx, t, srv, "beta")

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(value uint64) {
			defer wg.Done()
			_, _ = srv.Ping(alphaCtx, &pb.PingRequest{Stats: &pb.Statistics{Connections: value}})
		}(uint64(i))
		go func(value uint64) {
			defer wg.Done()
			_, _ = srv.Ping(betaCtx, &pb.PingRequest{Stats: &pb.Statistics{Connections: value}})
		}(uint64(i + 100))
	}
	wg.Wait()

	snapshot := store.Snapshot()
	if len(snapshot.Nodes) != 2 || len(snapshot.StatsByNode) != 2 {
		t.Fatalf("concurrent traffic collapsed node state: nodes=%d stats=%d", len(snapshot.Nodes), len(snapshot.StatsByNode))
	}
}

func TestPreSubscribeTaskSessionMigratesToStableNode(t *testing.T) {
	store := state.NewStore()
	srv := New(store, Options{})
	ctx := tcpPeerContext("10.0.0.16:49001")
	transportID := transportIdentity(ctx)
	sess, err := srv.registerTransportSession(transportID)
	if err != nil {
		t.Fatalf("register provisional session: %v", err)
	}
	stream, err := srv.StartTask(
		context.Background(),
		transportID,
		controller.NewNodeMonitorTask("10.0.0.16", "5s"),
	)
	if err != nil {
		t.Fatalf("start provisional task: %v", err)
	}
	<-sess.send

	subscribeNode(ctx, t, srv, "task-node")
	stableID := stableNodeIdentity(ctx, "task-node")
	if stream.NodeID() != stableID {
		t.Fatalf("task node id was not migrated: %q", stream.NodeID())
	}
	if err := srv.StopTask(context.Background(), stream); err != nil {
		t.Fatalf("stop migrated task: %v", err)
	}
	select {
	case notification := <-sess.send:
		if notification.GetType() != pb.Action_TASK_STOP || notification.GetClientName() != stableID {
			t.Fatalf("migrated task stop routed incorrectly: %+v", notification)
		}
	case <-time.After(time.Second):
		t.Fatal("migrated task stop was not routed")
	}
}

func subscribeNode(ctx context.Context, t *testing.T, srv *Server, name string) {
	t.Helper()
	if _, err := srv.Subscribe(ctx, &pb.ClientConfig{Name: name}); err != nil {
		t.Fatalf("Subscribe %q: %v", name, err)
	}
}

func tcpPeerContext(address string) context.Context {
	return peer.NewContext(context.Background(), &peer.Peer{
		Addr: &testAddr{network: "tcp", value: address},
	})
}

func unixPeerContext(address string) context.Context {
	return peer.NewContext(context.Background(), &peer.Peer{
		Addr: &testAddr{network: "unix", value: address},
	})
}

func startNotificationStreamForContext(
	ctx context.Context,
	t *testing.T,
	srv *Server,
) *notificationTestStream {
	t.Helper()
	baseCtx, cancel := context.WithCancel(ctx)
	stream := &notificationTestStream{
		ctx:         baseCtx,
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
	return stream
}
