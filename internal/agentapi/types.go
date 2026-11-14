// Package agentapi 面向外部 agent（未来的 harness 平台 / CLI / MCP 接入层）
// 提供「操作 kshell」的稳定工具 API：会话发现、输出读取与订阅、会话操控、
// 创建与自然语言创建、消息投递路由。
//
// 采用 port/adapter 模式（与 internal/hub 的 Console/Executor 同构）：本包定义
// 全部类型与 Backend 端口接口并实现 API 服务；desktop 层提供 Backend 适配器
// （复用 App 既有方法）。本包不依赖 internal/desktop、internal/chat，
// 避免低层反向依赖上层。
package agentapi

import "errors"

// Scope 是 List 的会话范围。
type Scope string

const (
	ScopeActive   Scope = "active"   // 活跃 chat（含运行状态）
	ScopeDisk     Scope = "disk"     // 未激活的磁盘会话
	ScopeArchived Scope = "archived" // 已归档会话
	ScopeAll      Scope = "all"      // active + disk
)

// Status 是会话状态（对齐 hub #st 的四态，补充磁盘/归档两态）。
type Status string

const (
	StatusRunning  Status = "running"  // 运行中（含启动/认证中）
	StatusPerm     Status = "perm"     // 等待权限/人工确认
	StatusIdle     Status = "idle"     // 空闲
	StatusGone     Status = "gone"     // 进程已退出
	StatusDisk     Status = "disk"     // 未激活的磁盘会话
	StatusArchived Status = "archived" // 已归档
)

// Session 是工具层可见的一个会话。ID 是活跃 chat 的链头 ID（未激活为空）；
// SessionID 是磁盘会话 ID。两者都可能作为寻址键（resolve 归一）。
type Session struct {
	ID             string
	SessionID      string
	Tool           string
	ToolName       string
	Title          string
	Workspace      string
	Status         Status
	TerminalActive bool // 以终端模式运行（不可投递）
	LastTs         int64
	Summary        string // 最后一条 agent 可见消息的单行截断
}

// Message 是工具层可见的一条会话消息（来自 hub store，已滤噪）。
type Message struct {
	ID        int64  // hub 消息 ID（Recall 用）
	SessionID string // 会话锚：活跃 chat 链头 ID（无归因时为空）
	Kind      string // user | agent
	Text      string
	Event     string // agent 消息的事件语义（Stop/Notification/error...）
	Status    string // 用户消息投递状态（queued/recalled）
	Ts        int64
}

// ModelOption 是会话可切换的一个模型。
type ModelOption struct {
	ID   string
	Name string
}

// Option 是通用选项（工具/工作区候选）。
type Option struct {
	ID    string
	Label string
}

// Permission 是一条待处理的权限请求。
type Permission struct {
	ChatID    string
	RequestID string
	Title     string
	Ts        int64
}

// Event 是 Watch 订阅到的一帧会话事件。
type Event struct {
	SessionID string // chat 链头 ID
	Kind      string // update | permission | exit
	Type      string // Kind=update 时的更新类型（assistant/turn_done/error...）
	Text      string
	Ts        int64
}

// Event Kind 取值。
const (
	EventUpdate     = "update"
	EventPermission = "permission"
	EventExit       = "exit"
)

// 哨兵错误：调用方按 errors.Is 分支处理。
var (
	ErrNotFound   = errors.New("agentapi: 会话不存在")
	ErrLLMNotReady = errors.New("agentapi: LLM 未配置")
)
