package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/yangk/kshell/internal/acp"
)

// elicitationSchema 只取表单需要的子集：顶层 properties + required。
type elicitationSchema struct {
	Type       string                     `json:"type"`
	Properties map[string]json.RawMessage `json:"properties"`
	Required   []string                   `json:"required"`
}

// elicitationProp 是单个属性的描述；oneOf 形态见 elicitationOneOf。
// Enum/Const 用 any：agent 可能给数字或布尔（如 true/false 二选一），
// 固定成 []string 会让整个属性解码失败、字段从表单里消失。
type elicitationProp struct {
	Type        string             `json:"type"`
	Title       string             `json:"title"`
	Description string             `json:"description"`
	Enum        []any              `json:"enum"`
	OneOf       []elicitationOneOf `json:"oneOf"`
	Items       *elicitationProp   `json:"items"`
	Default     any                `json:"default"`
}

// elicitationOneOf 是 oneOf 的枚举项（{const,title} 形态，const 可能是非字符串）。
type elicitationOneOf struct {
	Const any    `json:"const"`
	Title string `json:"title"`
}

// parseElicitationFields 把 requestedSchema 拍平成前端可渲染的字段列表。
// 字段按属性名排序，保证同一请求多次解析的顺序稳定（前端 diff 不抖动）。
func parseElicitationFields(raw json.RawMessage) []ElicitationField {
	out := []ElicitationField{}
	if len(raw) == 0 {
		return out
	}
	var schema elicitationSchema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return out
	}
	required := make(map[string]bool, len(schema.Required))
	for _, k := range schema.Required {
		required[k] = true
	}
	keys := make([]string, 0, len(schema.Properties))
	for k := range schema.Properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		var p elicitationProp
		if err := json.Unmarshal(schema.Properties[k], &p); err != nil {
			// 单个属性形态异常不该拖垮整张表单：降级成文本输入，用户仍可作答。
			out = append(out, ElicitationField{Key: k, Kind: "text", Required: required[k]})
			continue
		}
		opts := optionsFromSchema(&p)
		out = append(out, ElicitationField{
			Key:         k,
			Kind:        elicitationKind(&p, opts),
			Title:       p.Title,
			Description: p.Description,
			Required:    required[k],
			Options:     opts,
			Default:     p.Default,
		})
	}
	return out
}

// elicitationKind 把 JSON Schema 的 type 映射成前端控件类型。数组按多选处理；
// 带有效枚举的标量（含 number/integer/boolean）按单选处理，其余按 type 直译。
func elicitationKind(p *elicitationProp, opts []ElicitationOption) string {
	if p.Type == "array" {
		return "multiselect"
	}
	if len(opts) > 0 {
		return "select"
	}
	switch p.Type {
	case "boolean":
		return "boolean"
	case "number":
		return "number"
	case "integer":
		return "integer"
	}
	return "text"
}

// optionsFromSchema 取可选项：数组取 items 的枚举，标量取自身枚举；值为数字/
// 布尔时用 fmt.Sprint 归一成字符串（ACP content 的 key 只能是字符串）。
// oneOf 的 title 缺失时退回 const，保证下拉永远有可读文案；nil 与空串 const
// 无法作为有效取值，直接跳过。
func optionsFromSchema(p *elicitationProp) []ElicitationOption {
	src := p
	if p.Type == "array" && p.Items != nil {
		src = p.Items
	}
	out := make([]ElicitationOption, 0, len(src.Enum)+len(src.OneOf))
	for _, e := range src.Enum {
		if v, ok := optionText(e); ok {
			out = append(out, ElicitationOption{Value: v, Title: v})
		}
	}
	for _, o := range src.OneOf {
		v, ok := optionText(o.Const)
		if !ok {
			continue
		}
		title := o.Title
		if title == "" {
			title = v
		}
		out = append(out, ElicitationOption{Value: v, Title: title})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// optionText 把枚举值归一成非空字符串；nil 与空串不构成有效选项。
func optionText(v any) (string, bool) {
	if v == nil {
		return "", false
	}
	s := fmt.Sprint(v)
	if s == "" {
		return "", false
	}
	return s, true
}

// awaitElicitation 转发 elicitation 表单给宿主并等用户作答；会话不存在、
// 已退出、非 form 形态或宿主未注册回调时一律回 cancel，避免 agent 的反向
// 请求悬挂（与 ACP「不支持即 cancel」的回退语义一致）。
func (m *Manager) awaitElicitation(ctx context.Context, id, requestID string, p acp.ElicitationParams) (acp.ElicitationResult, error) {
	if p.Mode != "form" {
		return acp.ElicitationResult{Action: "cancel"}, nil
	}
	ch := make(chan elicitationResult, 1)
	m.mu.Lock()
	s := m.byID[id]
	if s == nil || s.exited {
		m.mu.Unlock()
		return acp.ElicitationResult{Action: "cancel"}, nil
	}
	onElicit := m.onElicitation
	// 没有宿主回调就没人能作答：不登记、不等待，直接回 cancel。
	if onElicit == nil {
		m.mu.Unlock()
		return acp.ElicitationResult{Action: "cancel"}, nil
	}
	// 先登记通道再回调：回调里前端可能立刻作答，晚登记会丢应答。
	s.pendingElicit[requestID] = ch
	m.mu.Unlock()

	req := ElicitationRequest{
		RequestID:  requestID,
		SessionID:  p.SessionID,
		ToolCallID: p.ToolCallID,
		Message:    p.Message,
		Fields:     parseElicitationFields(p.RequestedSchema),
	}
	// 登记后到回调前会话可能刚好退出（退出路径会取消挂起项）：再确认一次，
	// 避免把注定无人作答的表单推给前端。
	m.mu.Lock()
	exited := s.exited
	if exited {
		delete(s.pendingElicit, requestID)
	}
	m.mu.Unlock()
	if exited {
		return acp.ElicitationResult{Action: "cancel"}, nil
	}
	// 回调在锁外：desktop 层要 emit 到前端，不能持锁阻塞其他会话。
	onElicit(id, req)

	select {
	case r := <-ch:
		switch r.action {
		case "accept":
			return acp.ElicitationResult{Action: "accept", Content: r.content}, nil
		case "decline":
			return acp.ElicitationResult{Action: "decline"}, nil
		}
		return acp.ElicitationResult{Action: "cancel"}, nil
	case <-ctx.Done():
		// ctx 取消后必须摘除等待项，否则后续 Respond 会向无接收方的通道投递。
		m.mu.Lock()
		delete(s.pendingElicit, requestID)
		m.mu.Unlock()
		return acp.ElicitationResult{Action: "cancel"}, nil
	}
}

// RespondElicitation 提交用户对 elicitation 表单的作答；非 accept 时丢弃
// content（语义上放弃/取消不该携带已填数据）。
func (m *Manager) RespondElicitation(id, requestID, action string, content map[string]any) error {
	switch action {
	case "accept", "decline", "cancel":
	default:
		return fmt.Errorf("err.chat.bad_elicitation_action|%s", action)
	}
	if action != "accept" {
		content = nil
	}
	return m.resolveElicitation(id, requestID, elicitationResult{action: action, content: content})
}

// CancelElicitation 让等待中的表单请求以 cancel 结束。
func (m *Manager) CancelElicitation(id, requestID string) error {
	return m.resolveElicitation(id, requestID, elicitationResult{action: "cancel"})
}

// resolveElicitation 摘除等待项并投递结果，镜像 resolvePermission 的锁模式。
func (m *Manager) resolveElicitation(id, requestID string, r elicitationResult) error {
	m.mu.Lock()
	s := m.byID[id]
	if s == nil {
		m.mu.Unlock()
		return errNotFound
	}
	ch := s.pendingElicit[requestID]
	delete(s.pendingElicit, requestID)
	m.mu.Unlock()
	if ch == nil {
		return errNotFound
	}
	ch <- r
	return nil
}

// cancelPendingElicitations 非阻塞地让所有等待中的表单请求以 cancel 结束。
func cancelPendingElicitations(pending map[string]chan elicitationResult) {
	for _, ch := range pending {
		select {
		case ch <- elicitationResult{action: "cancel"}:
		default:
		}
	}
}
