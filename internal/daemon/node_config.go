package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	pb "github.com/adamkadaban/opensnitch-tui/internal/pb/protocol"
	"github.com/adamkadaban/opensnitch-tui/internal/state"
)

var ErrNodeConfigNotFound = errors.New("node configuration not found")

func parseNodeConfig(raw string, reportedLogLevel uint32) state.NodeConfigState {
	result := state.NodeConfigState{RawJSON: raw}
	document, err := decodeConfigDocument(raw)
	if err != nil {
		result.ParseError = err.Error()
		return result
	}

	var config state.NodeDaemonConfig
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		result.ParseError = err.Error()
		return result
	}
	if _, ok := document["LogLevel"]; !ok {
		config.LogLevel = int(reportedLogLevel)
	}
	result.Config = config
	result.Metadata = configMetadata(document)
	return result
}

// ApplyNodeConfig patches and sends the complete per-node document.
func (s *Server) ApplyNodeConfig(
	ctx context.Context,
	nodeID string,
	config state.NodeDaemonConfig,
) error {
	if err := state.ValidateNodeDaemonConfig(config); err != nil {
		return err
	}
	current, ok := s.store.NodeConfig(nodeID)
	if !ok {
		return fmt.Errorf("%w for %s", ErrNodeConfigNotFound, nodeID)
	}
	if current.ParseError != "" {
		return fmt.Errorf("node configuration for %s is invalid: %s", nodeID, current.ParseError)
	}

	patched, err := patchNodeConfig(current.RawJSON, config)
	if err != nil {
		return fmt.Errorf("patch node configuration: %w", err)
	}
	if _, err := s.SendNotification(ctx, nodeID, &pb.Notification{
		Type: pb.Action_CHANGE_CONFIG,
		Data: patched,
	}); err != nil {
		return err
	}

	s.store.SetNodeConfig(nodeID, parseNodeConfig(patched, uint32(config.LogLevel)))
	return nil
}

func patchNodeConfig(raw string, config state.NodeDaemonConfig) (string, error) {
	document, err := decodeConfigDocument(raw)
	if err != nil {
		return "", err
	}

	document["DefaultAction"] = config.DefaultAction
	document["DefaultDuration"] = config.DefaultDuration
	document["ProcMonitorMethod"] = config.ProcMonitorMethod
	document["LogLevel"] = config.LogLevel
	document["LogUTC"] = config.LogUTC
	document["LogMicro"] = config.LogMicro
	document["InterceptUnknown"] = config.InterceptUnknown

	rules, err := objectField(document, "Rules")
	if err != nil {
		return "", err
	}
	rules["EnableChecksums"] = config.Rules.EnableChecksums
	internal, err := objectField(document, "Internal")
	if err != nil {
		return "", err
	}
	internal["FlushConnsOnStart"] = config.Internal.FlushConnsOnStart
	internal["GCPercent"] = config.Internal.GCPercent
	firewall, err := objectField(document, "FwOptions")
	if err != nil {
		return "", err
	}
	firewall["MonitorInterval"] = config.FwOptions.MonitorInterval
	firewall["QueueBypass"] = config.FwOptions.QueueBypass
	stats, err := objectField(document, "Stats")
	if err != nil {
		return "", err
	}
	stats["MaxEvents"] = config.Stats.MaxEvents
	stats["MaxStats"] = config.Stats.MaxStats

	patched, err := json.Marshal(document)
	if err != nil {
		return "", err
	}
	return string(patched), nil
}

func decodeConfigDocument(raw string) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.UseNumber()
	var document map[string]any
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	if document == nil {
		return nil, errors.New("configuration must be a JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("configuration contains trailing JSON")
		}
		return nil, err
	}
	return document, nil
}

func objectField(document map[string]any, name string) (map[string]any, error) {
	value, exists := document[name]
	if !exists {
		nested := make(map[string]any)
		document[name] = nested
		return nested, nil
	}
	if existing, ok := value.(map[string]any); ok {
		return existing, nil
	}
	return nil, fmt.Errorf("%s must be a JSON object", name)
}

func configMetadata(document map[string]any) state.NodeConfigMetadata {
	server, _ := document["Server"].(map[string]any)
	auth, _ := server["Authentication"].(map[string]any)
	authType, _ := auth["Type"].(string)
	tlsOptions, _ := auth["TLSOptions"].(map[string]any)

	metadata := state.NodeConfigMetadata{
		AuthenticationType: authType,
		TLSConfigured:      strings.Contains(strings.ToLower(authType), "tls"),
	}
	if metadata.TLSConfigured {
		return metadata
	}
	for key, value := range tlsOptions {
		switch typed := value.(type) {
		case string:
			if key != "ClientAuthType" && typed != "" {
				metadata.TLSConfigured = true
			}
		case bool:
			if typed {
				metadata.TLSConfigured = true
			}
		}
	}
	return metadata
}
