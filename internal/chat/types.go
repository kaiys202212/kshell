// Package chat 编排 ACP agent 进程与会话：起进程、握手、new/load 会话、
// 把 session/update 归一化成前端时间线、转发权限请求。Backend 可打桩以便测试。
package chat

import (
	"context"
	"encoding/json"

	"github.com/yangk/kshell/internal/acp"
)

const (
	KindSession = "session"
	KindNew     = "new"

	StatusStarting = "starting"
	StatusReady    = "ready"
	StatusRunning  = "running"
	StatusExited   = "exited"
)

type Spec struct {
	Path string
	Args []string
	Dir  string
	Env  []string
}

type Info struct {
	ID        string
	Kind      string
	SessionID string
	Workspace string
	Title     string
	ToolID    string
	Status    string
	ExitCode  int
	Error     string
}

// ToolCall 是发给前端的工具调用快照（字段按前端约定的 PascalCase 序列化）。
type ToolCall struct {
	ToolCallID string            `json:"ToolCallID"`
	Name       string            `json:"Name,omitempty"`
	Title      string            `json:"Title,omitempty"`
	Kind       string            `json:"Kind,omitempty"`
	Status     string            `json:"Status,omitempty"`
	Content    []json.RawMessage `json:"Content,omitempty"`
	RawInput   json.RawMessage   `json:"RawInput,omitempty"`
	RawOutput  json.RawMessage   `json:"RawOutput,omitempty"`
}

type PlanEntry struct {
	Content  string `json:"Content"`
	Priority string `json:"Priority,omitempty"`
	Status   string `json:"Status,omitempty"`
}

// Update 是归一化时间线事件，带单调 Seq 供前端去重。
type Update struct {
	Seq        int64       `json:"Seq"`
	Type       string      `json:"Type"` // user | assistant | thought | tool | plan | turn_done | error
	MessageID  string      `json:"MessageID,omitempty"`
	Text       string      `json:"Text,omitempty"`
	ToolCallID string      `json:"ToolCallID,omitempty"`
	Tool       *ToolCall   `json:"Tool,omitempty"`
	Plan       []PlanEntry `json:"Plan,omitempty"`
	StopReason string      `json:"StopReason,omitempty"`
}

type PermissionOption struct {
	OptionID string `json:"OptionID"`
	Name     string `json:"Name"`
	Kind     string `json:"Kind"`
}

type PermissionRequest struct {
	RequestID string             `json:"RequestID"`
	SessionID string             `json:"SessionID"`
	ToolCall  ToolCall           `json:"ToolCall"`
	Options   []PermissionOption `json:"Options"`
}

// Conn 是 ACP 连接的高层封装；真实实现是 acp.Agent，测试注入假实现。
type Conn interface {
	Initialize(ctx context.Context) (acp.InitializeResult, error)
	NewSession(ctx context.Context, cwd string) (string, error)
	LoadSession(ctx context.Context, sessionID, cwd string) error
	Prompt(ctx context.Context, sessionID, text string) (string, error)
	Cancel(sessionID string) error
	Wait() (int, error)
	Close() error
}

// Backend 启动一条 ACP 连接；可打桩。
type Backend interface {
	Start(spec Spec, h acp.Handler) (Conn, error)
}
