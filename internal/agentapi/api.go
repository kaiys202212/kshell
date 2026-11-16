package agentapi

import (
	"context"
	"sync"
	"time"

	"github.com/yangk/kshell/internal/hubtools"
)

// Backend 是 API 的执行端口：desktop 层提供实现（复用 App 既有方法），
// 测试用桩实现。方法语义与 internal/hub.Executor 同源，但类型面向工具消费者。
type Backend interface {
	// ActiveSessions 返回活跃 chat 会话（含状态/摘要/LastTs，已按链头归一）。
	ActiveSessions() []Session
	// DiskSessions 返回未激活的磁盘会话（已按 ACP 能力过滤、终端占用已标记）。
	DiskSessions() []Session
	// WorkspaceSessions 返回指定工作区最近 n 个磁盘会话（按最后修改时间
	// 新→旧），供管理者 system 注入「各项目最近会话」段（模型据此理解
	// 投递/恢复目标）。
	WorkspaceSessions(wsPath string, n int) []Session
	// ArchivedIDs 返回已归档的磁盘会话 ID 名单。
	ArchivedIDs() []string
	// SessionMessages 返回指定活跃 chat 的全部可见消息（时间正序，已滤噪）。
	SessionMessages(chatID string) []Message
	// RecentHubMessages 返回 AgentHub 最近 n 条可见消息（时间正序，已滤噪），
	// 供管理者循环注入投递语境。
	RecentHubMessages(n int) []Message
	// Send 向活跃 chat 发送一轮用户输入。
	Send(chatID, text string) error
	// Stop 中断指定 chat 当前轮次。
	Stop(chatID string) error
	// Recall 按 hub 消息 ID 撤回一条用户消息。
	Recall(msgID int64) error
	// ListModels 返回 chat 可选模型与当前模型。
	ListModels(chatID string) ([]ModelOption, string, error)
	// SetModel 运行时切换 chat 模型。
	SetModel(chatID, model string) error
	// GrantPermission 处理权限请求（allow=true 单次允许，false 拒绝）。
	GrantPermission(chatID, reqID string, allow bool) error
	// Create 新建 ACP 会话（toolID/workspace/模型）。
	Create(toolID, wsID, model string) (Session, error)
	// Open 激活一个磁盘会话。
	Open(sessionID string) (Session, error)
	// Tools 返回已安装工具候选（CreateNL 用）。
	Tools() []Option
	// Workspaces 返回工作区候选（CreateNL 用）。
	Workspaces() []Option
	// Feed 返回事件多播源（desktop 从 chat 回调链转发，见 feed.go）。
	Feed() *Feed
}

// API 是工具层服务。所有方法并发安全。
type API struct {
	b Backend

	mu     sync.Mutex
	llm    hubtools.Config
	chatFn func(ctx context.Context, msgs []hubtools.Message) (hubtools.Response, error)

	// —— 管理者循环级状态（mu 保护；语义见 loop.go）——
	busy    bool          // Run/Resume 重入门禁
	suspend *suspendState // 挂起-续跑语境（ask 落地时写入，Resume/短输入 Run 消费）
	mem     *ManagerMemory // 管理者记忆 journal（nil = 未启用，Run 跳过记忆读写）
}

// New 构造 API。b 必须非 nil。
func New(b Backend) *API { return &API{b: b} }

// UpdateLLM 注入/刷新 LLM 配置（desktop 在配置变更时调用，与 hubtools 桥同源）。
func (a *API) UpdateLLM(cfg hubtools.Config) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.llm = cfg
	a.chatFn = nil
}

// llmCfg 返回当前 LLM 配置快照。
func (a *API) llmCfg() hubtools.Config {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.llm
}

// chat 发一次 LLM 请求：测试注入 chatFn，生产走 hubtools.Client。
// 未配置模型返回 ErrLLMNotReady。
func (a *API) chat(ctx context.Context, msgs []hubtools.Message) (hubtools.Response, error) {
	a.mu.Lock()
	cfg, fn := a.llm, a.chatFn
	a.mu.Unlock()
	if cfg.Model == "" {
		return hubtools.Response{}, ErrLLMNotReady
	}
	if fn != nil {
		return fn(ctx, msgs)
	}
	return hubtools.NewClient(cfg).Chat(ctx, msgs)
}

// setChatFn 仅测试用：注入假 LLM。
func (a *API) setChatFn(fn func(ctx context.Context, msgs []hubtools.Message) (hubtools.Response, error)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.chatFn = fn
}

// now 取当前 UnixNano（测试可替换的钟）。
var now = time.Now
