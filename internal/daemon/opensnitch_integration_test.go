//go:build linux

package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/adamkadaban/opensnitch-tui/internal/controller"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
)

const integrationSocketPath = "/tmp/osui.sock"

func TestOpenSnitchV18Integration(t *testing.T) {
	if reason := integrationGateReason(os.Getenv); reason != "" {
		t.Skip(reason)
	}
	if os.Geteuid() != 0 {
		t.Fatal("OpenSnitch integration test must run as root")
	}

	curlPath, err := exec.LookPath("curl")
	if err != nil {
		t.Fatalf("find curl: %v", err)
	}
	curlPath, err = filepath.Abs(curlPath)
	if err != nil {
		t.Fatalf("resolve curl path: %v", err)
	}

	store := state.NewStore()
	settings := store.Snapshot().Settings
	settings.PromptTimeout = 30 * time.Second
	store.SetSettings(settings)

	server := New(store, Options{
		ListenAddr:    "unix://" + integrationSocketPath,
		ServerName:    "opensnitch-tui-integration",
		ServerVersion: "integration",
	})
	serverCtx, cancelServer := context.WithCancel(context.Background())
	serverDone := make(chan struct{})
	var serverErr error
	go func() {
		serverErr = server.Start(serverCtx)
		close(serverDone)
	}()

	serverStopped := false
	t.Cleanup(func() {
		if !serverStopped {
			cancelServer()
			select {
			case <-serverDone:
				if serverErr != nil {
					t.Errorf("server cleanup returned error: %v", serverErr)
				}
			case <-time.After(5 * time.Second):
				t.Error("server did not stop during cleanup")
			}
		}
		removeIntegrationSocket(t)
	})
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		snapshot := store.Snapshot()
		t.Logf(
			"integration diagnostics: nodes=%+v firewall_count=%d prompt_count=%d last_error=%q",
			snapshot.Nodes,
			len(snapshot.SystemFirewalls),
			len(snapshot.Prompts),
			snapshot.LastError,
		)
	})

	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStartup()
	if err := waitForIntegrationSocket(startupCtx, serverDone, &serverErr); err != nil {
		t.Fatal(err)
	}
	t.Logf("server socket ready at %s; orchestration may start OpenSnitch", integrationSocketPath)

	readyCtx, cancelReady := context.WithTimeout(context.Background(), 45*time.Second)
	node, firewall, err := waitForReadyNode(readyCtx, store, serverDone, &serverErr)
	cancelReady()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf(
		"daemon ready: node=%q version=%q firewall_enabled=%t firewall_running=%t firewall_version=%d",
		node.Name,
		node.Version,
		firewall.Enabled,
		firewall.Running,
		firewall.Version,
	)

	firstTask := startNodeMonitor(serverCtx, t, server, node)
	stopTask(t, server, firstTask)

	secondTask := startNodeMonitor(serverCtx, t, server, node)
	stopTask(t, server, secondTask)

	reloadCtx, cancelReload := context.WithTimeout(context.Background(), 10*time.Second)
	err = server.ReloadFirewall(reloadCtx, node.ID)
	cancelReload()
	if err != nil {
		t.Fatalf("RELOAD_FW_RULES was not acknowledged: %v", err)
	}
	afterReload, ok := store.SystemFirewall(node.ID)
	if !ok {
		t.Fatal("system firewall state disappeared after reload")
	}
	if afterReload.Enabled != firewall.Enabled || afterReload.Running != firewall.Running {
		t.Fatalf(
			"reload changed firewall state: before enabled=%t running=%t, after enabled=%t running=%t",
			firewall.Enabled,
			firewall.Running,
			afterReload.Enabled,
			afterReload.Running,
		)
	}

	runCurlPromptFlow(serverCtx, t, server, store, curlPath)

	cancelServer()
	select {
	case <-serverDone:
		serverStopped = true
		if serverErr != nil {
			t.Fatalf("server returned error after cancellation: %v", serverErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not return cleanly after cancellation")
	}
	assertTaskStreamClean(t, firstTask)
	assertTaskStreamClean(t, secondTask)
}

func waitForIntegrationSocket(
	ctx context.Context,
	serverDone <-chan struct{},
	serverErr *error,
) error {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		info, err := os.Lstat(integrationSocketPath)
		if err == nil && info.Mode()&os.ModeSocket != 0 {
			return nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect integration socket: %w", err)
		}
		select {
		case <-serverDone:
			return fmt.Errorf("server returned before socket was ready: %v", *serverErr)
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for integration socket: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func waitForReadyNode(
	ctx context.Context,
	store *state.Store,
	serverDone <-chan struct{},
	serverErr *error,
) (state.Node, state.SystemFirewall, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if node, firewall, ok := readyNodeWithFirewall(store.Snapshot()); ok {
			return node, firewall, nil
		}
		select {
		case <-serverDone:
			return state.Node{}, state.SystemFirewall{}, fmt.Errorf(
				"server returned before daemon became ready: %v",
				*serverErr,
			)
		case <-ctx.Done():
			return state.Node{}, state.SystemFirewall{}, fmt.Errorf(
				"timed out waiting for NodeStatusReady and SystemFirewall: %w",
				ctx.Err(),
			)
		case <-ticker.C:
		}
	}
}

func startNodeMonitor(
	ctx context.Context,
	t *testing.T,
	server *Server,
	node state.Node,
) controller.TaskStream {
	t.Helper()
	request := controller.NewNodeMonitorTask(node.Name, "250ms")
	var stream controller.TaskStream
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for stream == nil {
		var err error
		stream, err = server.StartTask(ctx, node.ID, request)
		if err == nil {
			break
		}
		if !errors.Is(err, ErrNotificationNodeDisconnected) {
			t.Fatalf("TASK_START node-monitor: %v", err)
		}
		select {
		case <-deadline.C:
			t.Fatalf("timed out waiting for Notifications session: %v", err)
		case <-ticker.C:
		}
	}

	select {
	case update, ok := <-stream.Updates():
		if !ok {
			t.Fatalf("node-monitor closed before its first update: %v", stream.Err())
		}
		if err := validateNodeMonitorUpdate(update.Data); err != nil {
			t.Fatalf("invalid node-monitor update %q: %v", update.Data, err)
		}
		t.Logf("node-monitor update received: %s", update.Data)
	case <-stream.Done():
		t.Fatalf("node-monitor failed before its first update: %v", stream.Err())
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for node-monitor update")
	}
	return stream
}

func stopTask(t *testing.T, server *Server, stream controller.TaskStream) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.StopTask(ctx, stream); err != nil {
		t.Fatalf("TASK_STOP node-monitor: %v", err)
	}
	select {
	case <-stream.Done():
		if err := stream.Err(); err != nil {
			t.Fatalf("node-monitor stream ended with error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("node-monitor stream did not close after TASK_STOP")
	}
}

func runCurlPromptFlow(
	ctx context.Context,
	t *testing.T,
	server *Server,
	store *state.Store,
	curlPath string,
) {
	t.Helper()
	curlCtx, cancelCurl := context.WithTimeout(ctx, 30*time.Second)
	defer cancelCurl()

	var stderr bytes.Buffer
	command := exec.CommandContext(
		curlCtx,
		curlPath,
		"--fail",
		"--silent",
		"--show-error",
		"--location",
		"--connect-timeout",
		"10",
		"--max-time",
		"25",
		"https://example.com",
	)
	command.Stdout = io.Discard
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatalf("start controlled curl: %v", err)
	}
	curlDone := make(chan error, 1)
	go func() {
		curlDone <- command.Wait()
	}()

	promptCtx, cancelPrompt := context.WithTimeout(ctx, 15*time.Second)
	defer cancelPrompt()
	var prompt state.Prompt
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for prompt.ID == "" {
		for _, candidate := range store.Snapshot().Prompts {
			if executablePathsMatch(candidate.Connection.ProcessPath, curlPath) {
				prompt = candidate
				break
			}
		}
		if prompt.ID != "" {
			break
		}
		select {
		case err := <-curlDone:
			t.Fatalf("curl exited before an interactive prompt (stderr=%q): %v", stderr.String(), err)
		case <-promptCtx.Done():
			t.Fatalf(
				"timed out waiting for curl prompt by process path %q (stderr=%q)",
				curlPath,
				stderr.String(),
			)
		case <-ticker.C:
		}
	}

	if err := server.ResolvePrompt(controller.PromptDecision{
		PromptID: prompt.ID,
		Action:   controller.PromptActionAllow,
		Duration: controller.PromptDurationOnce,
		Target:   controller.PromptTargetProcessPath,
	}); err != nil {
		t.Fatalf("resolve curl prompt with allow-once: %v", err)
	}
	select {
	case err := <-curlDone:
		if err != nil {
			t.Fatalf("curl failed after allow-once decision (stderr=%q): %v", stderr.String(), err)
		}
	case <-curlCtx.Done():
		t.Fatalf("curl did not finish after allow-once decision (stderr=%q): %v", stderr.String(), curlCtx.Err())
	}
}

func assertTaskStreamClean(t *testing.T, stream controller.TaskStream) {
	t.Helper()
	select {
	case <-stream.Done():
	default:
		t.Fatal("task stream remained open after server shutdown")
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("task stream retained an error: %v", err)
	}
	task := stream.(*taskStream)
	task.session.mu.Lock()
	pending := len(task.session.pending)
	task.session.mu.Unlock()
	if pending != 0 {
		t.Fatalf("notification session retained %d pending task requests", pending)
	}
}

func removeIntegrationSocket(t *testing.T) {
	t.Helper()
	info, err := os.Lstat(integrationSocketPath)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Errorf("inspect integration socket during cleanup: %v", err)
		return
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Errorf("refusing to remove non-socket integration path %q", integrationSocketPath)
		return
	}
	if err := os.Remove(integrationSocketPath); err != nil {
		t.Errorf("remove integration socket: %v", err)
	}
}
