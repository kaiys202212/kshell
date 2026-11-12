package hubtools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrConfirmationRequired 是 Mutating 工具的挂起哨兵：本任务不直接执行写操作，
// 统一返回该错误并包装 *PendingConfirm，由确认状态机（confirm.go，下一任务）
// 接管用户确认后再执行。
var ErrConfirmationRequired = errors.New("mutating tool requires user confirmation")

// PendingConfirm 携带一次待确认的写操作：确认后按 ToolName+ArgsJSON 重放执行。
// 实现 error 并 Unwrap 到 ErrConfirmationRequired，调用方可用 errors.Is/As 判别。
type PendingConfirm struct {
	PendingID string // 确认状态机分配的挂起标识（Registry.Execute 填空，Session.Execute 填充）
	ToolName  string // 工具名
	ArgsJSON  string // 原始参数 JSON（重放时用）
	Summary   string // 人话参数摘要，展示给用户确认用
}

func (p *PendingConfirm) Error() string {
	return ErrConfirmationRequired.Error() + ": " + p.Summary
}

func (p *PendingConfirm) Unwrap() error { return ErrConfirmationRequired }

// mutatingTools 是写工具集合：调用即挂起等待确认。stop_agent 停掉 agent
// 正在跑的轮次属破坏性；create_agent/send_to_agent 直接执行（创建可逆、
// 投递即既有 dispatch 语义，挂确认会把「新建对话」拆成两步）。
var mutatingTools = map[string]bool{
	"create_requirement": true,
	"update_requirement": true,
	"delete_requirement": true,
	"stop_agent":         true,
}

// Registry 持有 Bridge 并对外暴露工具 schema 与执行入口。
type Registry struct {
	b        Bridge
	mutating map[string]bool // 暴露给测试断言的 Mutating 元数据（mutatingTools 拷贝）
}

// NewRegistry 创建注册表，注册一期 9 个工具。
func NewRegistry(b Bridge) *Registry {
	m := make(map[string]bool, len(mutatingTools))
	for k, v := range mutatingTools {
		m[k] = v
	}
	return &Registry{b: b, mutating: m}
}

// schema 构造 JSON Schema 的便捷函数。
func schema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

// strProp 构造字符串属性。
func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

// Tools 返回给 LLM 的工具 schema 列表（顺序固定，便于 diff 与缓存）。
func (r *Registry) Tools() []ToolDef {
	return []ToolDef{
		{
			Name:        "list_requirements",
			Description: "列出需求条目（最新在前），可用 project 过滤某个项目的需求。",
			Parameters: schema(map[string]any{
				"project": strProp("可选。按所属项目（工作区路径）过滤"),
			}),
		},
		{
			Name:        "get_requirement",
			Description: "按 ID 查看单条需求详情。",
			Parameters: schema(map[string]any{
				"id": strProp("需求 ID，必填"),
			}, "id"),
		},
		{
			Name:        "create_requirement",
			Description: "新建一条需求。写操作，需要用户确认后执行。",
			Parameters: schema(map[string]any{
				"project":  strProp("所属项目（工作区路径），必填"),
				"text":     strProp("需求描述，必填"),
				"priority": strProp("可选。优先级 high/medium/low，缺省 medium"),
				"tags": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "可选。自由标签列表",
				},
			}, "project", "text"),
		},
		{
			Name:        "update_requirement",
			Description: "修改一条需求，未提供的可选字段保持不变。写操作，需要用户确认后执行。",
			Parameters: schema(map[string]any{
				"id":       strProp("需求 ID，必填"),
				"text":     strProp("新的需求描述，必填"),
				"project":  strProp("可选。新的所属项目（工作区路径）"),
				"priority": strProp("可选。优先级 high/medium/low"),
				"tags": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "可选。新的标签列表（整体替换）",
				},
			}, "id", "text"),
		},
		{
			Name:        "delete_requirement",
			Description: "删除一条需求（幂等）。写操作，需要用户确认后执行。",
			Parameters: schema(map[string]any{
				"id": strProp("需求 ID，必填"),
			}, "id"),
		},
		{
			Name:        "list_workspaces",
			Description: "列出本机全部工作区（含名称、路径、各工具会话数、本地/ssh 类型）。",
			Parameters:  schema(map[string]any{}),
		},
		{
			Name:        "get_workspace",
			Description: "按 ID 查看单个工作区详情。",
			Parameters: schema(map[string]any{
				"id": strProp("工作区 ID，必填"),
			}, "id"),
		},
		{
			Name:        "list_agents",
			Description: "列出全部 agent 会话（活跃在前，编号 1..N）：工具、工作区、标题、状态（running=运行中 perm=等待授权 idle=空闲 gone=不可用）与最后一条消息摘要。投递/停止/查状态都引用这里的编号。",
			Parameters:  schema(map[string]any{}),
		},
		{
			Name:        "get_agent_status",
			Description: "查单个 agent 会话的实时状态，可附最近几条消息看它在干什么。",
			Parameters: schema(map[string]any{
				"id":     strProp("会话编号，取 list_agents 的 num，必填"),
				"recent": map[string]any{"type": "integer", "description": "可选。附该会话最近 n 条消息（0-20，0=不附）"},
			}, "id"),
		},
		{
			Name:        "create_agent",
			Description: "新建一个 agent 会话（立即执行，无需确认）。workspace 按名称或路径匹配 list_workspaces，匹配不到时先用自然语言向用户确认项目，不要猜；tool/model 可省略走默认。",
			Parameters: schema(map[string]any{
				"workspace": strProp("可选。工作区名称或路径（匹配 list_workspaces）；省略=默认工作区"),
				"tool":      strProp("可选。agent 工具 ID（如 claude/codex/gemini）；省略=默认工具"),
				"model":     strProp("可选。该会话使用的模型；省略=工具默认"),
			}),
		},
		{
			Name:        "send_to_agent",
			Description: "把消息/任务投递到指定 agent 会话执行（立即执行，无需确认）。未激活的磁盘会话会先自动激活。",
			Parameters: schema(map[string]any{
				"id":   strProp("目标会话编号，取 list_agents 的 num，必填"),
				"text": strProp("投递的消息正文，必填"),
			}, "id", "text"),
		},
		{
			Name:        "stop_agent",
			Description: "停止指定 agent 会话当前正在执行的轮次。写操作，需要用户确认后执行。",
			Parameters: schema(map[string]any{
				"id": strProp("目标会话编号，取 list_agents 的 num，必填"),
			}, "id"),
		},
	}
}

// ExecuteConfirmed 是确认状态机的直接执行通道：绕过 Mutating 挂起，
// 只接受写工具（只读工具走 Execute，无必要经此通道），供用户确认后重放。
func (r *Registry) ExecuteConfirmed(ctx context.Context, name, argsJSON string) (string, error) {
	if !mutatingTools[name] {
		return "", fmt.Errorf("ExecuteConfirmed 只接受写工具, got: %s", name)
	}
	return r.executeMutating(ctx, name, argsJSON)
}

// Execute 解析 argsJSON 后调用 Bridge，把结果 JSON 序列化为文本返回给 LLM。
// Mutating 工具不执行，返回包装 *PendingConfirm 的 ErrConfirmationRequired。
func (r *Registry) Execute(ctx context.Context, name, argsJSON string) (string, error) {
	if !mutatingTools[name] && r.findTool(name) == "" {
		return "", fmt.Errorf("未知工具: %s", name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("参数不是合法 JSON: %w", err)
	}
	if mutatingTools[name] {
		// stop_agent 是破坏性操作：挂起时即用当前列表快照把编号钉成 chatId，
		// 确认重放按 chatId 执行——挂起到用户点确认之间隔着一次完整 LLM 回合，
		// 会话列表可能增删，重放时再按编号解析可能静默停错会话。
		if name == "stop_agent" {
			pinned, perr := r.pinStopAgent(args)
			if perr != nil {
				return "", perr
			}
			argsJSON = pinned
		}
		return "", &PendingConfirm{ToolName: name, ArgsJSON: argsJSON, Summary: r.summarize(name, args)}
	}

	var out any
	var err error
	switch name {
	case "list_requirements":
		var reqs []RequirementView
		reqs, err = r.b.ListRequirements()
		if err == nil {
			if project, _ := args["project"].(string); project != "" {
				filtered := make([]RequirementView, 0, len(reqs))
				for _, it := range reqs {
					if it.Project == project {
						filtered = append(filtered, it)
					}
				}
				reqs = filtered
			}
			out = reqs
		}
	case "get_requirement":
		out, err = r.b.GetRequirement(argStr(args, "id"))
		if err != nil {
			break
		}
		if out == nil {
			return "", errors.New("需求不存在")
		}
	case "list_workspaces":
		out, err = r.b.ListWorkspaces()
	case "get_workspace":
		out, err = r.b.GetWorkspace(argStr(args, "id"))
		if err != nil {
			break
		}
		if out == nil {
			return "", errors.New("工作区不存在")
		}
	case "list_agents":
		out, err = r.b.ListAgents()
	case "get_agent_status":
		v, serr := r.b.GetAgentStatus(argStr(args, "id"), argInt(args, "recent"))
		if serr != nil {
			return "", serr
		}
		if v == nil {
			return "", errors.New("会话不存在")
		}
		out = v
	case "create_agent":
		v, cerr := r.b.CreateAgent(argStr(args, "workspace"), argStr(args, "tool"), argStr(args, "model"))
		if cerr != nil {
			return "", cerr
		}
		out = v
	case "send_to_agent":
		label, derr := r.b.SendToAgent(argStr(args, "id"), argStr(args, "text"))
		if derr != nil {
			return "", derr
		}
		out = map[string]string{"chat": label}
	default:
		return "", fmt.Errorf("未知工具: %s", name)
	}
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("序列化结果: %w", err)
	}
	return string(data), nil
}

// executeMutating 真正执行写工具：解析参数后调用 Bridge 并序列化结果。
// 由 ExecuteConfirmed（确认后重放）调用，Execute 的 Mutating 分支不经过这里。
func (r *Registry) executeMutating(ctx context.Context, name, argsJSON string) (string, error) {
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("参数不是合法 JSON: %w", err)
	}
	var out any
	var err error
	switch name {
	case "create_requirement":
		tags, _ := args["tags"].([]any)
		tagList := make([]string, 0, len(tags))
		for _, t := range tags {
			if s, ok := t.(string); ok {
				tagList = append(tagList, s)
			}
		}
		out, err = r.b.CreateRequirement(argStr(args, "project"), argStr(args, "text"), argStr(args, "priority"), tagList)
	case "update_requirement":
		tags, _ := args["tags"].([]any)
		tagList := make([]string, 0, len(tags))
		for _, t := range tags {
			if s, ok := t.(string); ok {
				tagList = append(tagList, s)
			}
		}
		out, err = r.b.UpdateRequirement(argStr(args, "id"), argStr(args, "project"), argStr(args, "text"), argStr(args, "priority"), tagList)
	case "delete_requirement":
		err = r.b.DeleteRequirement(argStr(args, "id"))
		if err == nil {
			out = map[string]string{"ok": "true", "id": argStr(args, "id")}
		}
	case "stop_agent":
		// chatId 是挂起时钉住的快照（pinStopAgent 注入）：优先按它执行，
		// 规避挂起窗口内编号漂移；空（快照缺失）退回按编号现解析。
		err = r.b.StopAgent(argStr(args, "id"), argStr(args, "chatId"))
		if err == nil {
			out = map[string]string{"ok": "true"}
		}
	default:
		return "", fmt.Errorf("未知写工具: %s", name)
	}
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("序列化结果: %w", err)
	}
	return string(data), nil
}

// pinStopAgent 在 stop_agent 挂起前把编号解析成当前列表的 chatId 并写入
// args（重序列化为新的 argsJSON）。编号不在列表内或会话未激活时报错——
// 不给用户一个注定停不下来的确认卡。
func (r *Registry) pinStopAgent(args map[string]any) (string, error) {
	agents, err := r.b.ListAgents()
	if err != nil {
		return "", err
	}
	n := argInt(args, "id")
	for _, v := range agents {
		if v.Num == n {
			if v.ChatID == "" {
				return "", fmt.Errorf("会话 #%d 未激活，无需停止", n)
			}
			args["chatId"] = v.ChatID
			out, merr := json.Marshal(args)
			if merr != nil {
				return "", fmt.Errorf("序列化挂起参数: %w", merr)
			}
			return string(out), nil
		}
	}
	return "", fmt.Errorf("会话编号 %d 不在当前列表内", n)
}

// findTool 查工具名是否存在（返回原名或空串）。
func (r *Registry) findTool(name string) string {
	for _, td := range r.Tools() {
		if td.Name == name {
			return td.Name
		}
	}
	return ""
}

// argStr 取字符串参数（缺省/类型不符返回空串）。
func argStr(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return s
}

// argInt 取整数参数：兼容模型传 "5"（字符串）与 5（数值）两种形态，缺省返回 0。
func argInt(args map[string]any, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}

// summarize 生成写操作的人话参数摘要，供确认 UI 展示。
func (r *Registry) summarize(name string, args map[string]any) string {
	switch name {
	case "create_requirement":
		return fmt.Sprintf("新建需求（项目 %s）：%s", argStr(args, "project"), argStr(args, "text"))
	case "update_requirement":
		return fmt.Sprintf("修改需求 #%s：%s", argStr(args, "id"), argStr(args, "text"))
	case "delete_requirement":
		return fmt.Sprintf("删除需求 #%s", argStr(args, "id"))
	case "stop_agent":
		return fmt.Sprintf("停止会话 #%s 当前轮次", argStr(args, "id"))
	}
	return name
}
