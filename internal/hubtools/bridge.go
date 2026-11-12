package hubtools

// Bridge 是 hubtools 对宿主（desktop 层）能力的抽象：需求管理、工作区查询、
// 运行中会话查询。desktop 后续注入真实实现，测试注入内存桩。
// 本包不 import internal/desktop / internal/requirements 等，避免低层反向依赖，
// 因此自定义 JSON 友好的 View 结构（字段与 desktop 现有绑定输出同名对齐）。

// RequirementView 对齐 internal/requirements.Item 的对话场景字段：
// 模型读写需求只需要标识、内容、优先级、标签、项目、状态与会话快照，
// 不需要 ChatID 等前端空间 ID。
type RequirementView struct {
	ID           string   `json:"id"`
	Text         string   `json:"text"`
	Priority     string   `json:"priority"`
	Tags         []string `json:"tags,omitempty"`
	Project      string   `json:"project,omitempty"`
	Status       string   `json:"status"`
	DispatchedTo string   `json:"dispatchedTo,omitempty"`
	SessionTitle string   `json:"sessionTitle,omitempty"`
}

// WorkspaceView 对齐 internal/discovery.Workspace 的展示字段精简：
// 对话场景够用的标识、名称、路径、工具分布与会话数；ssh 工作区补 Kind/ConnName。
// discovery.Workspace 没有独立 ID 字段，本地以 Path、ssh 以 ConnID+RemotePath
// 标识，这里统一用 ID 字段承载（desktop 层注入时填 Ref/Path）。
type WorkspaceView struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Path         string         `json:"path"`
	SessionCount int            `json:"sessionCount"`
	ToolCounts   map[string]int `json:"toolCounts,omitempty"`
	LastUsed     string         `json:"lastUsed,omitempty"`
	Kind         string         `json:"kind,omitempty"` // local | ssh；空视为 local
	ConnName     string         `json:"connName,omitempty"`
}

// ChatLogEntry 是注入提示词的一条聊天记录摘要：会话归属（agent 名）、
// 消息发出方与截断后的正文，由宿主从 hub 存储映射而来。ChatID 不渲染进
// 提示词（编号即引用），供宿主构建编号→会话映射。
type ChatLogEntry struct {
	Agent  string
	Sender string
	Text   string
	ChatID string
}

// AgentView 是 list_agents 的会话视图一行：模型经编号（Num）引用会话完成
// 状态查询/投递/停止。编号由宿主按 ListSessions 顺序 1..N 赋值，与命令层
// #l / 智能路由快照同源。Status 与 SessionStatuses 四态一致。
type AgentView struct {
	Num           int    `json:"num"`
	ChatID        string `json:"chatId,omitempty"`
	Tool          string `json:"tool"`
	ToolName      string `json:"toolName,omitempty"`
	WorkspaceName string `json:"workspaceName,omitempty"` // 路径末段（匹配"策略"类口吻）
	WorkspacePath string `json:"workspacePath,omitempty"`
	Title         string `json:"title"`
	Status        string `json:"status"`
	Summary       string `json:"summary,omitempty"` // 最后一条 agent 可见消息单行截断
}

// AgentStatusView 是 get_agent_status 的返回：会话快照 + 该会话最近消息
//（recent 条数由调用方指定，0 = 不附）。
type AgentStatusView struct {
	Agent  AgentView      `json:"agent"`
	Recent []ChatLogEntry `json:"recent,omitempty"`
}

// Bridge 提供宿主能力查询与需求写入（写操作由确认门禁前置把关）。
type Bridge interface {
	ListRequirements() ([]RequirementView, error)
	GetRequirement(id string) (*RequirementView, error)
	CreateRequirement(project, text, priority string, tags []string) (RequirementView, error)
	UpdateRequirement(id, project, text, priority string, tags []string) (RequirementView, error)
	DeleteRequirement(id string) error
	ListWorkspaces() ([]WorkspaceView, error)
	GetWorkspace(id string) (*WorkspaceView, error)
	// RecentMessages 返回最近 n 条纯文本聊天记录摘要，供意图识别做上下文。
	RecentMessages(n int) []ChatLogEntry

	// —— agent 会话工具（微 harness 一期扩展）——
	// 编号 id 均为 list_agents 顺序号（1..N）的字符串形式。

	// ListAgents 列出全部可见会话（活跃在前、磁盘补充），编号 1..N。
	ListAgents() ([]AgentView, error)
	// GetAgentStatus 查单个会话状态；recent>0 时附该会话最近 recent 条消息。
	GetAgentStatus(id string, recent int) (*AgentStatusView, error)
	// CreateAgent 新建会话：workspace 按名称/路径模糊匹配（空=默认工作区），
	// tool/model 空走宿主默认。返回新会话视图（含编号）。
	CreateAgent(workspace, tool, model string) (AgentView, error)
	// SendToAgent 投递消息到指定会话；未激活的磁盘会话先激活再投。
	// 返回目标会话人话标签；编号无法解析或不可投递时返回错误。
	SendToAgent(id, text string) (string, error)
	// StopAgent 停止指定会话当前轮次（mutating，确认门禁前置）。
	StopAgent(id string) error
}
