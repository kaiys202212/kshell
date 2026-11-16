package agentapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

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

// fakeClock 把包级 now 替换为可推进的假钟，返回推进函数；测试结束自动还原。
func fakeClock(t *testing.T, start time.Time) func(time.Duration) {
	t.Helper()
	cur := start
	t.Cleanup(func() { now = time.Now })
	now = func() time.Time { return cur }
	return func(d time.Duration) { cur = cur.Add(d) }
}

// askScript 构造一个首轮就调用 ask 的脚本（挂起链测试的最小起点）。
func askScript(q string) *scriptChat {
	args := fmt.Sprintf(`{"question":%q,"options":["a","b"]}`, q)
	return &scriptChat{resps: []hubtools.Response{
		{ToolCalls: []hubtools.ToolCall{tc("c", "ask", args)}},
	}}
}

// 转述上限：会话消息正文 2000 rune 内完整呈现（原 200 截断会把模块回复掐头）；
// 摘要从 SessionMessages 末条直取、300 rune 内完整（不再用 60 rune 的预算摘要）。
func TestRenderMessagesTailKeepsLongText(t *testing.T) {
	long := strings.Repeat("长", 1500)
	msgs := []Message{{Kind: "agent", Text: long}}
	out := renderMessagesTail(msgs, 5)
	if !strings.Contains(out, "长长长长长长长长") || len([]rune(out)) < 1500 {
		t.Fatalf("1500 rune 正文不应被截断: len=%d", len([]rune(out)))
	}
	huge := strings.Repeat("超", 3000)
	trimmed := renderMessagesTail([]Message{{Kind: "agent", Text: huge}}, 5)
	// oneLineText 截到 2000 rune + 省略号，行首还有序号前缀
	if len([]rune(trimmed)) > 2020 {
		t.Fatalf("超过 2000 rune 仍应截断到上限附近: len=%d", len([]rune(trimmed)))
	}
}

func TestLastSummaryPrefersSessionMessages(t *testing.T) {
	long := strings.Repeat("回", 250)
	api := New(&fakeBackend{
		active: []Session{{ID: "c1", Tool: "claude", Title: "甲", Status: StatusIdle, Summary: "60字预算摘要"}},
		msgs:   map[string][]Message{"c1": {{Kind: "agent", Text: long}}},
	})
	got := api.lastSummary(api.backend().active[0])
	if len([]rune(got)) < 250 || !strings.HasPrefix(got, "回回回") {
		t.Fatalf("lastSummary 应取 SessionMessages 末条全文: len=%d head=%q", len([]rune(got)), got[:min(20, len([]rune(got)))])
	}
}

// 测试：多步工具调用——list_sessions → send → 结论。
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
	if res.Suspended || res.Ask != nil {
		t.Fatal("纯执行轮不应挂起")
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

// 测试：create_session 全参数 → Backend.Create + 首条任务 Send。
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
	if res.Suspended {
		t.Fatal("全参数创建不应挂起")
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

// 测试 1（新）：ask 落地 → Suspended/Ask 载荷/编号选项回执；HasSuspendedAsk
// 渠道口径。
func TestSuspendAskLanding(t *testing.T) {
	api, _ := newLoopAPI(t)
	chatToolsHook = askScript("选哪个？").hook
	res, err := api.Run(context.Background(), "看着办", RunOpts{Channel: "hub"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Suspended || res.Ask == nil {
		t.Fatalf("ask 落地应挂起: %+v", res)
	}
	if res.Ask.Mode != "single" {
		t.Fatalf("mode 缺省应归一 single: %+v", res.Ask)
	}
	if res.Ask.Question != "选哪个？" || len(res.Ask.Options) != 2 {
		t.Fatalf("Ask 载荷错误: %+v", res.Ask)
	}
	if !strings.Contains(res.Replies[0], "1. a") || !strings.Contains(res.Replies[0], "2. b") {
		t.Fatalf("Replies 应含编号选项: %q", res.Replies[0])
	}
	if !api.HasSuspendedAsk("hub") {
		t.Fatal("同渠道应报告挂起在场")
	}
	if api.HasSuspendedAsk("wechat") {
		t.Fatal("跨渠道不应报告挂起在场")
	}
}

// 测试 2（新）：Resume 续跑 msgs 延续——请求 = 挂起前完整序列（含 system/
// user/assistant/tool）+ 用户回复原话（无前缀、不重建 system）。
func TestResumeMsgsContinuity(t *testing.T) {
	api, _ := newLoopAPI(t)
	sc1 := &scriptChat{resps: []hubtools.Response{
		{ToolCalls: []hubtools.ToolCall{tc("c1", "list_sessions", "{}")}},
		{ToolCalls: []hubtools.ToolCall{tc("c2", "ask", `{"question":"发给哪个？","options":["甲","乙"]}`)}},
	}}
	chatToolsHook = sc1.hook
	if _, err := api.Run(context.Background(), "把结论发过去", RunOpts{Channel: "hub"}); err != nil {
		t.Fatalf("Run1: %v", err)
	}
	suspended := sc1.last // 挂起前最后一次请求的序列（ask 的 assistant/tool 结果在其后追加）

	sc2 := &scriptChat{resps: []hubtools.Response{{Text: "已发给甲。"}}}
	chatToolsHook = sc2.hook
	res, err := api.Resume(context.Background(), "hub", "1", RunOpts{})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if res.Suspended || res.Ask != nil {
		t.Fatalf("结论收尾不应挂起: %+v", res)
	}
	// 续跑请求 = 挂起前序列 + assistant(tool_calls) + tool(ask 结果) + 用户回复原话
	if len(sc2.last) != len(suspended)+3 {
		t.Fatalf("续跑消息序列长度 = %d, want %d", len(sc2.last), len(suspended)+3)
	}
	for i, m := range suspended {
		if sc2.last[i].Role != m.Role || sc2.last[i].Content != m.Content {
			t.Fatalf("续跑消息[%d] 与挂起前不一致: got %+v want %+v", i, sc2.last[i], m)
		}
	}
	askTool := sc2.last[len(suspended)+1]
	if askTool.Role != "tool" || !strings.Contains(askTool.Content, "发给哪个") {
		t.Fatalf("挂起前的 ask 工具结果应原样延续: %+v", askTool)
	}
	reply := sc2.last[len(sc2.last)-1]
	if reply.Role != "user" || reply.Content != "1" {
		t.Fatalf("续跑应追加用户回复原话: %+v", reply)
	}
}

// 测试 3（新）：create_session 缺参数 → NeedsConfirm → ask 挂起（候选语境
// 挂在挂起态上）；Resume 回编号 → Candidates 翻译 → 创建落地。
func TestRunLoopCreateNeedsConfirmResume(t *testing.T) {
	api, f := newLoopAPI(t)

	sc1 := &scriptChat{resps: []hubtools.Response{
		{ToolCalls: []hubtools.ToolCall{tc("c1", "create_session", `{"task":"写周报"}`)}},
		{ToolCalls: []hubtools.ToolCall{tc("c2", "ask", `{"question":"请选择工具","options":["claude Claude Code","codex Codex"]}`)}},
	}}
	chatToolsHook = sc1.hook
	res1, err := api.Run(context.Background(), "新建会话写周报", RunOpts{Channel: "hub"})
	if err != nil {
		t.Fatalf("Run1: %v", err)
	}
	if !res1.Suspended || res1.Ask == nil {
		t.Fatalf("NeedsConfirm + ask 后应挂起: %+v", res1)
	}
	if !strings.Contains(res1.Replies[0], "请选择工具") || !strings.Contains(res1.Replies[0], "1. claude") {
		t.Fatalf("Replies[0] 缺问题或编号选项: %q", res1.Replies[0])
	}
	s := api.getSuspend()
	if s == nil || s.st.confirm == nil || s.st.candidates[1] != "claude" || s.st.candidates[2] != "codex" {
		t.Fatalf("挂起态候选语境缺失: %+v", s)
	}
	if s.st.askRound != 1 {
		t.Fatalf("首轮 ask askRound = %d, want 1", s.st.askRound)
	}

	sc2 := &scriptChat{resps: []hubtools.Response{
		{ToolCalls: []hubtools.ToolCall{tc("c3", "create_session", `{"tool_id":"1","workspace":"d:/ws/a","task":"写周报"}`)}},
		{Text: "已创建 claude 会话。"},
	}}
	chatToolsHook = sc2.hook
	res2, err := api.Resume(context.Background(), "hub", "1", RunOpts{})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if res2.Suspended {
		t.Fatal("创建落地后不应挂起")
	}
	if len(f.created) != 1 || f.created[0] != [3]string{"claude", "d:/ws/a", ""} {
		t.Fatalf("编号未翻译，f.created = %v", f.created)
	}
	if len(f.sent) != 1 || f.sent[0] != "chat-new|写周报" {
		t.Fatalf("首条任务未投递: %v", f.sent)
	}
	if api.HasSuspendedAsk("hub") {
		t.Fatal("结论落地后挂起应清除")
	}
}

// 测试 4（新）：ask 挂起后数字短输入 Run 自动续跑（无需显式标志）。
func TestRunLoopAskAndShortInputAutoResume(t *testing.T) {
	api, _ := newLoopAPI(t)
	sc1 := askScript("选哪个？")
	chatToolsHook = sc1.hook
	res1, err := api.Run(context.Background(), "看着办", RunOpts{Channel: "hub"})
	if err != nil {
		t.Fatalf("Run1: %v", err)
	}
	if !res1.Suspended {
		t.Fatalf("ask 未挂起: %+v", res1)
	}

	sc2 := &scriptChat{resps: []hubtools.Response{{Text: "好的，按新建处理。"}}}
	chatToolsHook = sc2.hook
	res2, err := api.Run(context.Background(), "2", RunOpts{Channel: "hub"})
	if err != nil {
		t.Fatalf("Run2: %v", err)
	}
	if res2.Suspended {
		t.Fatal("续轮给结论后不应挂起")
	}
	// 续跑消息序列含挂起前全部消息（+ask 的 assistant/tool 结果）+ 用户短回复
	if len(sc2.last) != len(sc1.last)+3 {
		t.Fatalf("自动续跑消息序列长度 = %d, want %d", len(sc2.last), len(sc1.last)+3)
	}
	if sc2.last[len(sc2.last)-1].Content != "2" {
		t.Fatalf("续跑应追加用户回复: %+v", sc2.last[len(sc2.last)-1])
	}
	if api.HasSuspendedAsk("hub") {
		t.Fatal("结论落地后挂起应清除")
	}
}

// 测试 5（新）：AskMaxRounds 门禁——挂起链内 3 连问放行，第 4 问被拒：
// 清挂起 + 结构化指引（含上限与上一问），正常返回不挂起。
func TestRunLoopAskMaxRounds(t *testing.T) {
	api, _ := newLoopAPI(t)

	// 三连问均在门禁内（每次短输入自动续跑）
	for i, input := range []string{"起点", "a", "b"} {
		chatToolsHook = askScript("还要什么？").hook
		res, err := api.Run(context.Background(), input, RunOpts{Channel: "hub"})
		if err != nil {
			t.Fatalf("第 %d 问 Run: %v", i+1, err)
		}
		if !res.Suspended {
			t.Fatalf("第 %d 问应挂起: %+v", i+1, res)
		}
	}
	// 第四问被拒
	chatToolsHook = askScript("还要什么？").hook
	res, err := api.Run(context.Background(), "c", RunOpts{Channel: "hub"})
	if err != nil {
		t.Fatalf("第 4 问 Run: %v", err)
	}
	if res.Suspended || res.Ask != nil {
		t.Fatalf("超限后不应挂起: %+v", res)
	}
	if !strings.Contains(res.Replies[0], fmt.Sprintf("连续反问已达上限（%d）", hubtools.AskMaxRounds)) {
		t.Fatalf("指引缺上限说明: %q", res.Replies[0])
	}
	if !strings.Contains(res.Replies[0], "刚才的问题是：还要什么？") {
		t.Fatalf("指引缺上一问原文: %q", res.Replies[0])
	}
	if api.HasSuspendedAsk("hub") {
		t.Fatal("超限拒绝后挂起应清除")
	}
}

// 测试 6（新）：挂起超时——Resume 返回 ErrNoSuspend 且挂起被清；之后的
// 短输入 Run 按全新循环跑（不携带旧语境）。
func TestSuspendTimeout(t *testing.T) {
	api, _ := newLoopAPI(t)
	advance := fakeClock(t, time.Now())

	chatToolsHook = askScript("选哪个？").hook
	if _, err := api.Run(context.Background(), "看着办", RunOpts{Channel: "hub"}); err != nil {
		t.Fatalf("Run1: %v", err)
	}
	advance(SuspendTTL + time.Minute)
	if api.HasSuspendedAsk("hub") {
		t.Fatal("超时后不应报告挂起在场")
	}
	_, err := api.Resume(context.Background(), "hub", "1", RunOpts{})
	if !errors.Is(err, ErrNoSuspend) {
		t.Fatalf("超时期望 ErrNoSuspend, got %v", err)
	}
	if api.getSuspend() != nil {
		t.Fatal("超时 Resume 应清挂起")
	}
	sc2 := &scriptChat{resps: []hubtools.Response{{Text: "好的"}}}
	chatToolsHook = sc2.hook
	if _, err := api.Run(context.Background(), "1", RunOpts{Channel: "hub"}); err != nil {
		t.Fatalf("Run2: %v", err)
	}
	if len(sc2.last) != 2 {
		t.Fatalf("超时后短输入应按全新循环跑（[sys,user]）: len=%d", len(sc2.last))
	}
}

// 测试 7（新）：挂起在场 + 长输入 → 清挂起按全新输入跑（新 system、
// round=1，消息序列不带旧语境）。
func TestRunLongInputClearsSuspend(t *testing.T) {
	api, _ := newLoopAPI(t)
	chatToolsHook = askScript("选哪个？").hook
	if _, err := api.Run(context.Background(), "看着办", RunOpts{Channel: "hub"}); err != nil {
		t.Fatalf("Run1: %v", err)
	}
	sc2 := &scriptChat{resps: []hubtools.Response{{Text: "结论"}}}
	chatToolsHook = sc2.hook
	long := strings.Repeat("长", shortInputMax+1)
	if _, err := api.Run(context.Background(), long, RunOpts{Channel: "hub"}); err != nil {
		t.Fatalf("Run2: %v", err)
	}
	if len(sc2.last) != 2 {
		t.Fatalf("长输入应全新开循环（[sys,user]）: len=%d", len(sc2.last))
	}
	if api.HasSuspendedAsk("hub") {
		t.Fatal("长输入应清挂起")
	}
}

// 测试 8（新）：mode 三态落地——multi/confirm 原样进载荷，非法值归一 single。
func TestAskModeVariants(t *testing.T) {
	cases := []struct {
		mode string
		want string
	}{
		{"multi", "multi"},
		{"confirm", "confirm"},
		{"bogus", "single"},
	}
	for _, c := range cases {
		api, _ := newLoopAPI(t)
		args := fmt.Sprintf(`{"question":"选哪个？","options":["a"],"mode":%q}`, c.mode)
		chatToolsHook = (&scriptChat{resps: []hubtools.Response{
			{ToolCalls: []hubtools.ToolCall{tc("c", "ask", args)}},
		}}).hook
		res, err := api.Run(context.Background(), "看着办", RunOpts{Channel: "hub"})
		if err != nil {
			t.Fatalf("mode=%s Run: %v", c.mode, err)
		}
		if res.Ask == nil || res.Ask.Mode != c.want {
			t.Fatalf("mode=%s 应归一为 %s: %+v", c.mode, c.want, res.Ask)
		}
	}
}

// 测试 9（新）：Resume 瞬时错误 → 挂起保留（候选语境不丢），重试同一输入
// 可续且不产生重复的 user 回复；ErrLLMNotReady 同样保留挂起且不写记忆。
func TestResumeTransientErrorKeepsSuspend(t *testing.T) {
	api, _ := newLoopAPI(t)
	chatToolsHook = askScript("选哪个？").hook
	if _, err := api.Run(context.Background(), "看着办", RunOpts{Channel: "hub"}); err != nil {
		t.Fatalf("Run1: %v", err)
	}

	// 瞬时错误：挂起保留
	chatToolsHook = (&scriptChat{err: errors.New("err.hubtools.request_failed: 状态码 500, 响应: oops")}).hook
	_, err := api.Resume(context.Background(), "hub", "1", RunOpts{})
	if err == nil || !strings.Contains(err.Error(), "状态码 500") {
		t.Fatalf("瞬时错误应上抛, got %v", err)
	}
	if !api.HasSuspendedAsk("hub") {
		t.Fatal("瞬时错误应保留挂起")
	}

	// 重试同一输入：续跑成功，且 msgs 里只有一条 user 回复（无重复）
	sc2 := &scriptChat{resps: []hubtools.Response{{Text: "已按 1 处理。"}}}
	chatToolsHook = sc2.hook
	if _, err := api.Resume(context.Background(), "hub", "1", RunOpts{}); err != nil {
		t.Fatalf("重试 Resume: %v", err)
	}
	dup := 0
	for _, m := range sc2.last {
		if m.Role == "user" && m.Content == "1" {
			dup++
		}
	}
	if dup != 1 {
		t.Fatalf("重试后 user 回复应只出现一次, got %d", dup)
	}

	// ErrLLMNotReady：保留挂起且不写记忆
	chatToolsHook = askScript("再问一次").hook
	if _, err := api.Run(context.Background(), "b", RunOpts{Channel: "hub"}); err != nil {
		t.Fatalf("Run2: %v", err)
	}
	api.UpdateLLM(hubtools.Config{})
	_, err = api.Resume(context.Background(), "hub", "b", RunOpts{})
	if !errors.Is(err, ErrLLMNotReady) {
		t.Fatalf("未配置 LLM 期望 ErrLLMNotReady, got %v", err)
	}
	if !api.HasSuspendedAsk("hub") {
		t.Fatal("ErrLLMNotReady 应保留挂起")
	}
}

// 测试 10（新）：无挂起时 Resume 返回 ErrNoSuspend。
func TestResumeNoSuspend(t *testing.T) {
	api, _ := newLoopAPI(t)
	if _, err := api.Resume(context.Background(), "hub", "1", RunOpts{}); !errors.Is(err, ErrNoSuspend) {
		t.Fatalf("无挂起期望 ErrNoSuspend, got %v", err)
	}
}

// 测试 11（新）：跨渠道输入清挂起按全新跑；同渠道短输入照常自动续跑。
func TestSuspendChannelGuard(t *testing.T) {
	api, _ := newLoopAPI(t)
	sc := &scriptChat{resps: []hubtools.Response{{Text: "结论"}, {Text: "结论"}}}

	// hub 渠道的挂起在场，wechat 输入：不得携带 hub 语境，且清挂起。
	chatToolsHook = askScript("发给哪个？").hook
	if _, err := api.Run(context.Background(), "看着办", RunOpts{Channel: "hub"}); err != nil {
		t.Fatalf("Run1: %v", err)
	}
	chatToolsHook = sc.hook
	if _, err := api.Run(context.Background(), "1", RunOpts{Channel: "wechat"}); err != nil {
		t.Fatalf("Run2: %v", err)
	}
	if len(sc.last) != 2 {
		t.Fatalf("跨渠道输入应全新开循环: len=%d", len(sc.last))
	}
	if api.HasSuspendedAsk("hub") {
		t.Fatal("跨渠道输入应清挂起")
	}

	// 同渠道短输入自动续跑（挂起态整体延续，msgs 不带旧语境的断言见测试 4）。
	chatToolsHook = askScript("发给哪个？").hook
	if _, err := api.Run(context.Background(), "看着办", RunOpts{Channel: "hub"}); err != nil {
		t.Fatalf("Run3: %v", err)
	}
	sc2 := &scriptChat{resps: []hubtools.Response{{Text: "结论"}}}
	chatToolsHook = sc2.hook
	if _, err := api.Run(context.Background(), "1", RunOpts{Channel: "hub"}); err != nil {
		t.Fatalf("Run4: %v", err)
	}
	if len(sc2.last) <= 2 {
		t.Fatalf("同渠道短输入应自动续跑: len=%d", len(sc2.last))
	}
}

// 测试 12（重写）：续跑首轮 chatTools 4xx 应上抛错误，不进 RouteDecide。
// RouteDecide 没有上一轮问题与候选语境（用户短回复如 "1" 进去决策不可控）；
// 挂起与候选语境在错误后保留，用户重试可续。
func TestRunLoopDegradeSkipResume(t *testing.T) {
	api, f := newLoopAPI(t)

	sc1 := &scriptChat{resps: []hubtools.Response{
		{ToolCalls: []hubtools.ToolCall{tc("c1", "create_session", `{"task":"写周报"}`)}},
		{ToolCalls: []hubtools.ToolCall{tc("c2", "ask", `{"question":"请选择工具","options":["claude Claude Code"]}`)}},
	}}
	chatToolsHook = sc1.hook
	if _, err := api.Run(context.Background(), "新建会话写周报", RunOpts{Channel: "hub"}); err != nil {
		t.Fatalf("Run1: %v", err)
	}
	s := api.getSuspend()
	if s == nil || s.st.confirm == nil || s.st.candidates[1] != "claude" {
		t.Fatalf("第一轮后挂起候选语境缺失: %+v", s)
	}

	// 续跑：chatTools 直接 4xx → 应上抛错误，不进 RouteDecide
	chatToolsHook = func(context.Context, hubtools.Config, []hubtools.Message, []hubtools.ToolFunc) (hubtools.Response, error) {
		return hubtools.Response{}, errors.New("err.hubtools.request_failed: 状态码 400, 响应: bad request")
	}
	api.setChatFn(func(context.Context, []hubtools.Message) (hubtools.Response, error) {
		// 降级若误触发会走这里。
		t.Error("续跑不应触发降级 RouteDecide")
		return hubtools.Response{Text: `{"action":"single","targets":[1],"text":"误投"}`}, nil
	})

	_, err := api.Resume(context.Background(), "hub", "1", RunOpts{})
	if err == nil || !strings.Contains(err.Error(), "状态码 400") {
		t.Fatalf("续跑 4xx 应上抛错误, got %v", err)
	}
	// 挂起与候选语境未被降级路径破坏（瞬时错误保留挂起）
	s = api.getSuspend()
	if s == nil || s.st.confirm == nil || s.st.candidates[1] != "claude" {
		t.Fatalf("续跑错误后挂起候选语境应保留: %+v", s)
	}
	// 无 RouteSend 副作用
	if len(f.sent) != 0 {
		t.Fatalf("续跑错误不应产生投递: %v", f.sent)
	}
}

// 测试 13（新）：降级 ask 分支同样挂起（msgs=[system,user]），下一条短输入
// 能从同一语境续跑。
func TestRunLoopDegradeAskSuspends(t *testing.T) {
	api, _ := newLoopAPI(t)
	chatToolsHook = func(context.Context, hubtools.Config, []hubtools.Message, []hubtools.ToolFunc) (hubtools.Response, error) {
		return hubtools.Response{}, errors.New("err.hubtools.request_failed: 状态码 400, 响应: bad request")
	}
	api.setChatFn(func(context.Context, []hubtools.Message) (hubtools.Response, error) {
		return hubtools.Response{Text: `{"action":"ask","question":"发给哪个会话？"}`}, nil
	})

	res, err := api.Run(context.Background(), "发一下进度", RunOpts{Channel: "hub"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Suspended || res.Ask == nil || res.Ask.Question != "发给哪个会话？" {
		t.Fatalf("降级反问应挂起: %+v", res)
	}
	if !api.HasSuspendedAsk("hub") {
		t.Fatal("降级反问应记挂起")
	}

	// 短输入续跑：chatTools 恢复 → 结论；msgs = 降级时的 [sys,user] + 回复
	sc2 := &scriptChat{resps: []hubtools.Response{{Text: "已发给修复登录。"}}}
	chatToolsHook = sc2.hook
	if _, err := api.Run(context.Background(), "1", RunOpts{Channel: "hub"}); err != nil {
		t.Fatalf("Run2: %v", err)
	}
	if len(sc2.last) != 3 {
		t.Fatalf("降级续跑消息序列长度 = %d, want 3", len(sc2.last))
	}
}

// 测试：挂起返回也写记忆，outcome 记「反问：<question>」。
func TestMemorySuspendOutcome(t *testing.T) {
	api, _ := newLoopAPI(t)
	chatToolsHook = askScript("选哪个？").hook
	if _, err := api.Run(context.Background(), "看着办", RunOpts{Channel: "hub"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := api.memoryNow().Recent(1)
	if len(got) != 1 || !strings.Contains(got[0].Outcome, "反问：选哪个？") {
		t.Fatalf("挂起返回的记忆 outcome 应记反问: %+v", got)
	}
}

// 测试：记忆——首轮后 journal 有条目；第二轮 system 含记忆尾；search_memory 命中。
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

// 测试：ChatWithTools 4xx → RouteDecide 降级单发生效。
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

// 测试：mid-loop 4xx 不降级——第 1 轮 send 已真实投递，第 2 轮 chatTools
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

// 测试：ErrLoopBusy 并发重入；ErrLLMNotReady 原样上抛。
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

// 测试：轮次守卫——模型连调 6 轮工具后超限，最后一轮仍无结论返回指引文案。
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
	if res2.Suspended || res2.Replies[0] != loopGuidance {
		t.Fatalf("超限应返回指引文案: %+v", res2)
	}
}

// 最近 AgentHub 消息注入：标签归因、旧→新顺序、本轮 input 回显去重。
func TestManagerSystemInjectsRecentHubMessages(t *testing.T) {
	api, f := newLoopAPI(t)
	f.recent = []Message{
		{ID: 1, Kind: "user", SessionID: "chat-a", SessionTitle: "修复登录", Text: "继续修一下"},
		{ID: 2, Kind: "agent", SessionID: "chat-a", SessionTitle: "修复登录", Text: "改完了，要合并吗？"},
		{ID: 3, Kind: "user", SessionID: "chat-b", SessionTitle: "文档", Text: "补 README", ViaHub: true},
		{ID: 4, Kind: "agent", Event: "hub_manager_reply", Text: "已投递到「修复登录」"},
		{ID: 5, Kind: "user", Text: "继续刚才那个"}, // 本轮 input 的回显
	}
	sc := &scriptChat{resps: []hubtools.Response{{Text: "好的"}}}
	chatToolsHook = sc.hook
	if _, err := api.Run(context.Background(), "继续刚才那个", RunOpts{Channel: "hub"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	sys := sc.sysSeen[0]
	for _, want := range []string{
		"【最近 AgentHub 消息】",
		"用户→「修复登录」",
		"管理者代发→「文档」",
		"不要再调用 session_messages 或 search_memory",
	} {
		if !strings.Contains(sys, want) {
			t.Fatalf("system 缺少 %q:\n%s", want, sys)
		}
	}
	if strings.Contains(sys, "继续刚才那个") {
		t.Fatalf("本轮 input 回显应从最近消息段丢弃:\n%s", sys)
	}
	// 旧→新：第一条用户消息在会话回复之前。
	if strings.Index(sys, "继续修一下") > strings.Index(sys, "改完了") {
		t.Fatalf("最近消息应按时间正序:\n%s", sys)
	}
}

// 最近消息单条截断、空正文跳过、无历史不出段。
func TestRecentHubContextTruncateAndEmpty(t *testing.T) {
	long := strings.Repeat("长", 300)
	api, f := newLoopAPI(t)
	f.recent = []Message{
		{ID: 1, Kind: "user", Text: long},
		{ID: 2, Kind: "agent", Text: "   "},
	}
	sec := api.recentContextSection("新输入")
	if !strings.Contains(sec, "…") {
		t.Fatalf("超长正文应截断并带省略号: %q", sec)
	}
	if strings.Contains(sec, long) {
		t.Fatal("超长正文不应整段注入")
	}
	if strings.Contains(sec, "[会话]") {
		t.Fatalf("空正文消息应跳过: %q", sec)
	}

	f.recent = nil
	if got := api.recentContextSection("新输入"); got != "" {
		t.Fatalf("无历史不应输出段: %q", got)
	}
}

// 可见历史超过条数上限时只渲染最后 6 条（多取的一条仅用于回显去重）。
func TestRecentHubContextKeepsLastSix(t *testing.T) {
	api, f := newLoopAPI(t)
	for i := 1; i <= 8; i++ {
		f.recent = append(f.recent, Message{ID: int64(i), Kind: "user", Text: fmt.Sprintf("消息%d", i)})
	}
	sec := api.recentContextSection("新输入")
	if strings.Contains(sec, "消息2") || !strings.Contains(sec, "消息3") || !strings.Contains(sec, "消息8") {
		t.Fatalf("应只保留最后 6 条（消息3..8）:\n%s", sec)
	}
	if !strings.Contains(sec, "6. [用户] 消息8") {
		t.Fatalf("编号应按 1..6 重新连续:\n%s", sec)
	}
}

// 末尾代发消息即使文本等于本轮 input 也必须保留（只有非代发的用户回显才丢）。
func TestRecentHubContextKeepsViaHubEcho(t *testing.T) {
	api, f := newLoopAPI(t)
	f.recent = []Message{{Kind: "user", ViaHub: true, SessionTitle: "甲", Text: "继续"}}
	sec := api.recentContextSection("继续")
	if !strings.Contains(sec, "管理者代发→「甲」") || !strings.Contains(sec, "继续") {
		t.Fatalf("代发消息不应被回显去重误删:\n%s", sec)
	}
}

// 全部空正文消息：段整体不输出。
func TestRecentHubContextAllBlank(t *testing.T) {
	api, f := newLoopAPI(t)
	f.recent = []Message{{Kind: "user", Text: " "}, {Kind: "agent", Text: "\n"}}
	if got := api.recentContextSection("x"); got != "" {
		t.Fatalf("全空正文不应输出段: %q", got)
	}
}

// hub 与 wechat 两个渠道都注入同一份最近消息语境。
func TestManagerSystemRecentContextBothChannels(t *testing.T) {
	api, f := newLoopAPI(t)
	f.recent = []Message{{ID: 1, Kind: "user", Text: "上一条"}}
	sc := &scriptChat{resps: []hubtools.Response{{Text: "结论"}, {Text: "结论"}}}
	chatToolsHook = sc.hook
	for i, ch := range []string{"hub", "wechat"} {
		if _, err := api.Run(context.Background(), "下一条", RunOpts{Channel: ch}); err != nil {
			t.Fatalf("%s Run: %v", ch, err)
		}
		if !strings.Contains(sc.sysSeen[i], "【最近 AgentHub 消息】") {
			t.Fatalf("%s 渠道应注入最近消息段:\n%s", ch, sc.sysSeen[i])
		}
	}
}
