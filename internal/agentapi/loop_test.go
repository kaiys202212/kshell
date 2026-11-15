package agentapi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/hubtools"
)

// tc 是构造工具调用的便捷函数。
func tc(id, name, args string) hubtools.ToolCall {
	return hubtools.ToolCall{ID: id, Name: name, Arguments: args}
}

// scriptChat 是按脚本顺序返回响应的 chatToolsHook 载体：记录每次调用的
// system 内容与最后的完整消息序列，供断言装配形状与注入内容。
type scriptChat struct {
	resps   []hubtools.Response
	err     error
	i       int
	sysSeen []string           // 每次调用的 system 内容
	last    []hubtools.Message // 最后一次调用的完整消息序列
}

func (s *scriptChat) hook(_ context.Context, _ hubtools.Config, msgs []hubtools.Message, _ []hubtools.ToolFunc) (hubtools.Response, error) {
	if len(msgs) > 0 {
		s.sysSeen = append(s.sysSeen, msgs[0].Content)
	}
	s.last = append([]hubtools.Message(nil), msgs...)
	if s.err != nil {
		return hubtools.Response{}, s.err
	}
	r := s.resps[s.i]
	s.i++
	return r, nil
}

// newLoopAPI 构造带记忆与 LLM 配置的 API，并注册 hook 清理。
func newLoopAPI(t *testing.T) (*API, *fakeBackend) {
	t.Helper()
	api := New(newFake())
	api.SetMemory(NewManagerMemory(t.TempDir()))
	api.UpdateLLM(hubtools.Config{BaseURL: "http://x", APIKey: "k", Model: "m"})
	t.Cleanup(func() { chatToolsHook = nil })
	return api, api.backend()
}

// backend 取桩（测试白盒便捷访问）。
func (a *API) backend() *fakeBackend { return a.b.(*fakeBackend) }

// 测试 1：多步工具调用——list_sessions → send → 结论。
func TestRunLoopMultiStep(t *testing.T) {
	api, f := newLoopAPI(t)
	sc := &scriptChat{resps: []hubtools.Response{
		{ToolCalls: []hubtools.ToolCall{tc("c1", "list_sessions", "{}")}},
		{ToolCalls: []hubtools.ToolCall{tc("c2", "send", `{"chat_id":"chat-a","text":"继续修复"}`)}},
		{Text: "已把「继续修复」投递给修复登录会话。"},
	}}
	chatToolsHook = sc.hook

	var progress []LoopProgress
	res, err := api.Run(context.Background(), "把继续修复发给修复登录那个会话", RunOpts{
		Channel:  "hub",
		Progress: func(p LoopProgress) { progress = append(progress, p) },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.AskPending {
		t.Fatal("纯执行轮不应 AskPending")
	}
	if res.Replies[0] != "已把「继续修复」投递给修复登录会话。" {
		t.Fatalf("Replies[0] = %q", res.Replies[0])
	}
	if res.ToolSteps != 2 {
		t.Fatalf("ToolSteps = %d, want 2", res.ToolSteps)
	}
	// Backend 桩收到正确 Send
	if len(f.sent) != 1 || f.sent[0] != "chat-a|继续修复" {
		t.Fatalf("f.sent = %v", f.sent)
	}
	// Progress 每次工具调用触发
	if len(progress) != 2 || progress[0].Tool != "list_sessions" || progress[1].Tool != "send" {
		t.Fatalf("progress = %+v", progress)
	}
	if progress[0].Round != 1 || progress[1].Round != 2 {
		t.Fatalf("progress Round 错误: %+v", progress)
	}
	// 消息装配：[sys,user] → +assistant(tool_calls)+tool → 再追加一轮
	if len(sc.sysSeen) != 3 {
		t.Fatalf("chat 调用次数 = %d, want 3", len(sc.sysSeen))
	}
	if len(sc.last) != 6 {
		t.Fatalf("最后消息序列长度 = %d, want 6", len(sc.last))
	}
	if sc.last[2].Role != "assistant" || len(sc.last[2].ToolCalls) == 0 {
		t.Fatalf("消息[2] 应为带 tool_calls 的 assistant: %+v", sc.last[2])
	}
	if sc.last[3].Role != "tool" || sc.last[3].ToolCallID != "c1" {
		t.Fatalf("消息[3] 应为 c1 的 tool 结果: %+v", sc.last[3])
	}
	if !strings.Contains(sc.last[3].Content, "修复登录") {
		t.Fatalf("list_sessions 结果缺会话标题: %q", sc.last[3].Content)
	}
	if sc.last[5].Role != "tool" || sc.last[5].ToolCallID != "c2" || !strings.Contains(sc.last[5].Content, "已发送") {
		t.Fatalf("消息[5] 应为 c2 的成功结果: %+v", sc.last[5])
	}
}

// 测试 2：create_session 全参数 → Backend.Create + 首条任务 Send。
func TestRunLoopCreateFullParams(t *testing.T) {
	api, f := newLoopAPI(t)
	sc := &scriptChat{resps: []hubtools.Response{
		{ToolCalls: []hubtools.ToolCall{tc("c1", "create_session", `{"tool_id":"claude","workspace":"d:/ws/a","task":"跑回归测试"}`)}},
		{Text: "已创建会话并投递任务。"},
	}}
	chatToolsHook = sc.hook

	res, err := api.Run(context.Background(), "新建一个 claude 会话跑回归测试", RunOpts{Channel: "hub"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.AskPending {
		t.Fatal("全参数创建不应 AskPending")
	}
	if len(f.created) != 1 || f.created[0] != [3]string{"claude", "d:/ws/a", ""} {
		t.Fatalf("f.created = %v", f.created)
	}
	if len(f.sent) != 1 || f.sent[0] != "chat-new|跑回归测试" {
		t.Fatalf("首条任务未投递: %v", f.sent)
	}
	if res.ToolSteps != 1 {
		t.Fatalf("ToolSteps = %d, want 1", res.ToolSteps)
	}
}

// 测试 3：create_session 缺参数 → NeedsConfirm → ask 呈现候选；
// 续轮回编号 → Candidates 翻译 → CreateNL{Confirm} 落地 Create。
func TestRunLoopCreateNeedsConfirmContinuation(t *testing.T) {
	api, f := newLoopAPI(t)

	// 第一轮：缺工具参数 → 编号候选 → ask 终止
	sc1 := &scriptChat{resps: []hubtools.Response{
		{ToolCalls: []hubtools.ToolCall{tc("c1", "create_session", `{"task":"写周报"}`)}},
		{ToolCalls: []hubtools.ToolCall{tc("c2", "ask", `{"question":"请选择工具","options":["claude Claude Code","codex Codex"]}`)}},
	}}
	chatToolsHook = sc1.hook
	res1, err := api.Run(context.Background(), "新建会话写周报", RunOpts{Channel: "hub"})
	if err != nil {
		t.Fatalf("Run1: %v", err)
	}
	if !res1.AskPending {
		t.Fatal("NeedsConfirm + ask 后应 AskPending")
	}
	if !strings.Contains(res1.Replies[0], "请选择工具") || !strings.Contains(res1.Replies[0], "1. claude") {
		t.Fatalf("Replies[0] 缺问题或编号选项: %q", res1.Replies[0])
	}
	p := api.getPending()
	if p == nil || p.Confirm == nil || p.Candidates[1] != "claude" || p.Candidates[2] != "codex" {
		t.Fatalf("pendingAsk 候选语境缺失: %+v", p)
	}
	if p.Round != 1 {
		t.Fatalf("首轮 ask Round = %d, want 1", p.Round)
	}

	// 第二轮：Continuation，模型回编号 → 翻译成 ID → Confirm 落地
	sc2 := &scriptChat{resps: []hubtools.Response{
		{ToolCalls: []hubtools.ToolCall{tc("c3", "create_session", `{"tool_id":"1","workspace":"d:/ws/a","task":"写周报"}`)}},
		{Text: "已创建 codex… 不对，是 claude 会话。"},
	}}
	chatToolsHook = sc2.hook
	res2, err := api.Run(context.Background(), "1", RunOpts{Channel: "hub", Continuation: true})
	if err != nil {
		t.Fatalf("Run2: %v", err)
	}
	if res2.AskPending {
		t.Fatal("创建落地后不应 AskPending")
	}
	if len(f.created) != 1 || f.created[0] != [3]string{"claude", "d:/ws/a", ""} {
		t.Fatalf("编号未翻译，f.created = %v", f.created)
	}
	if len(f.sent) != 1 || f.sent[0] != "chat-new|写周报" {
		t.Fatalf("首条任务未投递: %v", f.sent)
	}
	// 续轮 system 注入上一轮问题与候选
	sys := sc2.sysSeen[0]
	if !strings.Contains(sys, "请选择工具") || !strings.Contains(sys, "claude") {
		t.Fatalf("续轮 system 缺上一轮 ask 语境: %q", sys)
	}
	if api.getPending() != nil {
		t.Fatal("创建落地后 pendingAsk 应清除")
	}
}

// 测试 4：ask 工具落地 + 数字短输入自动续轮（无 Continuation 标志）。
func TestRunLoopAskAndShortInputContinuation(t *testing.T) {
	api, _ := newLoopAPI(t)

	sc1 := &scriptChat{resps: []hubtools.Response{
		{ToolCalls: []hubtools.ToolCall{tc("c1", "ask", `{"question":"选哪个？","options":["继续","新建"]}`)}},
	}}
	chatToolsHook = sc1.hook
	res1, err := api.Run(context.Background(), "看着办", RunOpts{Channel: "hub"})
	if err != nil {
		t.Fatalf("Run1: %v", err)
	}
	if !res1.AskPending || !strings.Contains(res1.Replies[0], "1. 继续") {
		t.Fatalf("ask 未落地: %+v", res1)
	}

	// 数字短输入自动续轮（Continuation 未设置）
	sc2 := &scriptChat{resps: []hubtools.Response{{Text: "好的，按新建处理。"}}}
	chatToolsHook = sc2.hook
	res2, err := api.Run(context.Background(), "2", RunOpts{Channel: "hub"})
	if err != nil {
		t.Fatalf("Run2: %v", err)
	}
	if res2.AskPending {
		t.Fatal("续轮给结论后不应 AskPending")
	}
	if !strings.Contains(sc2.sysSeen[0], "选哪个？") || !strings.Contains(sc2.sysSeen[0], "2. 新建") {
		t.Fatalf("自动续轮 system 缺上一轮 ask 语境: %q", sc2.sysSeen[0])
	}
	if api.getPending() != nil {
		t.Fatal("结论落地后 pendingAsk 应清除")
	}
}

// 测试 5：AskMaxRounds 超限禁 ask，降级返回待选指引文案。
func TestRunLoopAskMaxRounds(t *testing.T) {
	api, _ := newLoopAPI(t)
	askCall := hubtools.Response{ToolCalls: []hubtools.ToolCall{tc("c", "ask", `{"question":"还要什么？","options":["a","b"]}`)}}
	newAskHook := func() func(context.Context, hubtools.Config, []hubtools.Message, []hubtools.ToolFunc) (hubtools.Response, error) {
		return (&scriptChat{resps: []hubtools.Response{askCall}}).hook
	}

	// 第一问
	chatToolsHook = newAskHook()
	if _, err := api.Run(context.Background(), "起点", RunOpts{Channel: "hub"}); err != nil {
		t.Fatalf("Run1: %v", err)
	}
	// 第二问（续轮）
	chatToolsHook = newAskHook()
	if _, err := api.Run(context.Background(), "a", RunOpts{Channel: "hub", Continuation: true}); err != nil {
		t.Fatalf("Run2: %v", err)
	}
	if p := api.getPending(); p == nil || p.Round != 2 {
		t.Fatalf("第二轮 ask 后 Round = %+v, want 2", p)
	}
	// 第三问被拒：AskPending=false，返回指引文案，pending 清除
	chatToolsHook = newAskHook()
	res, err := api.Run(context.Background(), "b", RunOpts{Channel: "hub", Continuation: true})
	if err != nil {
		t.Fatalf("Run3: %v", err)
	}
	if res.AskPending {
		t.Fatal("超限后不应再 AskPending")
	}
	if res.Replies[0] != loopGuidance {
		t.Fatalf("超限应返回指引文案, got %q", res.Replies[0])
	}
	if api.getPending() != nil {
		t.Fatal("超限拒绝后 pendingAsk 应清除")
	}
}

// 测试 6：记忆——首轮后 journal 有条目；第二轮 system 含记忆尾；search_memory 命中。
func TestRunLoopMemory(t *testing.T) {
	api, _ := newLoopAPI(t)

	sc1 := &scriptChat{resps: []hubtools.Response{
		{ToolCalls: []hubtools.ToolCall{tc("c1", "list_sessions", "{}")}},
		{Text: "已把指令发给修复登录会话。"},
	}}
	chatToolsHook = sc1.hook
	if _, err := api.Run(context.Background(), "催一下登录修复", RunOpts{Channel: "hub"}); err != nil {
		t.Fatalf("Run1: %v", err)
	}
	mem := api.memoryNow()
	if got := mem.Recent(1); len(got) != 1 || got[0].Input != "催一下登录修复" ||
		got[0].Outcome != "已把指令发给修复登录会话。" || got[0].Actions != "list_sessions" {
		t.Fatalf("journal 条目错误: %+v", got)
	}

	// 第二轮：system 含记忆尾；search_memory 命中早期条目
	sc2 := &scriptChat{resps: []hubtools.Response{
		{ToolCalls: []hubtools.ToolCall{tc("c2", "search_memory", `{"keyword":"登录"}`)}},
		{Text: "记忆里有相关记录。"},
	}}
	chatToolsHook = sc2.hook
	if _, err := api.Run(context.Background(), "之前登录的事到哪一步了", RunOpts{Channel: "hub"}); err != nil {
		t.Fatalf("Run2: %v", err)
	}
	if !strings.Contains(sc2.sysSeen[0], "历史轮次") || !strings.Contains(sc2.sysSeen[0], "催一下登录修复") {
		t.Fatalf("第二轮 system 缺记忆尾: %q", sc2.sysSeen[0])
	}
	// tool 结果（search_memory）应命中首轮结论
	var searchResult string
	for _, m := range sc2.last {
		if m.Role == "tool" {
			searchResult = m.Content
		}
	}
	if !strings.Contains(searchResult, "已把指令发给修复登录会话。") {
		t.Fatalf("search_memory 未命中首轮条目: %q", searchResult)
	}
}

// 测试 7：ChatWithTools 4xx → RouteDecide 降级单发生效。
func TestRunLoopDegrade4xx(t *testing.T) {
	api, f := newLoopAPI(t)
	chatToolsHook = func(context.Context, hubtools.Config, []hubtools.Message, []hubtools.ToolFunc) (hubtools.Response, error) {
		return hubtools.Response{}, errors.New("err.hubtools.request_failed: 状态码 400, 响应: bad request")
	}
	api.setChatFn(func(context.Context, []hubtools.Message) (hubtools.Response, error) {
		return hubtools.Response{Text: `{"action":"single","targets":[2],"text":"查一下进度"}`}, nil
	})

	res, err := api.Run(context.Background(), "问下文档会话进度", RunOpts{Channel: "hub"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(f.sent) != 1 || f.sent[0] != "chat-b|查一下进度" {
		t.Fatalf("降级投递未生效: %v", f.sent)
	}
	if !strings.Contains(res.Replies[0], "已投递") || !strings.Contains(res.Replies[0], "#2") {
		t.Fatalf("降级结论缺投递摘要: %q", res.Replies[0])
	}
	// 降级轮不写工具摘要
	if got := api.memoryNow().Recent(1); len(got) != 1 || got[0].Actions != "" {
		t.Fatalf("降级轮 Actions 应为空: %+v", got)
	}
}

// 测试 7b：mid-loop 4xx 不降级——第 1 轮 send 已真实投递，第 2 轮 chatTools
// 返回 429 时应上抛错误，而不是对原文重新 RouteDecide + RouteSend 重复投递。
func TestRunLoopDegradeOnlyFirstRound(t *testing.T) {
	api, f := newLoopAPI(t)
	err429 := errors.New("err.hubtools.request_failed: 状态码 429, 响应: too many requests")
	// 第 1 轮正常返回 send 工具调用，第 2 轮起返回 429 错误。
	calls := 0
	chatToolsHook = func(context.Context, hubtools.Config, []hubtools.Message, []hubtools.ToolFunc) (hubtools.Response, error) {
		calls++
		if calls == 1 {
			return hubtools.Response{ToolCalls: []hubtools.ToolCall{tc("c1", "send", `{"chat_id":"chat-a","text":"先投一轮"}`)}}, nil
		}
		return hubtools.Response{}, err429
	}
	api.setChatFn(func(context.Context, []hubtools.Message) (hubtools.Response, error) {
		// 降级若误触发会走这里：返回一个会再次投递的决策，用于暴露重复投递。
		return hubtools.Response{Text: `{"action":"single","targets":[1],"text":"重复投递的原文"}`}, nil
	})

	_, err := api.Run(context.Background(), "原始需求", RunOpts{Channel: "hub"})
	if err == nil || !strings.Contains(err.Error(), "状态码 429") {
		t.Fatalf("mid-loop 4xx 应上抛错误, got %v", err)
	}
	// Backend 只有第 1 轮那一次 Send，没有降级带来的第二次投递
	if len(f.sent) != 1 || f.sent[0] != "chat-a|先投一轮" {
		t.Fatalf("不应重复投递: %v", f.sent)
	}
	// 已执行过工具的错误路径照常写记忆（Actions 非空、Outcome 为失败）
	if got := api.memoryNow().Recent(1); len(got) != 1 || got[0].Actions != "send" || !strings.Contains(got[0].Outcome, "失败") {
		t.Fatalf("错误路径记忆条目错误: %+v", got)
	}
}

// 测试 7c：续轮不降级——续轮首轮 chatTools 4xx 应上抛错误。RouteDecide 没有
// 上一轮问题与候选语境（用户短回复如 "1" 进去决策不可控），且降级 ask 分支
// 会用新 pendingAsk 覆盖 Confirm/Candidates，打断创建确认链路。
func TestRunLoopDegradeSkipContinuation(t *testing.T) {
	api, f := newLoopAPI(t)

	// 第一轮：create_session 缺参数 → 候选存 pendingAsk → ask 终止
	sc1 := &scriptChat{resps: []hubtools.Response{
		{ToolCalls: []hubtools.ToolCall{tc("c1", "create_session", `{"task":"写周报"}`)}},
		{ToolCalls: []hubtools.ToolCall{tc("c2", "ask", `{"question":"请选择工具","options":["claude Claude Code"]}`)}},
	}}
	chatToolsHook = sc1.hook
	if _, err := api.Run(context.Background(), "新建会话写周报", RunOpts{Channel: "hub"}); err != nil {
		t.Fatalf("Run1: %v", err)
	}
	if p := api.getPending(); p == nil || p.Confirm == nil || p.Candidates[1] != "claude" {
		t.Fatalf("第一轮后候选语境缺失: %+v", p)
	}

	// 续轮：chatTools 直接 4xx → 应上抛错误，不进 RouteDecide
	chatToolsHook = func(context.Context, hubtools.Config, []hubtools.Message, []hubtools.ToolFunc) (hubtools.Response, error) {
		return hubtools.Response{}, errors.New("err.hubtools.request_failed: 状态码 400, 响应: bad request")
	}
	api.setChatFn(func(context.Context, []hubtools.Message) (hubtools.Response, error) {
		// 降级若误触发会走这里：投递决策 + ask 分支覆盖 pendingAsk 双重破坏。
		t.Error("续轮不应触发降级 RouteDecide")
		return hubtools.Response{Text: `{"action":"single","targets":[1],"text":"误投"}`}, nil
	})

	_, err := api.Run(context.Background(), "1", RunOpts{Channel: "hub", Continuation: true})
	if err == nil || !strings.Contains(err.Error(), "状态码 400") {
		t.Fatalf("续轮 4xx 应上抛错误, got %v", err)
	}
	// 候选语境未被降级路径破坏（Confirm/Candidates/Options 原样保留）
	p := api.getPending()
	if p == nil || p.Confirm == nil || p.Candidates[1] != "claude" ||
		len(p.Options) != 1 || p.Question != "请选择工具" {
		t.Fatalf("续轮错误后候选语境应保留: %+v", p)
	}
	// 无 RouteSend 副作用
	if len(f.sent) != 0 {
		t.Fatalf("续轮错误不应产生投递: %v", f.sent)
	}
}

// 测试 8：ErrLoopBusy 并发重入；ErrLLMNotReady 原样上抛。
func TestRunLoopBusyAndNotReady(t *testing.T) {
	// 未配置 LLM
	api := New(newFake())
	_, err := api.Run(context.Background(), "hi", RunOpts{})
	if !errors.Is(err, ErrLLMNotReady) {
		t.Fatalf("期望 ErrLLMNotReady, got %v", err)
	}
	// 重入
	api2, _ := newLoopAPI(t)
	api2.mu.Lock()
	api2.busy = true
	api2.mu.Unlock()
	_, err = api2.Run(context.Background(), "hi", RunOpts{})
	if !errors.Is(err, ErrLoopBusy) {
		t.Fatalf("期望 ErrLoopBusy, got %v", err)
	}
}

// 测试 9：轮次守卫——模型连调 6 轮工具后超限，最后一轮仍无结论返回指引文案。
func TestRunLoopRoundGuard(t *testing.T) {
	api, _ := newLoopAPI(t)
	loopCall := hubtools.Response{ToolCalls: []hubtools.ToolCall{tc("c", "list_sessions", "{}")}}
	convergeHook := &scriptChat{resps: []hubtools.Response{
		loopCall, loopCall, loopCall, loopCall, loopCall, loopCall,
		{Text: "结论来了"},
	}}
	chatToolsHook = convergeHook.hook

	res, err := api.Run(context.Background(), "看看会话", RunOpts{Channel: "hub"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Replies[0] != "结论来了" {
		t.Fatalf("最后一轮提示后应允许结论, got %q", res.Replies[0])
	}
	if res.ToolSteps != 6 {
		t.Fatalf("ToolSteps = %d, want 6", res.ToolSteps)
	}

	// 连 7 轮都不收敛 → 指引文案
	api2, _ := newLoopAPI(t)
	runawayHook := &scriptChat{resps: []hubtools.Response{
		loopCall, loopCall, loopCall, loopCall, loopCall, loopCall, loopCall,
	}}
	chatToolsHook = runawayHook.hook
	res2, err := api2.Run(context.Background(), "看看会话", RunOpts{Channel: "hub"})
	if err != nil {
		t.Fatalf("Run2: %v", err)
	}
	if res2.AskPending || res2.Replies[0] != loopGuidance {
		t.Fatalf("超限应返回指引文案: %+v", res2)
	}
}
