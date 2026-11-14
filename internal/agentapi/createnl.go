package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yangk/kshell/internal/hubtools"
)

// NLCreate 是自然语言创建的补充参数：Confirm 非 nil 时跳过 LLM 解析，
// 直接按用户已确认的选择走校验与创建。
type NLCreate struct {
	Confirm *CreatePlan
}

// CreatePlan 是一次创建计划的完整描述：LLM 解析产物或用户确认后的选择。
// ToolOptions/WorkspaceOptions 仅在 NeedsConfirm 时填充（多候选让调用方
// 展示给用户挑选）。
type CreatePlan struct {
	ToolID           string
	Workspace        string
	Model            string
	Task             string // 创建成功后投给新会话的首条任务（可空）
	Question         string // NeedsConfirm 时给调用方的反问
	ToolOptions      []Option
	WorkspaceOptions []Option
}

// CreateResult 是自然语言创建的结果：要么已创建，要么需要调用方跟用户确认。
type CreateResult struct {
	Created      *Session
	NeedsConfirm *CreatePlan
}

// CreateNL 根据自然语言创建会话：LLM 解析出工具/工作区/模型/首个任务，
// 代码侧校验候选。LLM 未就绪返回 ErrLLMNotReady；解析失败返回错误（不静默
// 回落）；工具不存在或工作区歧义返回 NeedsConfirm，调用方确认后带
// NLCreate.Confirm 重发。
func (a *API) CreateNL(ctx context.Context, text string, opts NLCreate) (CreateResult, error) {
	cfg := a.llmCfg()
	if !hubtools.Ready(cfg) {
		return CreateResult{}, ErrLLMNotReady
	}
	plan := CreatePlan{}
	if opts.Confirm != nil {
		plan = *opts.Confirm // 用户已确认：不再问模型
	} else {
		parsed, err := a.parseNL(ctx, text)
		if err != nil {
			return CreateResult{}, err
		}
		plan = parsed
	}
	return a.validateAndCreate(ctx, plan)
}

// parseNL 调 LLM 解析用户文本为创建计划。
func (a *API) parseNL(ctx context.Context, text string) (CreatePlan, error) {
	tools := a.b.Tools()
	workspaces := a.b.Workspaces()
	var b strings.Builder
	b.WriteString("你是 kshell 的会话创建解析器：根据用户输入，从候选中选出工具与工作区，并提取要交给新会话的首个任务描述。\n")
	b.WriteString("只输出一个 JSON 对象，不要输出解释或 Markdown 代码块。\n\n")
	b.WriteString("输出 JSON 契约：\n")
	b.WriteString(`{"tool":"","workspace":"","model":"","task":"","question":"","ready":false}` + "\n")
	b.WriteString("- tool：候选工具 ID 之一；用户未明确指定时留空。\n")
	b.WriteString("- workspace：候选工作区路径之一；用户未明确指定时留空。\n")
	b.WriteString("- model：用户明确要求的模型名；未要求留空。\n")
	b.WriteString("- task：新会话要执行的首个任务（可按语境改写）；纯闲聊建会话则为空。\n")
	b.WriteString("- question：ready=false 时向用户提出的具体反问。\n")
	b.WriteString("- ready：tool 与 workspace 都已确定时为 true。\n\n")
	b.WriteString("候选工具（ID 名称）：\n")
	for _, t := range tools {
		b.WriteString("- " + t.ID + " " + t.Label + "\n")
	}
	b.WriteString("候选工作区（路径 名称）：\n")
	for _, w := range workspaces {
		b.WriteString("- " + w.ID + " " + w.Label + "\n")
	}

	resp, err := a.chat(ctx, []hubtools.Message{
		{Role: "system", Content: b.String()},
		{Role: "user", Content: text},
	})
	if err != nil {
		return CreatePlan{}, err
	}
	i := strings.Index(resp.Text, "{")
	if i < 0 {
		return CreatePlan{}, fmt.Errorf("创建解析响应不含 JSON，模型原文: %.200s", resp.Text)
	}
	var parsed struct {
		Tool      string `json:"tool"`
		Workspace string `json:"workspace"`
		Model     string `json:"model"`
		Task      string `json:"task"`
		Question  string `json:"question"`
		Ready     bool   `json:"ready"`
	}
	// 仿 ParseRouteDecision：从首个 { 起流式解码，容忍前后夹杂的自然语言。
	if err := json.NewDecoder(strings.NewReader(resp.Text[i:])).Decode(&parsed); err != nil {
		return CreatePlan{}, fmt.Errorf("解析创建 JSON: %w", err)
	}
	return CreatePlan{
		ToolID:    strings.TrimSpace(parsed.Tool),
		Workspace: strings.TrimSpace(parsed.Workspace),
		Model:     strings.TrimSpace(parsed.Model),
		Task:      strings.TrimSpace(parsed.Task),
		Question:  strings.TrimSpace(parsed.Question),
	}, nil
}

// validateAndCreate 校验计划并执行创建：工具必须存在；工作区精确命中或
// 小写包含唯一命中；未定/歧义返回 NeedsConfirm。创建成功且 Task 非空时
// 立即投递首条任务——投递失败时错误上抛，但 Created 仍有效（会话已存在，
// 调用方可据此继续盯输出或重投）。
func (a *API) validateAndCreate(ctx context.Context, plan CreatePlan) (CreateResult, error) {
	tools := a.b.Tools()
	workspaces := a.b.Workspaces()

	need := CreatePlan{Model: plan.Model, Task: plan.Task}

	// 工具：ID 必须精确命中候选。
	if plan.ToolID == "" {
		need.Question = strings.TrimSpace(plan.Question + " 请选择要使用的工具。")
		need.ToolOptions = tools
		return CreateResult{NeedsConfirm: &need}, nil
	}
	found := false
	for _, t := range tools {
		if t.ID == plan.ToolID {
			found = true
			break
		}
	}
	if !found {
		need.Question = fmt.Sprintf("工具 %q 不存在，请从候选中选择。", plan.ToolID)
		need.ToolOptions = tools
		return CreateResult{NeedsConfirm: &need}, nil
	}

	// 工作区：精确路径命中；否则小写包含唯一命中自动采用，歧义/零命中进确认。
	ws, ok := matchWorkspace(plan.Workspace, workspaces)
	if !ok {
		need.ToolID = plan.ToolID
		need.Question = strings.TrimSpace(plan.Question + " 请选择工作区。")
		need.WorkspaceOptions = capOptions(workspaces, 10)
		return CreateResult{NeedsConfirm: &need}, nil
	}

	s, err := a.b.Create(plan.ToolID, ws, plan.Model)
	if err != nil {
		return CreateResult{}, err
	}
	if plan.Task != "" {
		if err := a.b.Send(s.ID, plan.Task); err != nil {
			// 会话已创建：错误与 Created 同时返回，调用方按 Created != nil 区分。
			return CreateResult{Created: &s}, fmt.Errorf("投递首条任务: %w", err)
		}
	}
	return CreateResult{Created: &s}, nil
}

// matchWorkspace 按精确路径 → 唯一小写包含命中两级匹配工作区。
func matchWorkspace(ws string, candidates []Option) (string, bool) {
	ws = strings.TrimSpace(ws)
	if ws == "" {
		return "", false
	}
	for _, w := range candidates {
		if w.ID == ws {
			return ws, true
		}
	}
	lower := strings.ToLower(ws)
	var hits []string
	for _, w := range candidates {
		if strings.Contains(strings.ToLower(w.ID), lower) {
			hits = append(hits, w.ID)
		}
	}
	if len(hits) == 1 {
		return hits[0], true
	}
	return "", false
}

// capOptions 截断候选列表（展示给用户的选择不应无限长）。
func capOptions(opts []Option, n int) []Option {
	if len(opts) > n {
		return opts[:n]
	}
	return opts
}
