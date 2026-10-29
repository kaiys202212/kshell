package acp

import "encoding/json"

// Spec 描述如何启动 ACP agent 子进程（已完成平台 shim 解析）。
type Spec struct {
	Path string
	Args []string
	Dir  string
	Env  []string
	// Label 是进程退出日志的归属标记（聊天ID/工具ID），由 chat 层填入；
	// 空则日志无归属。仅用于诊断，不参与协议。
	Label string
}

// Implementation 是 initialize 的 clientInfo/agentInfo。
type Implementation struct {
	Name    string `json:"name,omitempty"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version,omitempty"`
}

// ElicitationCapabilities 声明客户端支持的 elicitation 形态；
// 目前只实现 form（结构化表单），url 等形态不声明。
type ElicitationCapabilities struct {
	Form *struct{} `json:"form,omitempty"`
}

type ClientCapabilities struct {
	Elicitation *ElicitationCapabilities `json:"elicitation,omitempty"`
}

type InitializeParams struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	ClientCapabilities ClientCapabilities `json:"clientCapabilities"`
	ClientInfo         Implementation     `json:"clientInfo"`
}

type PromptCapabilities struct {
	Image           bool `json:"image"`
	Audio           bool `json:"audio"`
	EmbeddedContext bool `json:"embeddedContext"`
}

type AgentCapabilities struct {
	LoadSession        bool               `json:"loadSession"`
	PromptCapabilities PromptCapabilities `json:"promptCapabilities"`
}

type AuthMethod struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AuthenticateParams 是 authenticate 请求的参数：methodId 取自 initialize
// 广播的 authMethods 列表。
type AuthenticateParams struct {
	MethodID string `json:"methodId"`
}

type InitializeResult struct {
	ProtocolVersion   int               `json:"protocolVersion"`
	AgentCapabilities AgentCapabilities `json:"agentCapabilities"`
	AgentInfo         Implementation    `json:"agentInfo"`
	AuthMethods       []AuthMethod      `json:"authMethods"`
}

// ContentBlock 是 session/prompt 的内容块。按 ACP 规范支持 text / image /
// resource_link / resource 等形态：image 用 Data(base64)+MimeType，
// resource_link 用 URI+Name，resource 用 Resource。字段全 omitempty，
// 纯文本路径的序列化与旧版完全一致。
type ContentBlock struct {
	Type     string            `json:"type"`
	Text     string            `json:"text,omitempty"`
	Data     string            `json:"data,omitempty"`
	MimeType string            `json:"mimeType,omitempty"`
	URI      string            `json:"uri,omitempty"`
	Name     string            `json:"name,omitempty"`
	Resource *ResourceContents `json:"resource,omitempty"`
}

// ResourceContents 是内嵌资源的内容体：文本用 Text，二进制用 Blob(base64)。
type ResourceContents struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

// ModelInfo / SessionModelState 是 session/new、session/load 响应里
// v1 形态的可用模型列表（部分 agent 以 configOptions 形态报告，见下）。
type ModelInfo struct {
	ModelID     string `json:"modelId"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type SessionModelState struct {
	AvailableModels []ModelInfo `json:"availableModels"`
	CurrentModelID  string      `json:"currentModelId"`
}

// ConfigOptionValue / SessionConfigOption 是新式会话配置选项的解析面：
// 既覆盖 id=model 的模型切换路径，也覆盖 OpenCode 的 mode 等其它 select 选项。
type ConfigOptionValue struct {
	Value       string `json:"value"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type SessionConfigOption struct {
	ID           string              `json:"id"`
	Name         string              `json:"name,omitempty"`
	Description  string              `json:"description,omitempty"`
	Category     string              `json:"category,omitempty"`
	Type         string              `json:"type,omitempty"`
	CurrentValue json.RawMessage     `json:"currentValue,omitempty"`
	Options      []ConfigOptionValue `json:"options,omitempty"`
}

// EnvVariable 是 MCP stdio 服务器的环境变量项（ACP 要求 name/value，不能用 map）。
type EnvVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// McpServerStdio 是 session/new 注入的 stdio MCP 服务器。
// Type 必须 omitempty：claude-agent-acp 按「"type" in server」区分形态，
// 带 type（含空串）一律当 HTTP server 处理并丢弃 command/args，
// claude CLI 拿到无命令的 server 会在 session/new 阶段退出（-32603）。
type McpServerStdio struct {
	Type    string        `json:"type,omitempty"`
	Name    string        `json:"name"`
	Command string        `json:"command"`
	Args    []string      `json:"args"`
	Env     []EnvVariable `json:"env"`
}

// NewSessionParams 的 Meta 携带 agent 专属扩展（ACP _meta 位）：
// claude-agent-acp 只从 params._meta.claudeCode.options 读 SDK options，
// kshell 经它传 settings（模型 env 注入）——适配器 argv 形态的 --settings
// 会被静默丢弃。空值 omitempty，对不认识 _meta 的 agent 零噪音。
type NewSessionParams struct {
	Cwd        string          `json:"cwd"`
	McpServers []any           `json:"mcpServers"`
	Meta       json.RawMessage `json:"_meta,omitempty"`
}

type NewSessionResult struct {
	SessionID     string                `json:"sessionId"`
	Models        *SessionModelState    `json:"models,omitempty"`
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
}

// SessionStart 是 new/load 会话后调用方需要的会话信息快照。
type SessionStart struct {
	SessionID string
	Models    *SessionModelState
	// ConfigOptions 保留 agent 报告的全部会话配置选项（model/mode 等），
	// 供 UI 渲染通用选择器，而非只识别 model。
	ConfigOptions []SessionConfigOption
	// ModelConfig 非 nil 表示 agent 以 configOptions 形态报告了 id=model 的
	// 选项（新式模型切换路径），选项值列表与当前值都在里面。
	ModelConfig *SessionConfigOption
}

type LoadSessionParams struct {
	SessionID  string          `json:"sessionId"`
	Cwd        string          `json:"cwd"`
	McpServers []any           `json:"mcpServers"`
	Meta       json.RawMessage `json:"_meta,omitempty"`
}

type PromptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

type PromptResult struct {
	StopReason string `json:"stopReason"`
}

type SetModelParams struct {
	SessionID string `json:"sessionId"`
	ModelID   string `json:"modelId"`
}

// SetConfigOptionParams 的 Value 按 agent 声明可能是字符串或其他 JSON 值，保留原样序列化。
// 字段名为 configId（而非 optionId），OpenCode 等实现按此严格校验。
type SetConfigOptionParams struct {
	SessionID string          `json:"sessionId"`
	ConfigID  string          `json:"configId"`
	Value     json.RawMessage `json:"value"`
}

// SetConfigOptionResult 返回设置后的完整配置状态（不是增量）。
type SetConfigOptionResult struct {
	ConfigOptions []SessionConfigOption `json:"configOptions"`
}

// ElicitationParams 是 agent 发来的 elicitation/create 反向请求参数。
// 目前只处理 mode=form；其余形态原样保留以便未来扩展。
type ElicitationParams struct {
	SessionID       string          `json:"sessionId,omitempty"`
	ToolCallID      string          `json:"toolCallId,omitempty"`
	RequestID       json.RawMessage `json:"requestId,omitempty"`
	Mode            string          `json:"mode"`
	Message         string          `json:"message"`
	RequestedSchema json.RawMessage `json:"requestedSchema,omitempty"`
	URL             string          `json:"url,omitempty"`
	ElicitationID   string          `json:"elicitationId,omitempty"`
}

// ElicitationResult 是客户端对 elicitation 的应答。
type ElicitationResult struct {
	Action  string         `json:"action"`
	Content map[string]any `json:"content,omitempty"`
}

type CancelParams struct {
	SessionID string `json:"sessionId"`
}

type ToolCallLocation struct {
	Path string `json:"path"`
	Line *int   `json:"line,omitempty"`
}

type PermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

// ToolCall 同时用于 request_permission 的 toolCall 与 session/update 的 tool_call。
type ToolCall struct {
	ToolCallID string             `json:"toolCallId"`
	Name       string             `json:"name,omitempty"`
	Title      string             `json:"title,omitempty"`
	Kind       string             `json:"kind,omitempty"`
	Status     string             `json:"status,omitempty"`
	Content    []json.RawMessage  `json:"content,omitempty"`
	Locations  []ToolCallLocation `json:"locations,omitempty"`
	RawInput   json.RawMessage    `json:"rawInput,omitempty"`
	RawOutput  json.RawMessage    `json:"rawOutput,omitempty"`
}

type RequestPermissionParams struct {
	SessionID string             `json:"sessionId"`
	ToolCall  ToolCall           `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}

type PermissionOutcome struct {
	Outcome  string `json:"outcome"` // "selected" | "cancelled"
	OptionID string `json:"optionId,omitempty"`
}

type RequestPermissionResult struct {
	Outcome PermissionOutcome `json:"outcome"`
}

// updateParams 是 session/update 的参数信封。
type updateParams struct {
	SessionID string          `json:"sessionId"`
	Update    json.RawMessage `json:"update"`
}

func decodeUpdateParams(raw json.RawMessage) (string, json.RawMessage, error) {
	var p updateParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", nil, err
	}
	return p.SessionID, p.Update, nil
}

func decodeRequestPermissionParams(raw json.RawMessage) (RequestPermissionParams, error) {
	var p RequestPermissionParams
	err := json.Unmarshal(raw, &p)
	return p, err
}
