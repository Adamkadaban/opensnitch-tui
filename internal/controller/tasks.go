package controller

import (
	"context"
	"encoding/json"
	"strconv"
)

// TaskName identifies a daemon background task.
type TaskName string

const (
	TaskPIDMonitor     TaskName = "pid-monitor"
	TaskNodeMonitor    TaskName = "node-monitor"
	TaskSocketsMonitor TaskName = "sockets-monitor"
)

// TaskRequest contains the typed configuration sent to a daemon task.
type TaskRequest struct {
	Name TaskName `json:"name"`
	Data any      `json:"data"`
}

// PIDMonitorConfig configures process detail updates.
type PIDMonitorConfig struct {
	Interval string `json:"interval"`
	PID      string `json:"pid"`
}

// NodeMonitorConfig configures node resource updates.
type NodeMonitorConfig struct {
	Node     string `json:"node"`
	Interval string `json:"interval"`
}

// SocketsMonitorConfig configures socket table updates.
type SocketsMonitorConfig struct {
	Interval string `json:"interval"`
	State    uint8  `json:"state"`
	Proto    uint8  `json:"proto"`
	Family   uint8  `json:"family"`
}

// NewPIDMonitorTask creates a pid-monitor request.
func NewPIDMonitorTask(pid int, interval string) TaskRequest {
	return TaskRequest{
		Name: TaskPIDMonitor,
		Data: PIDMonitorConfig{Interval: interval, PID: strconv.Itoa(pid)},
	}
}

// NewNodeMonitorTask creates a node-monitor request.
func NewNodeMonitorTask(node, interval string) TaskRequest {
	return TaskRequest{
		Name: TaskNodeMonitor,
		Data: NodeMonitorConfig{Node: node, Interval: interval},
	}
}

// NewSocketsMonitorTask creates a sockets-monitor request.
func NewSocketsMonitorTask(interval string, state, proto, family uint8) TaskRequest {
	return TaskRequest{
		Name: TaskSocketsMonitor,
		Data: SocketsMonitorConfig{
			Interval: interval,
			State:    state,
			Proto:    proto,
			Family:   family,
		},
	}
}

// TaskUpdate contains one daemon task result.
type TaskUpdate struct {
	Data json.RawMessage
}

// TaskStream is a bounded stream of updates for one running daemon task.
type TaskStream interface {
	ID() uint64
	NodeID() string
	Name() TaskName
	Updates() <-chan TaskUpdate
	Done() <-chan struct{}
	Err() error
}

// TaskManager controls daemon task streams.
type TaskManager interface {
	StartTask(ctx context.Context, nodeID string, request TaskRequest) (TaskStream, error)
	StopTask(ctx context.Context, stream TaskStream) error
}
