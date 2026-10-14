package acp

import "encoding/json"

// Spec 描述如何启动 ACP agent 子进程（已完成平台 shim 解析）。
type Spec struct {
	Path string
	Args []string
	Dir  string
	Env  []string
}

// Implementation 是 initialize 的 clientInfo/agentInfo。
type Implementation struct {
	Name    string `json:"name,omitempty"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version,omitempty"`
}

type ClientCapabilities struct{} // v1 不声明任何可选能力

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

// ConfigOptionValue / SessionConfigOption 是新式会话配置选项的最小解析面：
// 只关心 id=model 的 select 选项（模型切换的另一种 agent 实现路径）。
type ConfigOptionValue struct {
	Value       string `json:"value"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type SessionConfigOption struct {
	ID           string              `json:"id"`
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
type McpServerStdio struct {
	Type    string        `json:"type"`
	Name    string        `json:"name"`
	Command string        `json:"command"`
	Args    []string      `json:"args"`
	Env     []EnvVariable `json:"env"`
}

type NewSessionParams struct {
	Cwd        string `json:"cwd"`
	McpServers []any  `json:"mcpServers"`
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
	// ModelConfig 非 nil 表示 agent 以 configOptions 形态报告了 id=model 的
	// 选项（新式模型切换路径），选项值列表与当前值都在里面。
	ModelConfig *SessionConfigOption
}

type LoadSessionParams struct {
	SessionID  string `json:"sessionId"`
	Cwd        string `json:"cwd"`
	McpServers []any  `json:"mcpServers"`
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
type SetConfigOptionParams struct {
	SessionID string          `json:"sessionId"`
	OptionID  string          `json:"optionId"`
	Value     json.RawMessage `json:"value"`
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
