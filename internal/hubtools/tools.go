package hubtools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// mutatingTools 是一期写工具集合：调用即挂起等待确认。
var mutatingTools = map[string]bool{
	"create_requirement": true,
	"update_requirement": true,
	"delete_requirement": true,
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
			Name:        "list_running_tasks",
			Description: "列出当前运行中的会话/任务（标题、工具、工作区、状态）。",
			Parameters:  schema(map[string]any{}),
		},
		{
			Name:        "get_task_detail",
			Description: "按 ID 查看单个运行中会话/任务的详情。",
			Parameters: schema(map[string]any{
				"id": strProp("任务（会话）ID，必填"),
			}, "id"),
		},
		{
			Name:        "dispatch_task",
			Description: "把一个明确任务派发到聊天记录中的某个 agent 会话执行（ref 为近期聊天记录里的编号）。立即执行，无需确认。",
			Parameters: schema(map[string]any{
				"ref":  strProp("目标会话编号，取近期聊天记录列表里的 [n]，必填"),
				"task": strProp("派发的任务描述，必填"),
			}, "ref", "task"),
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
	case "list_running_tasks":
		out, err = r.b.ListRunningTasks()
	case "get_task_detail":
		out, err = r.b.GetTaskDetail(argStr(args, "id"))
		if err != nil {
			break
		}
		if out == nil {
			return "", errors.New("任务不存在")
		}
	case "dispatch_task":
		label, derr := r.b.DispatchTask(argStr(args, "ref"), argStr(args, "task"))
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

// summarize 生成写操作的人话参数摘要，供确认 UI 展示。
func (r *Registry) summarize(name string, args map[string]any) string {
	switch name {
	case "create_requirement":
		return fmt.Sprintf("新建需求（项目 %s）：%s", argStr(args, "project"), argStr(args, "text"))
	case "update_requirement":
		return fmt.Sprintf("修改需求 #%s：%s", argStr(args, "id"), argStr(args, "text"))
	case "delete_requirement":
		return fmt.Sprintf("删除需求 #%s", argStr(args, "id"))
	}
	return name
}
