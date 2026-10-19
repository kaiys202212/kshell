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

// TaskView 对齐 internal/chat.Info 的会话列表字段精简：
// 模型关注哪个会话在哪个工作区跑什么工具、什么状态。
type TaskView struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	ToolID    string `json:"toolId,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Status    string `json:"status"`
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

// Bridge 提供宿主能力查询与需求写入（写操作由确认门禁前置把关）。
type Bridge interface {
	ListRequirements() ([]RequirementView, error)
	GetRequirement(id string) (*RequirementView, error)
	CreateRequirement(project, text, priority string, tags []string) (RequirementView, error)
	UpdateRequirement(id, project, text, priority string, tags []string) (RequirementView, error)
	DeleteRequirement(id string) error
	ListWorkspaces() ([]WorkspaceView, error)
	GetWorkspace(id string) (*WorkspaceView, error)
	ListRunningTasks() ([]TaskView, error)
	GetTaskDetail(id string) (*TaskView, error)
	// RecentMessages 返回最近 n 条纯文本聊天记录摘要，供意图识别做上下文。
	RecentMessages(n int) []ChatLogEntry
	// DispatchTask 把任务派发到 ref 对应的聊天会话（ref 由本轮提示词里的
	// 记录编号定义），返回目标会话人话标签；ref 无法解析时返回错误。
	DispatchTask(ref, task string) (string, error)
}
