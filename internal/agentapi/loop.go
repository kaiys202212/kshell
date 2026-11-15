package agentapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/hubtools"
)

// loop.go：管理者循环——把 hubtools 的 function calling 协议与 Backend 原语
// 缝合成一个「会看会话、会发消息、会建会话、会反问」的自主 agent 循环。
//
// 循环级状态（busy/pending/memory）挂在 API 结构体字段上（mu 保护），见 api.go。

// SetMemory 注入管理者记忆 journal（desktop 启动时创建并注入；nil 关闭记忆）。
func (a *API) SetMemory(m *ManagerMemory) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mem = m
}

// memoryNow 测试白盒：取当前记忆实例。
func (a *API) memoryNow() *ManagerMemory {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.mem
}

// getPending / setPending / clearPending 是 pendingAsk 的互斥访问口。
func (a *API) getPending() *pendingAsk {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.pending
}

func (a *API) setPending(p *pendingAsk) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pending = p
}

func (a *API) clearPending() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pending = nil
}

// chatToolsHook 仅测试用：注入带工具定义的假 LLM（镜像 api.go 的 chatFn
// 范式，但 ChatWithTools 多一个 tools 参数，chatFn 签名装不下）。
var chatToolsHook func(ctx context.Context, cfg hubtools.Config, msgs []hubtools.Message, tools []hubtools.ToolFunc) (hubtools.Response, error)

// chatTools 发一次带工具定义的 LLM 请求：测试注入 chatToolsHook，
// 生产走 hubtools.Client.ChatWithTools。未配置模型返回 ErrLLMNotReady。
func (a *API) chatTools(ctx context.Context, msgs []hubtools.Message, tools []hubtools.ToolFunc) (hubtools.Response, error) {
	cfg := a.llmCfg()
	if cfg.Model == "" {
		return hubtools.Response{}, ErrLLMNotReady
	}
	if chatToolsHook != nil {
		return chatToolsHook(ctx, cfg, msgs, tools)
	}
	return hubtools.NewClient(cfg).ChatWithTools(ctx, msgs, tools)
}

// 哨兵与常量。
var ErrLoopBusy = errors.New("agentapi: 管理者循环已在运行")

// loopMaxRounds 是工具循环的轮次上限：超限后注入末轮提示再给一轮，
// 仍不收敛则交还用户，避免模型在工具调用里打转烧钱。
const loopMaxRounds = 6

// PendingAskTTL 是未决反问语境的有效期：过期后短输入不再自动续轮。desktop
// 侧续轮判定（hubroute.go）引用同一常量，两边过期口径同源——显式传
// Continuation 会绕过 Run 内部的 TTL 检查，调用方必须自行判过期。
const PendingAskTTL = 30 * time.Minute

// shortInputMax 是自动续轮判定的输入长度上限（rune）：超过视为新需求。
const shortInputMax = 20

// loopFinalHint 是轮次超限后注入给模型的末轮提示。
const loopFinalHint = "已到轮次上限：必须立即调用 ask 向用户确认，或直接输出自然语言结论，不要再调用其他工具。"

// loopGuidance 是循环无法收敛（轮次超限 / ask 被禁）时返回给用户的
// 待选指引文案：不自动反问，把决定权交还用户。
const loopGuidance = "这一轮没有形成明确结论。请换一种说法描述需求，或直接指定目标会话（例如「发给修复登录那个会话」）。"

// LoopProgress 是一次工具调用的进度帧：Total 在循环里天然未知（模型可能
// 一直追加调用），Round 表示当前 LLM 轮次，调用方按「第 Round 轮」渲染。
type LoopProgress struct {
	Tool  string
	Round int
}

// RunOpts 是 Run 的可选参数。
type RunOpts struct {
	Channel      string // hub | wechat（区分记忆与续轮语境归属）
	Continuation bool   // 本输入是对上一轮 ask 的回复
	Progress     func(LoopProgress)
}

// RunResult 是一次管理者循环的产出：Replies[0] 为最终结论（ask 时为
// 问题+编号选项列表）。
type RunResult struct {
	Replies    []string
	AskPending bool
	ToolSteps  int
}

// pendingAsk 是跨 Run 存续的未决反问语境：ask 工具落地时写入，续轮时
// 注入 system，创建落地/结论/超限/错误时清除。Options 携带 ask 的编号
// 选项原文（续轮注入必需）；Confirm/Candidates 由 create_session 的
// NeedsConfirm 填充，供续轮把用户回复的编号翻译回 ID。
type pendingAsk struct {
	Question   string
	Options    []string
	Channel    string
	Round      int // 连续 ask 轮数（AskMaxRounds 门禁计数）
	Ts         int64
	Confirm    *CreatePlan
	Candidates map[int]string // 候选编号 → 工具/工作区 ID
}

// loopState 是单次 Run 的循环内状态：工具注册表闭包与收尾记忆摘要共用。
type loopState struct {
	channel  string
	input    string
	cont     bool        // 本轮是上一轮 ask 的续轮
	pending  *pendingAsk // Run 开始时的语境快照（system 注入用）
	steps    int         // 已执行工具步数
	actions  []string    // 工具名序列（去重后作记忆摘要）
	degraded bool        // 本轮走了 RouteDecide 降级（不写工具摘要）
	ask      *RunResult  // ask 落地/被禁时置位，循环立即终止
}

// Run 执行一次管理者循环：把用户输入交给带工具的 LLM，模型按需调用
// list/send/create/ask 等工具，直到给出结论或反问。
func (a *API) Run(ctx context.Context, input string, opts RunOpts) (RunResult, error) {
	a.mu.Lock()
	if a.busy {
		a.mu.Unlock()
		return RunResult{}, ErrLoopBusy
	}
	a.busy = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.busy = false
		a.mu.Unlock()
	}()

	// 续轮判定：显式 Continuation 标志，或「短输入 + 同渠道未过期 pendingAsk」。
	// 短文本大概率是用户在回复上一轮的编号选项；新长需求清 pending 开新轮。
	// 显式标志须与 pending 渠道一致才采信：hub/wechat 渠道各自维护标记，可能
	// 陈旧（如 hub 渠道 ask 后另一渠道抢先开新轮）——跨渠道语境不得注入模型，
	// 按新输入处理（下方清 pending）。pending 为空时保留标志：clearPending
	// 变为 no-op，且续轮不降级 4xx 的纪律不受影响。
	pend := a.getPending()
	cont := opts.Continuation
	if cont && pend != nil && pend.Channel != opts.Channel {
		cont = false
	}
	if !cont && pend != nil && pend.Channel == opts.Channel &&
		time.Duration(now().UnixNano()-pend.Ts) <= PendingAskTTL &&
		len([]rune(strings.TrimSpace(input))) <= shortInputMax {
		cont = true
	}
	if !cont {
		a.clearPending()
	}
	st := &loopState{channel: opts.Channel, input: input, cont: cont, pending: pend}

	res, err := a.runLoop(ctx, input, opts, st)
	if errors.Is(err, ErrLLMNotReady) {
		// 环境未配置属调用方错误：不动 pending（用户配好模型后原语境仍在），
		// 也不写记忆（避免每次未配置都污染 journal）。
		return res, err
	}
	// 收尾清 pending：结论/超限/新轮错误都说明本轮语境已走到头；ask 落地
	// 保留等续轮。**续轮错误例外保留**——短回复的确认链路（候选编号 →
	// create_session）不应因一次瞬时错误作废，用户重试同一输入还能续上；
	// 否则 Confirm/Candidates 被清掉，创建确认链路就此打断。
	if err == nil && !res.AskPending {
		a.clearPending()
	}
	if err != nil && !st.cont {
		a.clearPending()
	}
	a.memoryAppend(st, res, err)
	return res, err
}

// runLoop 是工具循环主体：装配 system → 循环 chatTools → 执行工具 →
// 终止于结论 / ask / 降级 / 轮次超限。
func (a *API) runLoop(ctx context.Context, input string, opts RunOpts, st *loopState) (RunResult, error) {
	tools := a.buildLoopTools(ctx, st)
	defs := make([]hubtools.ToolFunc, len(tools))
	for i, t := range tools {
		defs[i] = t.def
	}
	msgs := []hubtools.Message{
		{Role: "system", Content: a.buildManagerSystem(st)},
		{Role: "user", Content: input},
	}

	for round := 1; ; round++ {
		// 轮次守卫：正常轮 1..loopMaxRounds；第 loopMaxRounds+1 轮注入末轮
		// 提示强制收敛；第 loopMaxRounds+2 轮仍不收敛则交还用户。
		if round == loopMaxRounds+1 {
			msgs = append(msgs, hubtools.Message{Role: "user", Content: loopFinalHint})
		}
		if round > loopMaxRounds+1 {
			return RunResult{Replies: []string{loopGuidance}, ToolSteps: st.steps}, nil
		}

		resp, err := a.chatTools(ctx, msgs, defs)
		if err != nil {
			if errors.Is(err, ErrLLMNotReady) {
				return RunResult{}, err
			}
			// 4xx 降级仅限非续轮的首轮：协议不被网关支持时第 1 轮就会失败；
			// mid-loop 的 4xx（如 429 限流）更可能是瞬时错误，且此时工具可能
			// 已真实投递，再走 RouteDecide 会把用户原文重复投出——直接上抛。
			// 续轮一律不降级：RouteDecide 没有上一轮问题与候选语境（用户短
			// 回复如 "1" 进去决策不可控），且降级 ask 分支会用新 pendingAsk
			// 覆盖 Confirm/Candidates，打断创建确认链路。
			if !st.cont && st.steps == 0 && strings.Contains(err.Error(), "状态码 4") {
				st.degraded = true
				return a.degradeRoute(ctx, input, st)
			}
			return RunResult{}, err
		}

		// 无工具调用：文本即结论。空白回复视为未产出，nudge 后继续轮转
		//（轮次守卫最终兜底）。
		if len(resp.ToolCalls) == 0 {
			text := strings.TrimSpace(resp.Text)
			if text == "" {
				msgs = append(msgs, hubtools.Message{Role: "user", Content: "回复为空：请给出自然语言结论，或调用工具继续。"})
				continue
			}
			return RunResult{Replies: []string{text}, ToolSteps: st.steps}, nil
		}

		// 追加 assistant(tool_calls) 与逐个 tool 结果（hubtools wire 已支持）。
		msgs = append(msgs, hubtools.Message{Role: "assistant", Content: resp.Text, ToolCalls: resp.ToolCalls})
		for _, call := range resp.ToolCalls {
			st.steps++
			st.actions = append(st.actions, call.Name)
			if opts.Progress != nil {
				opts.Progress(LoopProgress{Tool: call.Name, Round: round})
			}
			msgs = append(msgs, hubtools.Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    a.execTool(st, call, tools),
			})
		}
		// ask 是终止性工具：落地或被禁都立即结束循环。
		if st.ask != nil {
			st.ask.ToolSteps = st.steps
			return *st.ask, nil
		}
	}
}

// execTool 按名执行一次工具调用并把结果文本化：工具错误也喂回模型
// （模型可重试或转 ask），不中断循环。
func (a *API) execTool(st *loopState, call hubtools.ToolCall, tools []loopTool) string {
	for _, t := range tools {
		if t.def.Name != call.Name {
			continue
		}
		out, err := t.run(call.Arguments)
		if err != nil {
			return fmt.Sprintf("工具 %s 执行失败: %v", call.Name, err)
		}
		return out
	}
	return fmt.Sprintf("未知工具 %s", call.Name)
}

// degradeRoute 是 4xx 降级路径：回退 RouteDecide 单发决策并按 action 执行。
// 该轮不写工具摘要（st.degraded 已标记），status/expand 用 Backend 原语合成文本。
func (a *API) degradeRoute(ctx context.Context, input string, st *loopState) (RunResult, error) {
	plan, err := a.RouteDecide(ctx, input)
	if err != nil {
		return RunResult{}, err
	}
	switch plan.Action {
	case "single", "group":
		if err := a.RouteSend(plan); err != nil {
			return RunResult{}, err
		}
		var names []string
		for _, n := range plan.Targets {
			if n >= 1 && n <= len(plan.Sessions) {
				names = append(names, fmt.Sprintf("#%d %s", n, plan.Sessions[n-1].Title))
			}
		}
		return RunResult{Replies: []string{"已投递：" + strings.Join(names, "、")}}, nil
	case "status":
		return RunResult{Replies: []string{a.renderStatusReply(plan)}}, nil
	case "expand":
		return RunResult{Replies: []string{a.renderExpandReply(plan)}}, nil
	case "ask":
		// 降级反问同样记 pending，让下一条短输入能续上（轮次计数沿用）。
		round := 1
		if p := a.getPending(); p != nil {
			round = p.Round + 1
		}
		a.setPending(&pendingAsk{Question: plan.Question, Channel: st.channel, Round: round, Ts: now().UnixNano()})
		return RunResult{Replies: []string{plan.Question}, AskPending: true}, nil
	}
	return RunResult{}, fmt.Errorf("agentapi: 未知降级路由 action %q", plan.Action)
}

// renderStatusReply 合成 status 决策的文本回复：targets 为空时汇报全部活跃会话。
func (a *API) renderStatusReply(plan RoutePlan) string {
	idx := plan.Targets
	if len(idx) == 0 {
		for i := range plan.Sessions {
			idx = append(idx, i+1)
		}
	}
	var lines []string
	for _, n := range idx {
		if n < 1 || n > len(plan.Sessions) {
			continue
		}
		s := plan.Sessions[n-1]
		line := fmt.Sprintf("#%d 「%s」 %s", n, s.Title, s.Status)
		if sum := a.lastSummary(s); sum != "" {
			line += " 最后消息: " + sum
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "当前没有活跃会话。"
	}
	return strings.Join(lines, "\n")
}

// renderExpandReply 合成 expand 决策的文本回复：展开指定会话最近消息。
func (a *API) renderExpandReply(plan RoutePlan) string {
	n := plan.ExpandNum
	if n < 1 || n > len(plan.Sessions) {
		return fmt.Sprintf("编号 %d 不在会话快照内。", n)
	}
	s := plan.Sessions[n-1]
	head := fmt.Sprintf("「%s」最近消息：", s.Title)
	if s.ID == "" {
		return head + "\n（磁盘会话未激活，无消息）"
	}
	body := renderMessagesTail(a.b.SessionMessages(s.ID), plan.ExpandCount)
	if body == "" {
		return head + "\n（暂无可见消息）"
	}
	return head + "\n" + body
}

// memoryAppend 在循环收尾写记忆：结论/ask/错误/超限都记；降级轮不写工具
// 摘要；写失败只记日志（记忆尽力而为，不影响返回）。
func (a *API) memoryAppend(st *loopState, res RunResult, runErr error) {
	a.mu.Lock()
	m := a.mem
	a.mu.Unlock()
	if m == nil {
		return
	}
	outcome := ""
	switch {
	case runErr != nil:
		outcome = "失败: " + runErr.Error()
	case len(res.Replies) > 0:
		outcome = res.Replies[0]
	}
	actions := ""
	if !st.degraded {
		actions = strings.Join(dedupeStrings(st.actions), ",")
	}
	e := MemoryEntry{Channel: st.channel, Input: st.input, Outcome: outcome, Actions: actions}
	if err := m.Append(e); err != nil {
		log.Printf("agentapi: 写管理者记忆失败: %v", err)
	}
}

// buildManagerSystem 装配管理者 system 提示：角色 + 规则 +（续轮语境）+
// 记忆尾。记忆尾渲染进 system 而非独立消息，保持消息序列简单可断言。
func (a *API) buildManagerSystem(st *loopState) string {
	var b strings.Builder
	b.WriteString("你是 AgentHub 的管理者 agent：根据用户消息决定投递、查询或创建 agent 会话，并给出自然语言结论。\n")
	b.WriteString("规则：\n")
	b.WriteString("- 会话编号语境：活跃会话按 list_sessions 输出顺序编号 #1..#N。send/stop/set_model/session_messages 的 chat_id 参数一律用 list_sessions 输出的 ID 字符串，不要传编号。\n")
	b.WriteString("- 等待信号：状态 perm 表示会话在等权限确认；最后一条 agent 消息以 ？/? 结尾表示会话在向用户提问。等待中的会话仍可投递新指令。\n")
	b.WriteString("- 末轮必须产出自然语言结论：不再调用工具时，直接输出结论文本。\n")
	b.WriteString("- 意图不确定时必须调用 ask 列编号选项向用户确认，禁止猜测性群发。\n")

	if st.cont && st.pending != nil {
		b.WriteString("\n【续轮语境】你上一轮向用户提问：\n" + st.pending.Question + "\n")
		if len(st.pending.Options) > 0 {
			b.WriteString("选项：\n")
			for i, o := range st.pending.Options {
				fmt.Fprintf(&b, "%d. %s\n", i+1, o)
			}
		}
		if len(st.pending.Candidates) > 0 {
			keys := make([]int, 0, len(st.pending.Candidates))
			for k := range st.pending.Candidates {
				keys = append(keys, k)
			}
			sort.Ints(keys)
			b.WriteString("创建候选编号：")
			for _, k := range keys {
				fmt.Fprintf(&b, " %d=%s", k, st.pending.Candidates[k])
			}
			b.WriteString("\n用户回复编号时，把编号作为 tool_id/workspace 传给 create_session，系统会翻译成实际 ID。\n")
		}
	}

	if tail := renderMemoryTail(a.memoryRecent()); tail != "" {
		b.WriteString("\n" + tail)
	}
	return b.String()
}

// memoryRecent 取最近 10 条记忆（新在前）；未启用记忆返回空。
func (a *API) memoryRecent() []MemoryEntry {
	a.mu.Lock()
	m := a.mem
	a.mu.Unlock()
	if m == nil {
		return nil
	}
	return m.Recent(10)
}

// memorySearch 供 search_memory 工具检索 journal；未启用返回空。
func (a *API) memorySearch(kw string) []MemoryEntry {
	a.mu.Lock()
	m := a.mem
	a.mu.Unlock()
	if m == nil {
		return nil
	}
	return m.Search(kw)
}

// renderMemoryTail 把记忆条目渲染为「历史轮次」段，每条截断 ≤300 字。
func renderMemoryTail(entries []MemoryEntry) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("【历史轮次】最近的处理记录（新在前）：\n")
	for _, e := range entries {
		line := fmt.Sprintf("- [%s] 输入: %s → 结论: %s", e.Channel, oneLineText(e.Input, 150), oneLineText(e.Outcome, 150))
		if e.Actions != "" {
			line += fmt.Sprintf("（动作: %s）", e.Actions)
		}
		b.WriteString(oneLineText(line, 300) + "\n")
	}
	return b.String()
}

// oneLineText 单行化并按 rune 截断（喂给模型/用户的摘要不带换行与超长原文）。
func oneLineText(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", "⏎"))
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

// dedupeStrings 保序去重（记忆里的动作摘要不需要重复计次）。
func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// renderMessagesTail 渲染会话消息的尾部 n 条（保持时间正序）。
func renderMessagesTail(msgs []Message, n int) string {
	if n <= 0 {
		n = 5
	}
	if len(msgs) > n {
		msgs = msgs[len(msgs)-n:]
	}
	var b strings.Builder
	for i, m := range msgs {
		fmt.Fprintf(&b, "%d. [%s] %s\n", i+1, m.Kind, oneLineText(m.Text, 200))
	}
	return b.String()
}

// lastSummary 取会话最后一条可见消息的单行摘要：优先后端预算的 Summary，
// 缺失时从 SessionMessages 现算。
func (a *API) lastSummary(s Session) string {
	if s.Summary != "" {
		return oneLineText(s.Summary, 80)
	}
	if s.ID == "" {
		return ""
	}
	msgs := a.b.SessionMessages(s.ID)
	if len(msgs) == 0 {
		return ""
	}
	return oneLineText(msgs[len(msgs)-1].Text, 80)
}

// waitingUser 判断会话是否在等用户：perm 状态，或最后一条 agent 可见消息
// 以 ？/? 结尾（会话在向用户提问）。
func (a *API) waitingUser(s Session) bool {
	if s.Status == StatusPerm {
		return true
	}
	if s.ID == "" {
		return false
	}
	msgs := a.b.SessionMessages(s.ID)
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Kind == "agent" {
			t := strings.TrimSpace(msgs[i].Text)
			return strings.HasSuffix(t, "？") || strings.HasSuffix(t, "?")
		}
	}
	return false
}

// loopTool 是管理者循环注册表的一项：def 给模型的定义，run 执行并产出
// 喂回模型的文本。
type loopTool struct {
	def hubtools.ToolFunc
	run func(args string) (string, error)
}

// schemaObj 构造 object 参数的 JSON schema；required 为空时不输出该键
// （hubtools.ToolFunc 注释：Parameters 必须是完整 schema，nil 会 wire 成 null）。
func schemaObj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

// strProp 是字符串属性的便捷构造。
func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

// parseArgs 解析工具调用的 JSON 参数。
func parseArgs(args string) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(args), &m); err != nil {
		return nil, fmt.Errorf("参数 JSON 非法: %w", err)
	}
	return m, nil
}

// argStr / argInt 取字符串 / 数字参数（缺省零值）。
func argStr(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func argInt(m map[string]any, key string) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return 0
}

// buildLoopTools 构建固定 10 工具注册表（闭包捕获本次 Run 的 ctx 与状态）。
// 工具入参统一用会话 ID 字符串而非编号：编号在循环多轮间会 TOCTOU 漂移，
// 编号只在文本语境里供模型引用。
func (a *API) buildLoopTools(ctx context.Context, st *loopState) []loopTool {
	var tools []loopTool
	add := func(name, desc string, params map[string]any, run func(args string) (string, error)) {
		tools = append(tools, loopTool{
			def: hubtools.ToolFunc{Name: name, Description: desc, Parameters: params},
			run: run,
		})
	}

	// 1. list_sessions：活跃+磁盘快照，附编号语境与等待信号。
	add("list_sessions", "列出全部会话快照：编号/ID/工具/标题/状态/最后消息/是否等用户。send 等工具的 chat_id 用输出的 ID 值。",
		schemaObj(map[string]any{}),
		func(string) (string, error) { return a.renderSessionSnapshot(), nil })

	// 2. session_messages：读活跃会话最近消息。
	add("session_messages", "读取某活跃会话最近 n 条可见消息（时间正序）。",
		schemaObj(map[string]any{
			"chat_id": strProp("会话 chat_id（list_sessions 输出的 ID 值）"),
			"n":       map[string]any{"type": "integer", "description": "条数，默认 5"},
		}, "chat_id"),
		func(args string) (string, error) {
			m, err := parseArgs(args)
			if err != nil {
				return "", err
			}
			s, err := a.resolve(argStr(m, "chat_id"))
			if err != nil {
				return "", err
			}
			if s.ID == "" {
				return "", errors.New("会话未激活（磁盘会话），请先 open_session")
			}
			n := argInt(m, "n")
			if n <= 0 {
				n = 5
			}
			msgs := a.b.SessionMessages(s.ID)
			if len(msgs) == 0 {
				return "该会话暂无可见消息。", nil
			}
			return renderMessagesTail(msgs, n), nil
		})

	// 3. send：向活跃会话投递一轮输入。
	add("send", "向一个活跃会话发送一轮用户输入。",
		schemaObj(map[string]any{
			"chat_id": strProp("目标会话 chat_id"),
			"text":    strProp("要发送的正文"),
		}, "chat_id", "text"),
		func(args string) (string, error) {
			m, err := parseArgs(args)
			if err != nil {
				return "", err
			}
			text := argStr(m, "text")
			if text == "" {
				return "", errors.New("send 缺少 text")
			}
			s, err := a.resolve(argStr(m, "chat_id"))
			if err != nil {
				return "", err
			}
			if s.ID == "" {
				return "", errors.New("会话未激活（磁盘会话），请先 open_session")
			}
			if err := a.b.Send(s.ID, text); err != nil {
				return "", err
			}
			return fmt.Sprintf("已发送到 「%s」。", s.Title), nil
		})

	// 4. send_group：多目标投递，per_target 可按目标改写。
	add("send_group", "向多个活跃会话投递；各会话语境不同时用 per_target 按目标改写文本。",
		schemaObj(map[string]any{
			"targets":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "目标会话 chat_id 列表"},
			"text":       strProp("通用正文（per_target 未覆盖的目标使用）"),
			"per_target": map[string]any{"type": "object", "description": "chat_id → 改写文本（可选）"},
		}, "targets"),
		func(args string) (string, error) {
			m, err := parseArgs(args)
			if err != nil {
				return "", err
			}
			text := argStr(m, "text")
			per := map[string]string{}
			if pm, ok := m["per_target"].(map[string]any); ok {
				for k, v := range pm {
					per[k] = strings.TrimSpace(fmt.Sprint(v))
				}
			}
			arr, _ := m["targets"].([]any)
			if len(arr) == 0 {
				return "", errors.New("send_group 缺少 targets")
			}
			if text == "" && len(per) == 0 {
				return "", errors.New("send_group 缺少投递内容：text 与 per_target 至少其一非空")
			}
			var b strings.Builder
			for _, tv := range arr {
				target := strings.TrimSpace(fmt.Sprint(tv))
				txt := text
				if p, ok := per[target]; ok && p != "" {
					txt = p
				}
				s, err := a.resolve(target)
				switch {
				case err != nil:
					fmt.Fprintf(&b, "✗ %s: %v\n", target, err)
				case s.ID == "":
					fmt.Fprintf(&b, "✗ %s: 会话未激活，请先 open_session\n", target)
				default:
					if serr := a.b.Send(s.ID, txt); serr != nil {
						fmt.Fprintf(&b, "✗ %s: %v\n", target, serr)
					} else {
						fmt.Fprintf(&b, "✓ %s（%s）\n", target, s.Title)
					}
				}
			}
			return b.String(), nil
		})

	// 5. stop：中断会话当前轮次。
	add("stop", "中断某活跃会话当前正在运行的轮次。",
		schemaObj(map[string]any{"chat_id": strProp("目标会话 chat_id")}, "chat_id"),
		func(args string) (string, error) {
			m, err := parseArgs(args)
			if err != nil {
				return "", err
			}
			s, err := a.resolve(argStr(m, "chat_id"))
			if err != nil {
				return "", err
			}
			if s.ID == "" {
				return "", errors.New("会话未激活，无需停止")
			}
			if err := a.b.Stop(s.ID); err != nil {
				return "", err
			}
			return fmt.Sprintf("已停止 「%s」。", s.Title), nil
		})

	// 6. create_session：薄包 CreateNL（结构化参数直接走 Confirm 校验路径，
	// 不再二次 LLM 解析）；NeedsConfirm 渲染编号候选并存 pendingAsk 语境。
	add("create_session", "创建新会话并投递首条任务；参数不全时返回编号候选，用 ask 呈现给用户，用户回复后按编号重调。",
		schemaObj(map[string]any{
			"tool_id":   strProp("工具 ID，或上一轮候选编号"),
			"workspace": strProp("工作区路径，或上一轮候选编号"),
			"model":     strProp("模型名（可选）"),
			"task":      strProp("新会话的首个任务（可选）"),
		}),
		func(args string) (string, error) {
			m, err := parseArgs(args)
			if err != nil {
				return "", err
			}
			// 组装计划：候选语境存在时继承已定字段（如工具已定只缺工作区），
			// 编号参数经 Candidates 翻译回 ID。
			plan := CreatePlan{}
			if p := a.getPending(); p != nil && p.Confirm != nil {
				plan = *p.Confirm
			}
			plan.Question = "" // 旧反问不带入新一轮校验
			if v := argStr(m, "tool_id"); v != "" {
				plan.ToolID = a.translateCandidate(v)
			}
			if v := argStr(m, "workspace"); v != "" {
				plan.Workspace = a.translateCandidate(v)
			}
			if v := argStr(m, "model"); v != "" {
				plan.Model = v
			}
			if v := argStr(m, "task"); v != "" {
				plan.Task = v
			}
			res, err := a.CreateNL(ctx, "", NLCreate{Confirm: &plan})
			if err != nil && res.Created == nil {
				return "", err
			}
			if res.Created != nil {
				a.clearPending() // 创建落地：候选语境已消费
				reply := fmt.Sprintf("已创建会话（chat_id=%s，工具=%s）。", res.Created.ID, res.Created.Tool)
				if err != nil {
					// 会话已存在但首条任务投递失败：如实告知，模型可重试 send。
					return reply + " 但投递首条任务失败: " + err.Error(), nil
				}
				return reply, nil
			}
			need := res.NeedsConfirm
			cands := map[int]string{}
			var b strings.Builder
			b.WriteString(strings.TrimSpace(need.Question) + "\n")
			num := 1
			for _, t := range need.ToolOptions {
				fmt.Fprintf(&b, "%d. 工具 %s（%s）\n", num, t.ID, t.Label)
				cands[num] = t.ID
				num++
			}
			for _, w := range need.WorkspaceOptions {
				fmt.Fprintf(&b, "%d. 工作区 %s（%s）\n", num, w.ID, w.Label)
				cands[num] = w.ID
				num++
			}
			b.WriteString("请用 ask 把以上编号选项呈现给用户；用户回复后按编号调用 create_session。")
			a.updatePendingCandidates(st.channel, need, cands)
			return b.String(), nil
		})

	// 7. open_session：激活磁盘会话。
	add("open_session", "激活一个未运行的磁盘会话（恢复运行）。",
		schemaObj(map[string]any{"session_id": strProp("磁盘会话 ID")}, "session_id"),
		func(args string) (string, error) {
			m, err := parseArgs(args)
			if err != nil {
				return "", err
			}
			id := argStr(m, "session_id")
			if id == "" {
				return "", errors.New("open_session 缺少 session_id")
			}
			s, err := a.Open(id)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("已激活会话 「%s」（chat_id=%s）。", s.Title, s.ID), nil
		})

	// 8. set_model：运行时切换模型。
	add("set_model", "切换某活跃会话的模型。",
		schemaObj(map[string]any{
			"chat_id": strProp("目标会话 chat_id"),
			"model":   strProp("目标模型名"),
		}, "chat_id", "model"),
		func(args string) (string, error) {
			m, err := parseArgs(args)
			if err != nil {
				return "", err
			}
			model := argStr(m, "model")
			if model == "" {
				return "", errors.New("set_model 缺少 model")
			}
			s, err := a.resolve(argStr(m, "chat_id"))
			if err != nil {
				return "", err
			}
			if s.ID == "" {
				return "", errors.New("会话未激活，无法切模型")
			}
			if err := a.b.SetModel(s.ID, model); err != nil {
				return "", err
			}
			return fmt.Sprintf("已把 「%s」 切换到模型 %s。", s.Title, model), nil
		})

	// 9. ask：终止性反问；AskMaxRounds 门禁防无限套娃。
	add("ask", "向用户提出带编号选项的问题并结束本轮（等待用户回复）。意图不确定时必须用它确认，禁止猜测性群发。",
		schemaObj(map[string]any{
			"question": strProp("要问用户的问题"),
			"options":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "编号选项文本列表"},
		}, "question"),
		func(args string) (string, error) {
			m, err := parseArgs(args)
			if err != nil {
				return "", err
			}
			q := argStr(m, "question")
			if q == "" {
				return "", errors.New("ask 缺少 question")
			}
			var options []string
			if arr, ok := m["options"].([]any); ok {
				for _, v := range arr {
					options = append(options, fmt.Sprint(v))
				}
			}
			// 连续反问达到上限：禁用 ask，返回待选指引文案并清除语境。
			if p := a.getPending(); p != nil && p.Round >= hubtools.AskMaxRounds {
				a.clearPending()
				st.ask = &RunResult{Replies: []string{loopGuidance}}
				return loopGuidance, nil
			}
			// 落地：轮次 +1，继承 create_session 留下的候选语境（用户回编号
			// 时续轮要把编号翻译回 ID）。
			newRound := 1
			var confirm *CreatePlan
			var cands map[int]string
			if p := a.getPending(); p != nil {
				newRound = p.Round + 1
				confirm, cands = p.Confirm, p.Candidates
			}
			a.setPending(&pendingAsk{
				Question:   q,
				Options:    options,
				Channel:    st.channel,
				Round:      newRound,
				Ts:         now().UnixNano(),
				Confirm:    confirm,
				Candidates: cands,
			})
			var b strings.Builder
			b.WriteString(q + "\n")
			for i, o := range options {
				fmt.Fprintf(&b, "%d. %s\n", i+1, o)
			}
			reply := strings.TrimRight(b.String(), "\n")
			st.ask = &RunResult{Replies: []string{reply}, AskPending: true}
			return reply, nil
		})

	// 10. search_memory：检索历史轮次 journal。
	add("search_memory", "在管理者记忆（历史轮次）里按关键词搜索，命中输入/结论/动作任一字段。",
		schemaObj(map[string]any{"keyword": strProp("搜索关键词")}, "keyword"),
		func(args string) (string, error) {
			m, err := parseArgs(args)
			if err != nil {
				return "", err
			}
			kw := argStr(m, "keyword")
			if kw == "" {
				return "", errors.New("search_memory 缺少 keyword")
			}
			entries := a.memorySearch(kw)
			if len(entries) == 0 {
				return "记忆中没有匹配「" + kw + "」的条目。", nil
			}
			var b strings.Builder
			b.WriteString("记忆命中：\n")
			for _, e := range entries {
				line := fmt.Sprintf("- [%s] 输入: %s → 结论: %s", e.Channel, oneLineText(e.Input, 150), oneLineText(e.Outcome, 150))
				if e.Actions != "" {
					line += fmt.Sprintf("（动作: %s）", e.Actions)
				}
				b.WriteString(line + "\n")
			}
			return b.String(), nil
		})

	return tools
}

// renderSessionSnapshot 渲染 list_sessions 的快照文本：活跃会话带 #n 编号
// 语境与等待信号；磁盘会话单独分区并提示先激活。
func (a *API) renderSessionSnapshot() string {
	active, err := a.List(ScopeActive)
	if err != nil {
		return "读取活跃会话失败: " + err.Error()
	}
	disk, err := a.List(ScopeDisk)
	if err != nil {
		return "读取磁盘会话失败: " + err.Error()
	}
	if len(active) == 0 && len(disk) == 0 {
		return "当前没有任何会话。"
	}
	var b strings.Builder
	if len(active) > 0 {
		b.WriteString("活跃会话（编号 #n 为投递语境；send 等工具的 chat_id 参数用 ID 值）：\n")
		for i, s := range active {
			fmt.Fprintf(&b, "#%d ID=%s 工具=%s 「%s」 状态=%s", i+1, s.ID, s.Tool, s.Title, s.Status)
			if sum := a.lastSummary(s); sum != "" {
				fmt.Fprintf(&b, " 最后消息: %s", sum)
			}
			if a.waitingUser(s) {
				b.WriteString(" [等待用户]")
			}
			b.WriteString("\n")
		}
	}
	if len(disk) > 0 {
		b.WriteString("磁盘会话（未激活，先 open_session 再 send）：\n")
		for i, s := range disk {
			fmt.Fprintf(&b, "磁盘#%d ID=%s 工具=%s 「%s」 状态=%s\n", i+1, s.SessionID, s.Tool, s.Title, s.Status)
		}
	}
	return b.String()
}

// translateCandidate 把候选编号（如 "1"）翻译为对应 ID：非编号、无候选语境
// 或编号越界时原样返回，让 CreateNL 校验给出明确错误（而非静默猜）。
func (a *API) translateCandidate(v string) string {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return v
	}
	p := a.getPending()
	if p == nil || p.Candidates == nil {
		return v
	}
	if id, ok := p.Candidates[n]; ok {
		return id
	}
	return v
}

// updatePendingCandidates 把 create_session 的待确认候选写入 pendingAsk
// （跨 Run 存续）：已有 ask 语境时只补 Confirm/Candidates，不破坏轮次计数。
func (a *API) updatePendingCandidates(channel string, confirm *CreatePlan, cands map[int]string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := a.pending
	if p == nil {
		p = &pendingAsk{Channel: channel, Ts: now().UnixNano()}
	}
	p.Confirm = confirm
	p.Candidates = cands
	a.pending = p
}
