package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
	"google.golang.org/grpc/peer"
)

const fullNodeConfigJSON = `{
  "Server": {
    "Address": "unix:///tmp/osui.sock",
    "Authentication": {
      "Type": "mutual-tls",
      "Token": "secret-token",
      "TLSOptions": {
        "CACert": "CA-SECRET",
        "ClientKey": "KEY-SECRET",
        "SkipVerify": false,
        "UnknownTLSField": {"nested": [1, 2, 3]}
      }
    },
    "LogFile": "/var/log/opensnitchd.log",
    "UnknownServer": true
  },
  "DefaultAction": "allow",
  "DefaultDuration": "once",
  "InterceptUnknown": false,
  "ProcMonitorMethod": "ebpf",
  "LogLevel": 2,
  "LogUTC": true,
  "LogMicro": false,
  "UnknownTopLevel": {"precise": 12345678901234567890},
  "FwOptions": {
    "ConfigPath": "/etc/opensnitchd/system-fw.json",
    "MonitorInterval": "15s",
    "QueueBypass": true,
    "ActionOnOverflow": "drop"
  },
  "Rules": {
    "Path": "/etc/opensnitchd/rules/",
    "EnableChecksums": false,
    "UnknownRuleOption": "preserve"
  },
  "Stats": {
    "MaxEvents": 250,
    "MaxStats": 25,
    "Workers": 6
  },
  "Internal": {
    "GCPercent": 100,
    "FlushConnsOnStart": true,
    "UnknownInternal": [true, false]
  }
}`

func TestParseNodeConfigConvertsSafeSubsetAndMetadata(t *testing.T) {
	parsed := parseNodeConfig(fullNodeConfigJSON, 5)
	if parsed.ParseError != "" {
		t.Fatalf("unexpected parse error: %s", parsed.ParseError)
	}
	config := parsed.Config
	if config.DefaultAction != "allow" ||
		config.ProcMonitorMethod != "ebpf" ||
		config.LogLevel != 2 ||
		!config.LogUTC ||
		config.Rules.Path != "/etc/opensnitchd/rules/" ||
		config.FwOptions.MonitorInterval != "15s" ||
		config.Stats.MaxEvents != 250 {
		t.Fatalf("unexpected parsed config: %+v", config)
	}
	if parsed.Metadata.AuthenticationType != "mutual-tls" || !parsed.Metadata.TLSConfigured {
		t.Fatalf("unexpected metadata: %+v", parsed.Metadata)
	}
	if parsed.RawJSON != fullNodeConfigJSON {
		t.Fatal("raw JSON was not preserved")
	}
}

func TestParseNodeConfigUsesReportedLogLevelWhenMissing(t *testing.T) {
	parsed := parseNodeConfig(`{
		"DefaultAction":"deny",
		"DefaultDuration":"once",
		"ProcMonitorMethod":"proc"
	}`, 4)
	if parsed.ParseError != "" || parsed.Config.LogLevel != 4 {
		t.Fatalf("expected reported log level fallback, got %+v", parsed)
	}
}

func TestSubscribeRetainsMalformedNodeConfig(t *testing.T) {
	store := state.NewStore()
	srv := New(store, Options{})
	ctx := peer.NewContext(context.Background(), &peer.Peer{
		Addr: &testAddr{network: "tcp", value: "1.2.3.4:5002"},
	})
	raw := `{"Server":{"Authentication":`
	if _, err := srv.Subscribe(ctx, &pb.ClientConfig{Config: raw, LogLevel: 3}); err != nil {
		t.Fatalf("malformed config failed subscription: %v", err)
	}
	config, ok := store.NodeConfig("tcp://1.2.3.4")
	if !ok || config.RawJSON != raw || config.ParseError == "" {
		t.Fatalf("expected retained raw config and parse error, got %+v", config)
	}
	if store.Snapshot().Nodes[0].Status != state.NodeStatusReady {
		t.Fatal("malformed config should not prevent a ready subscription")
	}
}

func TestPatchNodeConfigPreservesUnknownAndSensitiveFields(t *testing.T) {
	config := parseNodeConfig(fullNodeConfigJSON, 0).Config
	config.DefaultAction = "reject"
	config.DefaultDuration = "always"
	config.ProcMonitorMethod = "audit"
	config.LogLevel = 5
	config.LogUTC = false
	config.LogMicro = true
	config.InterceptUnknown = true
	config.Rules.EnableChecksums = true
	config.Internal.FlushConnsOnStart = false
	config.Internal.GCPercent = 50
	config.FwOptions.MonitorInterval = "30s"
	config.FwOptions.QueueBypass = false
	config.Stats.MaxEvents = 500
	config.Stats.MaxStats = 50

	patched, err := patchNodeConfig(fullNodeConfigJSON, config)
	if err != nil {
		t.Fatalf("patchNodeConfig returned error: %v", err)
	}
	before := decodeTestDocument(t, fullNodeConfigJSON)
	after := decodeTestDocument(t, patched)

	for _, path := range [][]string{
		{"Server"},
		{"UnknownTopLevel"},
		{"FwOptions", "ConfigPath"},
		{"FwOptions", "ActionOnOverflow"},
		{"Rules", "Path"},
		{"Rules", "UnknownRuleOption"},
		{"Stats", "Workers"},
		{"Internal", "UnknownInternal"},
	} {
		if !reflect.DeepEqual(documentValue(before, path...), documentValue(after, path...)) {
			t.Fatalf("field %v changed: before=%#v after=%#v", path, documentValue(before, path...), documentValue(after, path...))
		}
	}
	if got := documentValue(after, "DefaultAction"); got != "reject" {
		t.Fatalf("safe field was not patched: %#v", got)
	}
	if got := documentValue(after, "Rules", "EnableChecksums"); got != true {
		t.Fatalf("nested safe field was not patched: %#v", got)
	}
	if got := documentValue(after, "Stats", "MaxEvents"); got != json.Number("500") {
		t.Fatalf("numeric safe field was not patched: %#v", got)
	}
}

func TestApplyNodeConfigWaitsForOKBeforeUpdatingStore(t *testing.T) {
	srv, stream, stop := startNotificationTestStream(t, "node-config-ok")
	defer stop()
	srv.store.SetNodeConfig(stream.nodeID, parseNodeConfig(fullNodeConfigJSON, 0))
	updated := parseNodeConfig(fullNodeConfigJSON, 0).Config
	updated.DefaultAction = "deny"

	result := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		result <- srv.ApplyNodeConfig(ctx, stream.nodeID, updated)
	}()

	notification := <-stream.sent
	if notification.GetType() != pb.Action_CHANGE_CONFIG {
		t.Fatalf("expected CHANGE_CONFIG, got %s", notification.GetType())
	}
	if current, _ := srv.store.NodeConfig(stream.nodeID); current.Config.DefaultAction != "allow" {
		t.Fatal("store changed before daemon acknowledgement")
	}
	stream.replies <- &pb.NotificationReply{
		Id:   notification.GetId(),
		Code: pb.NotificationReplyCode_OK,
	}
	if err := <-result; err != nil {
		t.Fatalf("ApplyNodeConfig returned error: %v", err)
	}
	if current, _ := srv.store.NodeConfig(stream.nodeID); current.Config.DefaultAction != "deny" {
		t.Fatalf("store was not updated after OK: %+v", current.Config)
	}
}

func TestApplyNodeConfigDaemonErrorLeavesStoreUnchanged(t *testing.T) {
	srv, stream, stop := startNotificationTestStream(t, "node-config-error")
	defer stop()
	original := parseNodeConfig(fullNodeConfigJSON, 0)
	srv.store.SetNodeConfig(stream.nodeID, original)
	updated := original.Config
	updated.DefaultAction = "deny"

	result := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		result <- srv.ApplyNodeConfig(ctx, stream.nodeID, updated)
	}()
	notification := <-stream.sent
	stream.replies <- &pb.NotificationReply{
		Id:   notification.GetId(),
		Code: pb.NotificationReplyCode_ERROR,
		Data: "invalid configuration",
	}
	err := <-result
	var replyErr *NotificationReplyError
	if !errors.As(err, &replyErr) || !strings.Contains(err.Error(), "invalid configuration") {
		t.Fatalf("expected daemon reply error, got %v", err)
	}
	current, _ := srv.store.NodeConfig(stream.nodeID)
	if current.RawJSON != original.RawJSON || current.Config != original.Config {
		t.Fatal("store changed after daemon ERROR")
	}
}

func TestApplyNodeConfigRejectsInvalidOrMalformedState(t *testing.T) {
	store := state.NewStore()
	srv := New(store, Options{})
	store.SetNodeConfig("node-1", state.NodeConfigState{
		RawJSON:    `{`,
		ParseError: "unexpected end",
	})
	if err := srv.ApplyNodeConfig(context.Background(), "node-1", validDaemonConfig()); err == nil {
		t.Fatal("expected malformed state rejection")
	}

	invalid := validDaemonConfig()
	invalid.ProcMonitorMethod = "unknown"
	if err := srv.ApplyNodeConfig(context.Background(), "node-1", invalid); err == nil {
		t.Fatal("expected invalid enum rejection")
	}
}

func decodeTestDocument(t *testing.T, raw string) map[string]any {
	t.Helper()
	document, err := decodeConfigDocument(raw)
	if err != nil {
		t.Fatalf("decode document: %v", err)
	}
	return document
}

func documentValue(document map[string]any, path ...string) any {
	var current any = document
	for _, segment := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[segment]
	}
	return current
}

func validDaemonConfig() state.NodeDaemonConfig {
	return state.NodeDaemonConfig{
		DefaultAction:     "deny",
		DefaultDuration:   "once",
		ProcMonitorMethod: "ebpf",
		LogLevel:          2,
		Rules:             state.NodeRulesConfig{Path: "/etc/opensnitchd/rules"},
		Internal:          state.NodeInternalConfig{GCPercent: 100},
		FwOptions:         state.NodeFirewallOptions{MonitorInterval: "15s", QueueBypass: true},
		Stats:             state.NodeStatsConfig{MaxEvents: 250, MaxStats: 25},
	}
}
