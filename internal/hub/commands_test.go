package hub

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// reBareCmd 匹配行首裸命令词（Markdown 里会被解析成标题）。
var reBareCmd = regexp.MustCompile(`(?m)^#[a-z?？]`)

// fmtS 整数转字符串的简写。
func fmtS(n int) string { return strconv.Itoa(n) }

// fakeExecutor 是 Executor 的可配置假实现：各方法返回值/错误可注入，
// 并记录关键调用参数供断言。
type fakeExecutor struct {
	// 可配置返回值
	listSessions []SessionRef
	// lastActivityID 是假后端最后一条可归因 hub 消息的 ID（Console 水位比较用）；
	// lastActivityRef/hasActivity 模拟精确命中的会话。
	lastActivityID  int64
	lastActivityRef SessionRef
	hasActivity     bool
	switchErr       error
	easyNewRef      SessionRef
	easyNewErr      error
	tools           []MenuOption
	workspaces      []MenuOption
	models          []MenuOption
	createRef       SessionRef
	createErr       error
	stopErr         error
	listModels      []MenuOption
	listModelsCur   string
	listModelsErr   error
	setModelErr     error
	searchResult    []SessionRef
	sendErr         error
	grantErr        error
	status          ConsoleStatus
	openSessionRef  SessionRef
	openSessionErr  error
	recentMessages  []Message
	// 记录调用参数
	switchCalls      []string
	openSessionCalls []string
	createCalls      [][3]string // tool, ws, model
	stopCalls        []string
	setModelCalls    [][2]string
	sendCalls        [][2]string // chatID, text
	grantCalls       [][3]string // chatID, reqID, allow
}

func (f *fakeExecutor) ListSessions() []SessionRef { return f.listSessions }

func (f *fakeExecutor) LastActivity() (int64, SessionRef, bool) {
	return f.lastActivityID, f.lastActivityRef, f.hasActivity
}

func (f *fakeExecutor) SwitchTo(chatID string) error {
	f.switchCalls = append(f.switchCalls, chatID)
	return f.switchErr
}

func (f *fakeExecutor) OpenSession(sessionID string) (SessionRef, error) {
	f.openSessionCalls = append(f.openSessionCalls, sessionID)
	if f.openSessionErr != nil {
		return SessionRef{}, f.openSessionErr
	}
	return f.openSessionRef, nil
}

func (f *fakeExecutor) EasyNew() (SessionRef, error) { return f.easyNewRef, f.easyNewErr }

func (f *fakeExecutor) NewMenuOptions() (tools, workspaces, models []MenuOption) {
	return f.tools, f.workspaces, f.models
}

func (f *fakeExecutor) CreateNew(toolID, wsID, model string) (SessionRef, error) {
	f.createCalls = append(f.createCalls, [3]string{toolID, wsID, model})
	return f.createRef, f.createErr
}

func (f *fakeExecutor) Stop(chatID string) error {
	f.stopCalls = append(f.stopCalls, chatID)
	return f.stopErr
}

func (f *fakeExecutor) ListModels(chatID string) ([]MenuOption, string, error) {
	return f.listModels, f.listModelsCur, f.listModelsErr
}

func (f *fakeExecutor) SetModel(chatID, model string) error {
	f.setModelCalls = append(f.setModelCalls, [2]string{chatID, model})
	return f.setModelErr
}

func (f *fakeExecutor) SearchSessions(kw string) []SessionRef { return f.searchResult }

func (f *fakeExecutor) Send(chatID, text string) error {
	f.sendCalls = append(f.sendCalls, [2]string{chatID, text})
	return f.sendErr
}

func (f *fakeExecutor) GrantPermission(chatID, reqID string, allow bool) error {
	allowStr := "false"
	if allow {
		allowStr = "true"
	}
	f.grantCalls = append(f.grantCalls, [3]string{chatID, reqID, allowStr})
	return f.grantErr
}

func (f *fakeExecutor) Status() ConsoleStatus { return f.status }

func (f *fakeExecutor) RecentMessages(n int) []Message {
	return VisibleMessages(f.recentMessages, n)
}

// join 把多条回复拼成一段文本，便于断言。
func join(replies []Reply) string {
	var sb strings.Builder
	for i, r := range replies {
		if i > 0 {
			sb.WriteString("\n---\n")
		}
		sb.WriteString(r.Text)
	}
	return sb.String()
}

// wsRef 构造一个测试会话。
func wsRef(num int, chatID, sessionID, ws, tool, title string, current bool) SessionRef {
	return SessionRef{Num: num, ChatID: chatID, SessionID: sessionID, Workspace: ws, Tool: tool, Title: title, Current: current}
}

// —— HandleQuoted：引用回复按 chatID 定位会话，切换后投递 ——

func TestHandleQuoted(t *testing.T) {
	sessions := []SessionRef{
		{ChatID: "c1", Tool: "codex", Title: "会话A"},
		{ChatID: "", SessionID: "s2", Tool: "claude", Title: "未激活"}, // 未激活：不在引用命中范围
	}

	t.Run("命中切换并投递", func(t *testing.T) {
		ex := &fakeExecutor{listSessions: sessions, hasActivity: true, lastActivityRef: SessionRef{ChatID: "other"}}
		c := NewConsole(ex)
		handled, replies := c.HandleQuoted("c1", "继续修这个 bug")
		if !handled || len(replies) != 0 {
			t.Fatalf("handled = %v, replies = %v, 期望 true + 静默", handled, replies)
		}
		if len(ex.switchCalls) != 1 || ex.switchCalls[0] != "c1" {
			t.Errorf("应先 SwitchTo(c1): %v", ex.switchCalls)
		}
		if len(ex.sendCalls) != 1 || ex.sendCalls[0] != [2]string{"c1", "继续修这个 bug"} {
			t.Errorf("投递目标错误: %v", ex.sendCalls)
		}
	})

	t.Run("chatID 未命中 handled=false", func(t *testing.T) {
		ex := &fakeExecutor{listSessions: sessions}
		c := NewConsole(ex)
		handled, replies := c.HandleQuoted("nope", "文本")
		if handled || replies != nil {
			t.Errorf("未命中应 (false, nil) 走普通路径, 实际 (%v, %v)", handled, replies)
		}
		if len(ex.sendCalls) != 0 || len(ex.switchCalls) != 0 {
			t.Errorf("未命中不应投递/切换: send=%v switch=%v", ex.sendCalls, ex.switchCalls)
		}
	})

	t.Run("未激活会话不命中", func(t *testing.T) {
		ex := &fakeExecutor{listSessions: sessions}
		c := NewConsole(ex)
		if handled, _ := c.HandleQuoted("s2", "文本"); handled {
			t.Error("SessionID 不应被 chatID 命中")
		}
	})

	t.Run("切换失败报错不投递", func(t *testing.T) {
		ex := &fakeExecutor{listSessions: sessions, switchErr: errors.New("激活失败")}
		c := NewConsole(ex)
		handled, replies := c.HandleQuoted("c1", "文本")
		if !handled || len(replies) != 1 || !strings.Contains(replies[0].Text, "激活失败") {
			t.Fatalf("切换失败应 (true, 错误回执), 实际 (%v, %v)", handled, replies)
		}
		if len(ex.sendCalls) != 0 {
			t.Errorf("切换失败不应投递: %v", ex.sendCalls)
		}
	})

	t.Run("活跃菜单进行中不劫持", func(t *testing.T) {
		// #f 搜索出结果后菜单挂着：引用回复应提示选数字，不切会话不投递。
		ex := &fakeExecutor{listSessions: sessions, searchResult: sessions}
		c := NewConsole(ex)
		c.Handle("#f 会话")
		handled, replies := c.HandleQuoted("c1", "继续")
		if !handled || len(replies) != 1 || !strings.Contains(replies[0].Text, "请回复数字") && !strings.Contains(replies[0].Text, "pick_number") {
			t.Fatalf("菜单进行中应回 pick_number 提示, 实际 (%v, %v)", handled, replies)
		}
		if len(ex.sendCalls) != 0 || len(ex.switchCalls) != 0 {
			t.Errorf("菜单进行中不应投递/切换: send=%v switch=%v", ex.sendCalls, ex.switchCalls)
		}
	})

	t.Run("发送失败报错", func(t *testing.T) {
		ex := &fakeExecutor{listSessions: sessions, hasActivity: true, lastActivityRef: SessionRef{ChatID: "other"}, sendErr: errors.New("发送失败")}
		c := NewConsole(ex)
		handled, replies := c.HandleQuoted("c1", "文本")
		if !handled || len(replies) != 1 || !strings.Contains(replies[0].Text, "发送失败") {
			t.Fatalf("发送失败应 (true, 错误回执), 实际 (%v, %v)", handled, replies)
		}
	})
}

func TestHelpWithCurrent(t *testing.T) {
	c := NewConsole(&fakeExecutor{
		hasActivity:     true,
		lastActivityRef: wsRef(0, "c1", "", `D:\work\demo`, "claude", "修复登录", true),
	})
	out := join(c.Handle("#?"))
	if !strings.Contains(out, "**命令列表**") || !strings.Contains(out, "- #l") || !strings.Contains(out, "#st") {
		t.Fatalf("帮助应含 Markdown 命令表: %s", out)
	}
	if !strings.Contains(out, "当前会话: 修复登录 (claude)") {
		t.Fatalf("帮助应含当前会话行: %s", out)
	}
	// Markdown 化关键约束：命令词不得出现在行首（会被 ReactMarkdown 解析为
	// 标题），必须以列表项形式出现。
	if reBareCmd.MatchString(out) {
		t.Fatalf("帮助不得有行首裸命令（会渲染成标题）: %s", out)
	}
}

func TestHelpWithoutCurrent(t *testing.T) {
	c := NewConsole(&fakeExecutor{})
	out := join(c.Handle("#?"))
	if !strings.Contains(out, "当前会话: 无") {
		t.Fatalf("无当前会话时帮助应显示「无」: %s", out)
	}
}

func TestListTruncatesTo10AndMarksCurrent(t *testing.T) {
	var refs []SessionRef
	for i := 1; i <= 12; i++ {
		refs = append(refs, wsRef(0, "", "sid"+fmtS(i), `D:\ws\repo`, "claude", "会话"+fmtS(i), false))
	}
	refs[2].Current = true
	refs[2].ChatID = "chat-3"
	fe := &fakeExecutor{
		listSessions:    refs,
		hasActivity:     true,
		lastActivityRef: refs[2], // ▶ 由 refreshCurrent 与归因 ChatID 比对得出
	}
	c := NewConsole(fe)

	out := join(c.Handle("#l"))
	lines := strings.Split(out, "\n")
	// 10 行列表 + 空行 + 末尾 1 行「回复数字可切换」引导提示（空行分隔防
	// Markdown 懒续行把提示吞进列表项）。
	if len(lines) != 12 {
		t.Fatalf("应只列前 10 个加空行加引导提示，得到 %d 行: %q", len(lines), out)
	}
	if lines[10] != "" || lines[11] != "回复数字可切换到对应会话" {
		t.Fatalf("末尾应空行+引导提示: %q", lines[10:])
	}
	if !strings.Contains(lines[2], "3. ▶") {
		t.Fatalf("第 3 行应在编号后标 ▶: %q", lines[2])
	}
	if !strings.Contains(lines[0], "1. [repo] claude 会话1") {
		t.Fatalf("首行应含编号、工作区末段与标题: %q", lines[0])
	}

	// 编号映射：纯数字 3 应切换到 chat-3
	c.Handle("3")
	if len(fe.switchCalls) != 1 || fe.switchCalls[0] != "chat-3" {
		t.Fatalf("纯数字 3 应触发 SwitchTo(chat-3)，实际: %v", fe.switchCalls)
	}
}

func TestListRefreshesCurrent(t *testing.T) {
	fe := &fakeExecutor{
		listSessions:    []SessionRef{wsRef(0, "c1", "", `D:\a\b`, "codex", "标题", false)},
		hasActivity:     true,
		lastActivityRef: wsRef(0, "c1", "", `D:\a\b`, "codex", "标题", true),
	}
	c := NewConsole(fe)
	out := join(c.Handle("#l"))
	// c1 是当前会话，应标 ▶（由 #l 内 refreshCurrent 比对 ChatID 得出）。
	if !strings.HasPrefix(out, "1. ▶ [b] codex 标题") {
		t.Fatalf("列表行格式不符: %q", out)
	}
	// current 被刷新：#? 应显示
	out = join(c.Handle("#?"))
	if !strings.Contains(out, "当前会话: 标题 (codex)") {
		t.Fatalf("#l 后 current 应被刷新: %s", out)
	}
}

func TestListEmpty(t *testing.T) {
	c := NewConsole(&fakeExecutor{})
	if got := join(c.Handle("#l")); got != "暂无会话" {
		t.Fatalf("空列表应回「暂无会话」: %q", got)
	}
}

func TestListAll(t *testing.T) {
	var refs []SessionRef
	for i := 1; i <= 12; i++ {
		refs = append(refs, wsRef(0, "", fmtS(i), "", "gemini", "会话"+fmtS(i), false))
	}
	c := NewConsole(&fakeExecutor{listSessions: refs})
	lines := strings.Split(join(c.Handle("#la")), "\n")
	if len(lines) != 14 { // 12 个会话 + 空行 + 末尾引导提示
		t.Fatalf("#la 应列全量 12 个加引导提示，得到 %d 行", len(lines))
	}
}

func TestPureNumberNoMapping(t *testing.T) {
	c := NewConsole(&fakeExecutor{})
	if got := join(c.Handle("5")); !strings.Contains(got, "编号不存在") {
		t.Fatalf("无映射数字应提示编号不存在: %q", got)
	}
}

func TestSwitchFailure(t *testing.T) {
	fe := &fakeExecutor{
		listSessions: []SessionRef{wsRef(0, "c1", "", "", "claude", "A", false)},
		switchErr:    errors.New("chat closed"),
	}
	c := NewConsole(fe)
	c.Handle("#l")
	out := join(c.Handle("1"))
	if !strings.Contains(out, "切换失败") {
		t.Fatalf("SwitchTo 失败应回切换失败: %q", out)
	}
}

func TestEasyNew(t *testing.T) {
	fe := &fakeExecutor{
		easyNewRef: wsRef(0, "new1", "", `D:\x\proj`, "claude", "新会话", false),
	}
	c := NewConsole(fe)
	out := join(c.Handle("#en"))
	if !strings.Contains(out, "【新会话】你好！我是 claude，会话已就绪") {
		t.Fatalf("EasyNew 成功文案不符: %q", out)
	}
	// current 已更新：#s 应命中 new1
	c.Handle("#s")
	if len(fe.stopCalls) != 1 || fe.stopCalls[0] != "new1" {
		t.Fatalf("#en 后 current 应更新为 new1: %v", fe.stopCalls)
	}

	fe2 := &fakeExecutor{easyNewErr: errors.New("no tools")}
	out = join(NewConsole(fe2).Handle("#en"))
	if !strings.Contains(out, "创建失败") {
		t.Fatalf("EasyNew 失败应回创建失败: %q", out)
	}
}

func TestNewMenuFullFlow(t *testing.T) {
	fe := &fakeExecutor{
		tools:      []MenuOption{{ID: "claude", Label: "Claude"}, {ID: "codex", Label: "Codex"}},
		workspaces: []MenuOption{{ID: "ws1", Label: `D:\proj`}, {ID: "ws2", Label: `E:\lib`}},
		models:     []MenuOption{{ID: "sonnet", Label: "Sonnet"}, {ID: "opus", Label: "Opus"}},
		createRef:  wsRef(0, "nc1", "", `D:\proj`, "codex", "新会话", false),
	}
	c := NewConsole(fe)

	out := join(c.Handle("#n"))
	if !strings.Contains(out, "选择工具") || !strings.Contains(out, "1. Claude") {
		t.Fatalf("step1 应列工具: %s", out)
	}
	// Markdown 一致性：菜单标题与选项列表间须空行分隔（同 #l 的 hint 分隔）。
	if !strings.Contains(out, "选择工具:\n\n1. Claude") {
		t.Fatalf("菜单标题与列表间应空行分隔: %q", out)
	}
	out = join(c.Handle("2")) // codex
	if !strings.Contains(out, "选择项目") || !strings.Contains(out, "2. E:\\lib") {
		t.Fatalf("step2 应列项目: %s", out)
	}
	out = join(c.Handle("1")) // ws1
	if !strings.Contains(out, "选择模型") {
		t.Fatalf("step3 应列模型: %s", out)
	}
	out = join(c.Handle("2")) // opus
	if !strings.Contains(out, "我是 codex") {
		t.Fatalf("创建成功应回欢迎词: %s", out)
	}
	if len(fe.createCalls) != 1 || fe.createCalls[0] != [3]string{"codex", "ws1", "opus"} {
		t.Fatalf("CreateNew 参数不符: %v", fe.createCalls)
	}
	// 菜单已清：再发数字应提示编号不存在（映射为空）
	out = join(c.Handle("1"))
	if !strings.Contains(out, "编号不存在") {
		t.Fatalf("创建后菜单应已清: %q", out)
	}
}

func TestNewMenuSkipsEmptyModels(t *testing.T) {
	fe := &fakeExecutor{
		tools:      []MenuOption{{ID: "claude", Label: "Claude"}},
		workspaces: []MenuOption{{ID: "ws1", Label: `D:\proj`}},
		models:     nil,
		createRef:  wsRef(0, "nc1", "", `D:\proj`, "claude", "新会话", false),
	}
	c := NewConsole(fe)
	c.Handle("#n")
	c.Handle("1")              // 选工具 → 选项目
	out := join(c.Handle("1")) // 选项目 → models 为空直接创建
	if !strings.Contains(out, "会话已就绪") {
		t.Fatalf("models 为空时选完项目应直接创建: %s", out)
	}
	if len(fe.createCalls) != 1 || fe.createCalls[0] != [3]string{"claude", "ws1", ""} {
		t.Fatalf("CreateNew 参数不符: %v", fe.createCalls)
	}
}

func TestNewMenuCreateFailureClearsMenu(t *testing.T) {
	fe := &fakeExecutor{
		tools:      []MenuOption{{ID: "claude", Label: "Claude"}},
		workspaces: []MenuOption{{ID: "ws1", Label: `D:\proj`}},
		createErr:  errors.New("boom"),
	}
	c := NewConsole(fe)
	c.Handle("#n")
	c.Handle("1")              // 工具
	out := join(c.Handle("1")) // 项目 → 直接创建（无 models）→ 失败
	if !strings.Contains(out, "创建失败") {
		t.Fatalf("CreateNew 失败应回创建失败: %q", out)
	}
	// 菜单应已清：数字不再走菜单
	out = join(c.Handle("9"))
	if !strings.Contains(out, "编号不存在") {
		t.Fatalf("失败后菜单应已清: %q", out)
	}
}

func TestCancel(t *testing.T) {
	fe := &fakeExecutor{
		tools:      []MenuOption{{ID: "claude", Label: "Claude"}},
		workspaces: []MenuOption{{ID: "ws1", Label: `D:\proj`}},
	}
	c := NewConsole(fe)
	if got := join(c.Handle("#c")); got != "无进行中的选择" {
		t.Fatalf("无菜单取消文案不符: %q", got)
	}
	c.Handle("#n")
	if got := join(c.Handle("#c")); got != "已取消" {
		t.Fatalf("有菜单取消文案不符: %q", got)
	}
	// 取消后数字不走菜单
	if got := join(c.Handle("1")); !strings.Contains(got, "编号不存在") {
		t.Fatalf("取消后菜单应已清: %q", got)
	}
}

func TestStop(t *testing.T) {
	fe := &fakeExecutor{
		hasActivity:     true,
		lastActivityRef: wsRef(0, "c1", "sid1", "", "claude", "A", true),
	}
	c := NewConsole(fe)
	if got := join(c.Handle("#s")); got != "已发送中断指令" {
		t.Fatalf("中断成功文案不符: %q", got)
	}
	if len(fe.stopCalls) != 1 || fe.stopCalls[0] != "c1" {
		t.Fatalf("Stop 应收到当前 chatID: %v", fe.stopCalls)
	}
	if got := join(NewConsole(&fakeExecutor{}).Handle("#s")); got != "当前无活跃会话" {
		t.Fatalf("无 current 中断文案不符: %q", got)
	}
}

// handleStop：有 current 但 ChatID 空（未激活的磁盘会话）时，没有运行中的
// 任务可中断，直接回「当前无活跃会话」，不得把 SessionID 误传给 ex.Stop
// （CancelChat 按 chat ID 解析必报错）。
func TestStopInactiveSessionNoChatID(t *testing.T) {
	fe := &fakeExecutor{
		hasActivity:     true,
		lastActivityRef: wsRef(0, "", "sid-only", "", "claude", "磁盘会话", true),
	}
	c := NewConsole(fe)
	if got := join(c.Handle("#s")); got != "当前无活跃会话" {
		t.Fatalf("ChatID 空的中断应回无活跃会话: %q", got)
	}
	if len(fe.stopCalls) != 0 {
		t.Fatalf("ChatID 空时不得调用 ex.Stop: %v", fe.stopCalls)
	}
}

func TestModelsMenu(t *testing.T) {
	fe := &fakeExecutor{
		hasActivity:     true,
		lastActivityRef: wsRef(0, "c1", "", "", "claude", "A", true),
		listModels:      []MenuOption{{ID: "sonnet", Label: "Sonnet"}, {ID: "opus", Label: "Opus"}},
		listModelsCur:   "opus",
	}
	c := NewConsole(fe)
	out := join(c.Handle("#m"))
	if !strings.Contains(out, "选择模型") || !strings.Contains(out, "2. ▶ Opus") {
		t.Fatalf("#m 应列模型并标当前: %s", out)
	}
	if !strings.Contains(out, "选择模型:\n\n1. Sonnet") {
		t.Fatalf("模型菜单标题与列表间应空行分隔: %q", out)
	}
	out = join(c.Handle("1"))
	if !strings.Contains(out, "已切换模型: Sonnet") {
		t.Fatalf("切换模型成功文案不符: %s", out)
	}
	if len(fe.setModelCalls) != 1 || fe.setModelCalls[0] != [2]string{"c1", "sonnet"} {
		t.Fatalf("SetModel 参数不符: %v", fe.setModelCalls)
	}

	// 失败路径
	fe2 := &fakeExecutor{
		hasActivity:     true,
		lastActivityRef: wsRef(0, "c1", "", "", "claude", "A", true),
		listModels:      []MenuOption{{ID: "sonnet", Label: "Sonnet"}},
		setModelErr:     errors.New("nope"),
	}
	c2 := NewConsole(fe2)
	c2.Handle("#m")
	out = join(c2.Handle("1"))
	if !strings.Contains(out, "切换失败") {
		t.Fatalf("SetModel 失败应回切换失败: %q", out)
	}
	// 菜单已清
	out = join(c2.Handle("1"))
	if !strings.Contains(out, "编号不存在") {
		t.Fatalf("失败后菜单应已清: %q", out)
	}
}

func TestGSend(t *testing.T) {
	fe := &fakeExecutor{
		listSessions: []SessionRef{
			wsRef(0, "c1", "", "", "claude", "A", false),
			wsRef(0, "c2", "", "", "codex", "B", true),
		},
		hasActivity:     true,
		lastActivityRef: wsRef(0, "c2", "", "", "codex", "B", true),
	}
	c := NewConsole(fe)
	c.Handle("#l") // #g 依据上次列表的编号映射
	out := join(c.Handle("#g 1 hello world"))
	if !strings.Contains(out, "已发送到: 【A】") {
		t.Fatalf("#g 成功文案不符: %q", out)
	}
	if len(fe.sendCalls) != 1 || fe.sendCalls[0] != [2]string{"c1", "hello world"} {
		t.Fatalf("Send 参数不符: %v", fe.sendCalls)
	}
	// current 不变：#s 仍停 c2
	c.Handle("#s")
	if fe.stopCalls[0] != "c2" {
		t.Fatalf("#g 不应切 current: %v", fe.stopCalls)
	}
}

func TestGUsageErrors(t *testing.T) {
	fe := &fakeExecutor{
		listSessions: []SessionRef{wsRef(0, "c1", "", "", "claude", "A", false)},
	}
	c := NewConsole(fe)
	if got := join(c.Handle("#g")); !strings.Contains(got, "用法: #g <编号> <内容>") {
		t.Fatalf("#g 无参数应回用法: %q", got)
	}
	if got := join(c.Handle("#g 1")); !strings.Contains(got, "用法: #g <编号> <内容>") {
		t.Fatalf("#g 缺内容应回用法: %q", got)
	}
	if got := join(c.Handle("#g 9 hi")); !strings.Contains(got, "编号不存在") {
		t.Fatalf("#g 越界编号应提示: %q", got)
	}
}

func TestGSendFailure(t *testing.T) {
	fe := &fakeExecutor{
		listSessions: []SessionRef{wsRef(0, "c1", "", "", "claude", "A", false)},
		sendErr:      errors.New("offline"),
	}
	c := NewConsole(fe)
	c.Handle("#l")
	out := join(c.Handle("#g 1 hi"))
	if !strings.Contains(out, "发送失败") {
		t.Fatalf("Send 失败应回发送失败: %q", out)
	}
}

func TestSearch(t *testing.T) {
	fe := &fakeExecutor{
		searchResult: []SessionRef{
			wsRef(0, "s1", "", `D:\ws\login`, "claude", "登录修复", false),
		},
	}
	c := NewConsole(fe)
	if got := join(c.Handle("#f")); got != "用法: #f <关键词>" {
		t.Fatalf("空关键词应回用法: %q", got)
	}
	if got := join(NewConsole(&fakeExecutor{}).Handle("#f 登录")); got != "未找到匹配会话" {
		t.Fatalf("空结果应回未找到: %q", got)
	}
	out := join(c.Handle("#f 登录"))
	if !strings.Contains(out, "1. [login] claude 登录修复") {
		t.Fatalf("搜索结果列表格式不符: %s", out)
	}
	// 菜单命中 → 切换
	c.Handle("1")
	if len(fe.switchCalls) != 1 || fe.switchCalls[0] != "s1" {
		t.Fatalf("search 菜单数字应触发切换: %v", fe.switchCalls)
	}
}

func TestStatus(t *testing.T) {
	c := NewConsole(&fakeExecutor{
		status: ConsoleStatus{CurrentTitle: "修复登录", Running: 2, Undelivered: 5},
	})
	out := join(c.Handle("#st"))
	want := "- 当前会话: 修复登录\n- 运行中任务: 2\n- 未送达消息: 5"
	if out != want {
		t.Fatalf("#st 格式不符:\n得到 %q\n期望 %q", out, want)
	}
}

func TestPermissionMenu(t *testing.T) {
	fe := &fakeExecutor{}
	c := NewConsole(fe)
	c.OpenPermissionMenu("chat1", "req1", "允许执行 rm -rf /tmp/x")
	out := join(c.Handle("1"))
	if !strings.Contains(out, "已处理权限请求") {
		t.Fatalf("允许成功文案不符: %q", out)
	}
	if len(fe.grantCalls) != 1 || fe.grantCalls[0] != [3]string{"chat1", "req1", "true"} {
		t.Fatalf("GrantPermission 参数不符: %v", fe.grantCalls)
	}
	// 菜单已清
	out = join(c.Handle("2"))
	if !strings.Contains(out, "编号不存在") {
		t.Fatalf("处理完后 permission 菜单应已清: %q", out)
	}

	fe2 := &fakeExecutor{}
	c2 := NewConsole(fe2)
	c2.OpenPermissionMenu("chat1", "req1", "描述")
	out = join(c2.Handle("2"))
	if len(fe2.grantCalls) != 1 || fe2.grantCalls[0] != [3]string{"chat1", "req1", "false"} {
		t.Fatalf("拒绝参数不符: %v", fe2.grantCalls)
	}
}

func TestPermissionMenuNonNumericPassesThrough(t *testing.T) {
	fe := &fakeExecutor{
		hasActivity:     true,
		lastActivityRef: wsRef(0, "c1", "", "", "claude", "A", true),
	}
	c := NewConsole(fe)
	c.OpenPermissionMenu("chat1", "req1", "描述")
	// 非数字输入不被拦截：照常执行命令，菜单保持。
	if got := join(c.Handle("#?")); !strings.Contains(got, "命令列表") {
		t.Fatalf("permission 等待期 #? 应照常执行: %q", got)
	}
	if got := c.Handle("   "); len(got) != 0 {
		t.Fatalf("空输入仍应返回空: %v", got)
	}
	if got := c.Handle("#?"); !strings.Contains(join(got), "命令列表") {
		t.Fatalf("permission 等待期 #? 应照常执行: %q", join(got))
	}
	// 数字 1/2 之外的数字：提示重新选择，菜单保持。
	out := join(c.Handle("9"))
	if !strings.Contains(out, "1（允许）或 2（拒绝）") {
		t.Fatalf("其他数字应提示重新选择: %q", out)
	}
	if len(fe.grantCalls) != 0 {
		t.Fatalf("不应触发 GrantPermission: %v", fe.grantCalls)
	}
	// 菜单仍在：此时 1 应正常授权。
	out = join(c.Handle("1"))
	if !strings.Contains(out, "已处理权限请求") {
		t.Fatalf("保持的菜单应继续可响应: %q", out)
	}
	if len(fe.grantCalls) != 1 || fe.grantCalls[0] != [3]string{"chat1", "req1", "true"} {
		t.Fatalf("GrantPermission 参数不符: %v", fe.grantCalls)
	}
}

func TestPermissionMenuFailure(t *testing.T) {
	fe := &fakeExecutor{grantErr: errors.New("already resolved")}
	c := NewConsole(fe)
	c.OpenPermissionMenu("chat1", "req1", "描述")
	out := join(c.Handle("1"))
	if !strings.Contains(out, "该选择已失效") {
		t.Fatalf("GrantPermission 失败应回已失效: %q", out)
	}
	// 失败也清菜单
	out = join(c.Handle("1"))
	if !strings.Contains(out, "编号不存在") {
		t.Fatalf("失败后菜单应已清: %q", out)
	}
}

func TestPlainTextInput(t *testing.T) {
	fe := &fakeExecutor{
		hasActivity:     true,
		lastActivityRef: wsRef(0, "c1", "", "", "claude", "A", true),
	}
	c := NewConsole(fe)
	if got := c.Handle("你好"); len(got) != 0 {
		t.Fatalf("普通文本发送成功不应回显: %v", got)
	}
	if len(fe.sendCalls) != 1 || fe.sendCalls[0] != [2]string{"c1", "你好"} {
		t.Fatalf("Send 参数不符: %v", fe.sendCalls)
	}
	// 无 current 且无会话：进入待选（仅新建入口）
	out := join(NewConsole(&fakeExecutor{}).Handle("你好"))
	if !strings.Contains(out, "未精确匹配到会话") || !strings.Contains(out, "1. 新建 session") {
		t.Fatalf("无 current 普通文本应进仅新建入口待选: %q", out)
	}
	// 发送失败
	fe2 := &fakeExecutor{
		hasActivity:     true,
		lastActivityRef: wsRef(0, "c1", "", "", "claude", "A", true),
		sendErr:         errors.New("offline"),
	}
	out = join(NewConsole(fe2).Handle("你好"))
	if !strings.Contains(out, "发送失败") {
		t.Fatalf("Send 失败应回发送失败: %q", out)
	}
}

func TestPlainTextWithMenu(t *testing.T) {
	fe := &fakeExecutor{
		tools:      []MenuOption{{ID: "claude", Label: "Claude"}},
		workspaces: []MenuOption{{ID: "ws1", Label: `D:\p`}},
	}
	c := NewConsole(fe)
	c.Handle("#n")
	out := join(c.Handle("hello"))
	if !strings.Contains(out, "请回复数字选择") {
		t.Fatalf("有菜单时普通文本应提示选数字: %q", out)
	}
}

func TestUnknownCommand(t *testing.T) {
	c := NewConsole(&fakeExecutor{})
	if got := join(c.Handle("#xyz")); got != "未知命令，#? 查看帮助" {
		t.Fatalf("未知命令文案不符: %q", got)
	}
}

func TestEmptyInput(t *testing.T) {
	c := NewConsole(&fakeExecutor{})
	if got := c.Handle("   "); len(got) != 0 {
		t.Fatalf("空输入应返回空: %v", got)
	}
}

// #l 列出的未激活磁盘会话（ChatID 空、SessionID 非空）用数字切换时，
// 必须走 OpenSession 激活而非 SwitchTo；成功后 current 更新为返回的新 ref。
func TestSwitchInactiveSessionOpensSession(t *testing.T) {
	fe := &fakeExecutor{
		listSessions: []SessionRef{
			wsRef(0, "c1", "s1", `D:\ws\a`, "claude", "活跃会话", false),
			wsRef(0, "", "s2", `D:\ws\b`, "codex", "磁盘会话", false),
		},
		hasActivity:     true,
		lastActivityRef: wsRef(0, "c1", "s1", `D:\ws\a`, "claude", "活跃会话", true),
		openSessionRef:  wsRef(0, "c9", "s2", `D:\ws\b`, "codex", "磁盘会话", false),
	}
	c := NewConsole(fe)
	if got := join(c.Handle("#l")); !strings.Contains(got, "2. [b] codex 磁盘会话") {
		t.Fatalf("列表应含未激活会话: %q", got)
	}
	out := join(c.Handle("2"))
	if len(fe.openSessionCalls) != 1 || fe.openSessionCalls[0] != "s2" {
		t.Fatalf("未激活会话应走 OpenSession(s2)，实际 open=%v switch=%v",
			fe.openSessionCalls, fe.switchCalls)
	}
	if len(fe.switchCalls) != 0 {
		t.Fatalf("未激活会话不得走 SwitchTo: %v", fe.switchCalls)
	}
	if !strings.Contains(out, "已切换") {
		t.Fatalf("OpenSession 成功应回已切换: %q", out)
	}
	// current 更新为返回的新 ref：#s 应命中新 chatID c9。
	c.Handle("#s")
	if len(fe.stopCalls) != 1 || fe.stopCalls[0] != "c9" {
		t.Fatalf("切换后 current 应为 OpenSession 返回的 ref: %v", fe.stopCalls)
	}
}

// OpenSession 失败回「切换失败」，current 不变（fake 无预置 current，保持零值）。
func TestOpenSessionFailure(t *testing.T) {
	fe := &fakeExecutor{
		listSessions:   []SessionRef{wsRef(0, "", "s2", `D:\ws\b`, "codex", "磁盘会话", false)},
		openSessionErr: errors.New("session gone"),
	}
	c := NewConsole(fe)
	c.Handle("#l")
	out := join(c.Handle("1"))
	if !strings.Contains(out, "切换失败") {
		t.Fatalf("OpenSession 失败应回切换失败: %q", out)
	}
	if c.hasCurrent {
		t.Fatalf("失败不得改写 current: %+v", c.current)
	}
}

// SetCurrent 显式注入上下文后，普通文本直接发给该 ChatID（不等 refreshCurrent）。
func TestSetCurrentDirectsPlainText(t *testing.T) {
	fe := &fakeExecutor{
		listSessions: []SessionRef{wsRef(0, "c1", "", "", "claude", "A", false)},
	}
	c := NewConsole(fe)
	c.SetCurrent(wsRef(0, "c-injected", "s-x", `D:\ws\c`, "gemini", "注入会话", false))
	if got := c.Handle("你好"); len(got) != 0 {
		t.Fatalf("注入后普通文本发送成功不应回显: %v", got)
	}
	if len(fe.sendCalls) != 1 || fe.sendCalls[0] != [2]string{"c-injected", "你好"} {
		t.Fatalf("Send 应收到 SetCurrent 的 ChatID: %v", fe.sendCalls)
	}
}

// 全角符号归一化：＃前缀（仅已知命令提升）、全角空格、全角数字、全角冒号分隔。
func TestFullWidthInputNormalization(t *testing.T) {
	fe := &fakeExecutor{
		listSessions: []SessionRef{
			wsRef(0, "c1", "", "", "claude", "A", false),
			wsRef(0, "c2", "", "", "codex", "B", true),
		},
		hasActivity:     true,
		lastActivityRef: wsRef(0, "c2", "", "", "codex", "B", true),
	}
	c := NewConsole(fe)
	c.Handle("#l")

	// ＃l：全角井号 + 已知命令 → 列表
	if got := join(c.Handle("＃l")); !strings.Contains(got, "1. [") {
		t.Fatalf("＃l 应按命令处理: %q", got)
	}
	// ＃en：全角井号快速新建
	if got := join(c.Handle("＃en")); !strings.Contains(got, "会话已就绪") {
		t.Fatalf("＃en 应触发快速新建: %q", got)
	}
	// 全角数字编号切换（先 #l 建映射）
	c.Handle("#l")
	if got := join(c.Handle("１")); !strings.Contains(got, "已切换") {
		t.Fatalf("全角数字应按编号切换: %q", got)
	}
	if len(fe.switchCalls) == 0 || fe.switchCalls[len(fe.switchCalls)-1] != "c1" {
		t.Fatalf("１ 应切到编号 1: %v", fe.switchCalls)
	}
	// ＃g：全角井号 + 全角冒号 + 全角空格
	c.Handle("#l")
	if got := join(c.Handle("＃g：1　你好")); !strings.Contains(got, "已发送到: 【A】") {
		t.Fatalf("＃g：1　你好 应发送成功: %q", got)
	}
	if fe.sendCalls[len(fe.sendCalls)-1] != [2]string{"c1", "你好"} {
		t.Fatalf("#g 全角形态 Send 参数不符: %v", fe.sendCalls)
	}
	// 半角 #g 编号后跟全角冒号
	c.Handle("#l")
	if got := join(c.Handle("#g 1：再试")); !strings.Contains(got, "已发送到: 【A】") {
		t.Fatalf("#g 1：再试 应发送成功: %q", got)
	}
	if fe.sendCalls[len(fe.sendCalls)-1] != [2]string{"c1", "再试"} {
		t.Fatalf("#g 编号后冒号 Send 参数不符: %v", fe.sendCalls)
	}
	// ＃g 1 注意：正文里的冒号不受影响
	c.Handle("#l")
	if got := join(c.Handle("#g 1 注意：细节")); !strings.Contains(got, "已发送到: 【A】") {
		t.Fatalf("正文冒号不应影响 #g: %q", got)
	}
	if fe.sendCalls[len(fe.sendCalls)-1] != [2]string{"c1", "注意：细节"} {
		t.Fatalf("正文含冒号 Send 参数不符: %v", fe.sendCalls)
	}
	// ＃ + 未知首词：不劫持，按普通文本发给当前会话
	if got := c.Handle("＃话题 标签"); len(got) != 0 {
		t.Fatalf("＃未知词应按普通文本发送不回显: %v", got)
	}
	// 上一步 １ 是显式切换：水位内的旧 hub 归因不得把目标抢回 c2。
	if fe.sendCalls[len(fe.sendCalls)-1] != [2]string{"c1", "＃话题 标签"} {
		t.Fatalf("＃未知词应发给显式选中的会话: %v", fe.sendCalls)
	}
}

// 创建成功回复 agent 口吻欢迎词，含【会话名】与工具展示名（2026-10-18 用户反馈）。
func TestCreateReplyAgentVoice(t *testing.T) {
	fe := &fakeExecutor{
		easyNewRef: SessionRef{ChatID: "n1", Tool: "codebuddy", ToolName: "CodeBuddy", Title: "kshell · CodeBuddy"},
	}
	out := join(NewConsoleLang(fe, "en").Handle("#en"))
	want := "【kshell · CodeBuddy】Hi! This is CodeBuddy. The session is ready — type any task below and I will start working on it."
	if out != want {
		t.Fatalf("欢迎词不符:\n得到 %q\n期望 %q", out, want)
	}
	// 工具展示名缺失回落 ToolID
	fe2 := &fakeExecutor{easyNewRef: SessionRef{ChatID: "n1", Tool: "claude", Title: "T"}}
	if got := join(NewConsoleLang(fe2, "en").Handle("#en")); !strings.Contains(got, "This is claude.") {
		t.Fatalf("无展示名应回落 ToolID: %q", got)
	}
	// 分步菜单创建同样回欢迎词（默认中文文案）
	fe3 := &fakeExecutor{
		tools:      []MenuOption{{ID: "claude", Label: "Claude"}},
		workspaces: []MenuOption{{ID: "ws1", Label: `D:\proj`}},
		createRef:  SessionRef{ChatID: "nc1", Tool: "claude", ToolName: "Claude Code", Title: "新会话"},
	}
	c3 := NewConsole(fe3)
	c3.Handle("#n")
	c3.Handle("1")
	if got := join(c3.Handle("1")); !strings.Contains(got, "我是 Claude Code") || !strings.Contains(got, "【新会话】") {
		t.Fatalf("菜单创建应回欢迎词: %q", got)
	}
}

// 切换与 #g 回复统一【会话名】。
func TestSwitchAndSentSessionTag(t *testing.T) {
	fe := &fakeExecutor{listSessions: []SessionRef{wsRef(0, "c1", "", "", "claude", "会话A", false)}}
	c := NewConsole(fe)
	c.Handle("#l")
	if got := join(c.Handle("1")); !strings.Contains(got, "已切换: 【会话A】") {
		t.Fatalf("切换回复应带【会话名】: %q", got)
	}
	c.Handle("#l")
	if got := join(c.Handle("#g 1 hi")); !strings.Contains(got, "已发送到: 【会话A】") {
		t.Fatalf("#g 回复应带【会话名】: %q", got)
	}
}

// #en 带消息：创建成功后消息自动发给新会话（CreateNew 返回即握手完成，
// SendChatPrompt 对 starting 会话另有入队兜底）；发送失败追加发送失败回复。
func TestEasyNewWithMessage(t *testing.T) {
	fe := &fakeExecutor{
		easyNewRef: SessionRef{ChatID: "n1", Tool: "claude", ToolName: "Claude Code", Title: "T"},
	}
	out := join(NewConsole(fe).Handle("#en 帮我写个 todo"))
	if len(fe.sendCalls) != 1 || fe.sendCalls[0] != [2]string{"n1", "帮我写个 todo"} {
		t.Fatalf("#en 消息应自动下发: %v", fe.sendCalls)
	}
	if !strings.Contains(out, "我是 Claude Code") {
		t.Fatalf("仍应回欢迎词: %q", out)
	}

	// 发送失败：欢迎词 + 发送失败两条都回
	fe2 := &fakeExecutor{
		easyNewRef: SessionRef{ChatID: "n1", Tool: "claude", Title: "T"},
		sendErr:    errors.New("offline"),
	}
	out = join(NewConsole(fe2).Handle("#en 任务"))
	if !strings.Contains(out, "我是 claude") || !strings.Contains(out, "发送失败") {
		t.Fatalf("发送失败应同时回欢迎词与失败提示: %q", out)
	}

	// 无消息：不 Send
	fe3 := &fakeExecutor{easyNewRef: SessionRef{ChatID: "n1", Tool: "claude", Title: "T"}}
	NewConsole(fe3).Handle("#en")
	if len(fe3.sendCalls) != 0 {
		t.Fatalf("无消息不得 Send: %v", fe3.sendCalls)
	}
}

// 全角问号帮助：#？ 与 ＃？ 均应出帮助（2026-10-18 用户反馈漏网字符）。
func TestFullWidthQuestionHelp(t *testing.T) {
	c := NewConsole(&fakeExecutor{
		hasActivity:     true,
		lastActivityRef: wsRef(0, "c1", "", "", "claude", "A", true),
	})
	for _, in := range []string{"#？", "＃？", "#? ", "＃?"} {
		if got := join(c.Handle(in)); !strings.Contains(got, "命令列表") {
			t.Fatalf("%s 应按帮助处理: %q", in, got)
		}
	}
}

// #h 查询 AgentHub 最近消息：默认 10 条列表（存快照）、超长截断；
// `#h <序号>` 回显上次列表第 N 条全文（条数参数已砍，2026-10-19 用户决策 A）。
func TestRecentMessagesCommand(t *testing.T) {
	long := strings.Repeat("长", recentTextCap+5)
	fe := &fakeExecutor{recentMessages: []Message{
		{Kind: "user", SessionTitle: "会话A", Text: "第一问"},
		{Kind: "agent", SessionTitle: "会话A", Text: long},
	}}
	c := NewConsole(fe)

	out := join(c.Handle("#h"))
	if !strings.Contains(out, "**最近 2 条消息:**") || !strings.Contains(out, "1. [用户][会话A] 第一问") {
		t.Fatalf("#h 默认格式不符: %q", out)
	}
	if !strings.Contains(out, strings.Repeat("长", recentTextCap)+"…") {
		t.Fatalf("超长正文应截断加省略号: %q", out)
	}

	// 序号查全文：#h 2 回显快照第 2 条完整正文（不截断、无省略号）
	out = join(c.Handle("#h 2"))
	if !strings.Contains(out, "**消息 2** [agent][会话A]") {
		t.Fatalf("#h 2 应含详情头: %q", out)
	}
	if !strings.Contains(out, long) || strings.Contains(out, "…") {
		t.Fatalf("#h 2 应回全文不截断: %q", out)
	}

	// 全角序号
	out = join(c.Handle("#h １"))
	if !strings.Contains(out, "**消息 1** [用户][会话A]\n\n第一问") {
		t.Fatalf("#h 全角序号应查详情: %q", out)
	}

	// 无快照直接查：自动取最近一批再按序号取
	out = join(NewConsole(fe).Handle("#h 1"))
	if !strings.Contains(out, "**消息 1** [用户][会话A]\n\n第一问") {
		t.Fatalf("无快照时 #h 1 应自动取最近列表查详情: %q", out)
	}

	// 越界 / 非法参数（越界文案须指向 #h 而非 #l，见 recent_no_mapping）
	if got := join(c.Handle("#h 99")); !strings.Contains(got, "编号不存在") || !strings.Contains(got, "#h 查看最近消息") {
		t.Fatalf("#h 99 应提示编号不存在并引导 #h: %q", got)
	}
	for _, in := range []string{"#h abc", "#h 0", "#h -1"} {
		if got := join(c.Handle(in)); !strings.Contains(got, "用法: #h 或 #h <序号>") {
			t.Fatalf("%s 应回用法: %q", in, got)
		}
	}

	// 空历史：列表与查详情分别提示
	if got := join(NewConsole(&fakeExecutor{}).Handle("#h")); got != "暂无 AgentHub 消息" {
		t.Fatalf("空历史应回暂无消息: %q", got)
	}
	if got := join(NewConsole(&fakeExecutor{}).Handle("#h 1")); !strings.Contains(got, "暂无 AgentHub 消息") {
		t.Fatalf("空历史查详情应回暂无消息: %q", got)
	}
}

// #h 须过滤 ui.hub.* / hub.welcome / 已撤回，再取最近可见条。
func TestRecentMessagesFiltersHubNoise(t *testing.T) {
	fe := &fakeExecutor{recentMessages: []Message{
		{Kind: "user", SessionTitle: "A", Text: "真实提问"},
		{Kind: "agent", SessionTitle: "A", Event: "ui.hub.ack_delivered", Text: "ui.hub.ack_delivered"},
		{Kind: "agent", SessionTitle: "A", Text: "ui.hub.ack_queued"},
		{Kind: "agent", SessionTitle: "A", Text: "hub.welcome|CodeBuddy"},
		{Kind: "agent", SessionTitle: "A", Status: StatusRecalled, Text: "已撤回正文"},
		{Kind: "agent", SessionTitle: "A", Text: "真实回复"},
	}}
	out := join(NewConsole(fe).Handle("#h"))
	if strings.Contains(out, "ack_delivered") || strings.Contains(out, "ack_queued") ||
		strings.Contains(out, "hub.welcome") || strings.Contains(out, "已撤回") {
		t.Fatalf("#h 不应含事件/欢迎/撤回噪声: %q", out)
	}
	if !strings.Contains(out, "**最近 2 条消息:**") ||
		!strings.Contains(out, "1. [用户][A] 真实提问") ||
		!strings.Contains(out, "2. [agent][A] 真实回复") {
		t.Fatalf("#h 应只列可见对话: %q", out)
	}
	if !strings.Contains(out, "回复数字可查看该条全文") {
		t.Fatalf("#h 应含查全文引导: %q", out)
	}
}

// #h 后纯数字查全文；#l 后纯数字仍切会话（数字语境 A）。
func TestDigitContextAfterRecentVsList(t *testing.T) {
	fe := &fakeExecutor{
		listSessions: []SessionRef{
			wsRef(0, "c1", "", "", "claude", "会话一", false),
			wsRef(0, "c2", "", "", "codex", "会话二", false),
		},
		recentMessages: []Message{
			{Kind: "user", SessionTitle: "会话一", Text: "第一问很长正文"},
			{Kind: "agent", SessionTitle: "会话一", Text: "第一答全文内容"},
		},
	}
	c := NewConsole(fe)

	_ = c.Handle("#h")
	out := join(c.Handle("2"))
	if !strings.Contains(out, "**消息 2**") || !strings.Contains(out, "第一答全文内容") {
		t.Fatalf("#h 后纯数字 2 应查全文: %q", out)
	}
	if len(fe.switchCalls) != 0 {
		t.Fatalf("#h 后纯数字不得 SwitchTo: %v", fe.switchCalls)
	}

	_ = c.Handle("#l")
	out = join(c.Handle("2"))
	if !strings.Contains(out, "已切换") || len(fe.switchCalls) != 1 || fe.switchCalls[0] != "c2" {
		t.Fatalf("#l 后纯数字 2 应切会话: out=%q switch=%v", out, fe.switchCalls)
	}

	// 再 #h 后数字语境回到查全文
	fe.switchCalls = nil
	_ = c.Handle("#h")
	out = join(c.Handle("1"))
	if !strings.Contains(out, "**消息 1**") || !strings.Contains(out, "第一问很长正文") {
		t.Fatalf("再次 #h 后纯数字应查全文: %q", out)
	}
	if len(fe.switchCalls) != 0 {
		t.Fatalf("再次 #h 后纯数字不得 SwitchTo: %v", fe.switchCalls)
	}
}

// 用户显式选了编号后，水位内更旧的 hub 归因不得把投递目标抢回去。
func TestExplicitSwitchBeatsStaleActivity(t *testing.T) {
	fe := &fakeExecutor{
		listSessions: []SessionRef{
			wsRef(0, "c1", "", "", "claude", "A", false),
			wsRef(0, "c2", "", "", "codex", "B", false),
		},
		lastActivityID:  7,
		lastActivityRef: wsRef(0, "c1", "", "", "claude", "A", false),
		hasActivity:     true,
	}
	c := NewConsole(fe)
	c.Handle("#l")
	c.Handle("2") // 显式切到 c2
	if got := c.Handle("你好"); len(got) != 0 {
		t.Fatalf("普通文本发送成功不应回显: %v", got)
	}
	if len(fe.sendCalls) != 1 || fe.sendCalls[0] != [2]string{"c2", "你好"} {
		t.Fatalf("显式选择应胜过旧归因: %v", fe.sendCalls)
	}
}

// hub 出现更新归因（如别的渠道新建会话并发了消息）时，应覆盖显式选择。
func TestNewerActivityOverridesExplicitSwitch(t *testing.T) {
	fe := &fakeExecutor{
		listSessions: []SessionRef{
			wsRef(0, "c1", "", "", "claude", "A", false),
			wsRef(0, "c2", "", "", "codex", "B", false),
		},
		lastActivityID:  7,
		lastActivityRef: wsRef(0, "c1", "", "", "claude", "A", false),
		hasActivity:     true,
	}
	c := NewConsole(fe)
	c.Handle("#l")
	c.Handle("2") // 显式切到 c2
	// 新会话产生更高水位的归因。
	fe.lastActivityID = 8
	fe.lastActivityRef = wsRef(0, "c3", "", "", "gemini", "C", false)
	if got := c.Handle("你好"); len(got) != 0 {
		t.Fatalf("普通文本发送成功不应回显: %v", got)
	}
	if len(fe.sendCalls) != 1 || fe.sendCalls[0] != [2]string{"c3", "你好"} {
		t.Fatalf("新归因应覆盖显式选择: %v", fe.sendCalls)
	}
}

// 无精确目标：列出会话并暂存正文，不投递；回编号后激活并立即发送。
func TestPlainTextWithoutTargetListsSessions(t *testing.T) {
	fe := &fakeExecutor{listSessions: []SessionRef{
		wsRef(0, "c1", "", `D:\ws\a`, "claude", "会话A", false),
		wsRef(0, "", "s2", `D:\ws\b`, "codex", "会话B", false),
	}}
	c := NewConsole(fe)
	out := join(c.Handle("帮我修个 bug"))
	for _, want := range []string{"未精确匹配到会话", "2. [a] claude 会话A", "3. [b] codex 会话B", "请回复数字选择"} {
		if !strings.Contains(out, want) {
			t.Fatalf("无目标列表缺少 %q: %q", want, out)
		}
	}
	if len(fe.sendCalls) != 0 {
		t.Fatalf("列会话时不得投递: %v", fe.sendCalls)
	}

	fe.openSessionRef = wsRef(0, "c9", "s2", `D:\ws\b`, "codex", "会话B", false)
	out = join(c.Handle("3"))
	if len(fe.openSessionCalls) != 1 || fe.openSessionCalls[0] != "s2" {
		t.Fatalf("回编号应激活所选磁盘会话: %v", fe.openSessionCalls)
	}
	if len(fe.sendCalls) != 1 || fe.sendCalls[0] != [2]string{"c9", "帮我修个 bug"} {
		t.Fatalf("回编号应立即发送暂存正文: %v", fe.sendCalls)
	}
	if !strings.Contains(out, "已发送到: 【会话B】") {
		t.Fatalf("发送成功应回已发送到: %q", out)
	}
}

// #c 取消待选：暂存正文丢弃，后续数字只切换不发送。
func TestPendingPickCancelDiscardsText(t *testing.T) {
	fe := &fakeExecutor{listSessions: []SessionRef{
		wsRef(0, "c1", "", "", "claude", "会话A", false),
	}}
	c := NewConsole(fe)
	c.Handle("先别发")
	if got := join(c.Handle("#c")); got != "已取消" {
		t.Fatalf("取消待选文案不符: %q", got)
	}
	c.Handle("2") // 编号 1 是「新建」，c1 在编号 2
	if len(fe.switchCalls) != 1 || fe.switchCalls[0] != "c1" {
		t.Fatalf("取消后数字应正常切换: %v", fe.switchCalls)
	}
	if len(fe.sendCalls) != 0 {
		t.Fatalf("取消后不得再发送暂存正文: %v", fe.sendCalls)
	}
}

// 当前会话终端占用：不作默认投递，进入待选（含 en）。
func TestPlainTextTerminalActiveCurrentGoesToPick(t *testing.T) {
	ref := wsRef(0, "c1", "s1", `D:\ws\a`, "claude", "终端会话", false)
	ref.TerminalActive = true
	fe := &fakeExecutor{
		lastActivityID:  9,
		lastActivityRef: ref,
		hasActivity:     true,
		listSessions:    []SessionRef{ref},
		easyNewRef:      wsRef(0, "c-new", "", `D:\ws\a`, "claude", "新会话", false),
	}
	c := NewConsole(fe)
	out := join(c.Handle("你好"))
	if !strings.Contains(out, "未精确匹配到会话") || !strings.Contains(out, "新建 session") {
		t.Fatalf("终端占用默认目标应进待选: %q", out)
	}
	if len(fe.sendCalls) != 0 {
		t.Fatalf("进待选前不得发送: %v", fe.sendCalls)
	}
	if len(fe.openSessionCalls) != 0 {
		t.Fatalf("不得 OpenSession: %v", fe.openSessionCalls)
	}
}

// 当前会话是未激活磁盘会话：不再自动激活，进入待选（含 en）。
func TestPlainTextInactiveCurrentGoesToPick(t *testing.T) {
	fe := &fakeExecutor{
		lastActivityID:  7,
		lastActivityRef: wsRef(0, "", "s1", `D:\ws\a`, "claude", "磁盘会话", false),
		hasActivity:     true,
		listSessions: []SessionRef{
			wsRef(0, "c1", "", `D:\ws\a`, "claude", "活跃A", false),
		},
		easyNewRef: wsRef(0, "c-new", "", `D:\ws\a`, "claude", "新会话", false),
	}
	c := NewConsole(fe)
	out := join(c.Handle("你好"))
	if !strings.Contains(out, "未精确匹配到会话") || !strings.Contains(out, "新建 session") {
		t.Fatalf("未激活默认目标应进待选: %q", out)
	}
	if len(fe.openSessionCalls) != 0 {
		t.Fatalf("不得自动 OpenSession: %v", fe.openSessionCalls)
	}
	if len(fe.sendCalls) != 0 {
		t.Fatalf("进待选前不得发送: %v", fe.sendCalls)
	}
}

// AgentHub 无目标时调用 PickTarget：分组菜单（新建 + 最近 5 会话），暂存正文。
func TestPickTargetListsSessions(t *testing.T) {
	fe := &fakeExecutor{listSessions: []SessionRef{
		wsRef(0, "c1", "", `D:\ws\a`, "claude", "会话A", false),
	}}
	c := NewConsole(fe)
	out, pending := c.PickTarget("  随便看看  ")
	if !pending {
		t.Fatal("有会话时应报告已进入待选菜单")
	}
	joined := join(out)
	if !strings.Contains(joined, "未精确匹配到会话") ||
		!strings.Contains(joined, "1. 新建") ||
		!strings.Contains(joined, "2. [a] claude 会话A") {
		t.Fatalf("PickTarget 应输出分组菜单（新建+会话）: %v", out)
	}
	if strings.Contains(joined, "en —") {
		t.Fatalf("分组菜单不再追加 en 项: %v", out)
	}
	if len(fe.sendCalls) != 0 {
		t.Fatalf("PickTarget 不得直接投递: %v", fe.sendCalls)
	}
	sent := c.Handle("2")
	if len(fe.sendCalls) != 1 || fe.sendCalls[0] != [2]string{"c1", "随便看看"} {
		t.Fatalf("回编号应发送去掉首尾空白的正文: %v", fe.sendCalls)
	}
	if len(sent) != 1 || !sent[0].Sent {
		t.Fatalf("待选投递成功应标记 Sent（微信桥据此清附件队列）: %+v", sent)
	}
}

// PickTarget 列表最多 5 个会话（编号顺延）。
func TestPickTargetCapsFiveSessions(t *testing.T) {
	var refs []SessionRef
	for i := 0; i < 8; i++ {
		refs = append(refs, wsRef(0, fmtS(i), "", fmtS(i), "claude", fmt.Sprintf("会话%d", i), false))
	}
	fe := &fakeExecutor{listSessions: refs}
	c := NewConsole(fe)
	out, pending := c.PickTarget("正文")
	if !pending {
		t.Fatal("应进入待选菜单")
	}
	joined := join(out)
	if !strings.Contains(joined, "6. [4]") {
		t.Fatalf("应列到第 5 个会话（编号 6）: %v", out)
	}
	if strings.Contains(joined, "7. [5]") {
		t.Fatalf("不得列出第 6 个会话: %v", out)
	}
}

// 待选选「新建」：转 #n 分步流程（选工具→项目→模型→创建→发送暂存正文）。
func TestPickTargetNewGoesMenuNew(t *testing.T) {
	fe := &fakeExecutor{
		listSessions: []SessionRef{
			wsRef(0, "c1", "", `D:\ws\a`, "claude", "会话A", false),
		},
		tools:      []MenuOption{{ID: "claude", Label: "Claude"}},
		workspaces: []MenuOption{{ID: "ws1", Label: `D:\proj`}},
		models:     []MenuOption{{ID: "sonnet", Label: "Sonnet"}},
		createRef:  wsRef(0, "c-new", "", `D:\ws\a`, "claude", "新会话", false),
	}
	c := NewConsole(fe)
	if _, pending := c.PickTarget("任务正文"); !pending {
		t.Fatal("应进入待选")
	}
	// 选 1（新建）→ 工具菜单
	out := c.Handle("1")
	if !strings.Contains(join(out), "选择工具") {
		t.Fatalf("选新建应进入选工具步骤: %v", out)
	}
	// 选工具 → 项目菜单
	out = c.Handle("1")
	if !strings.Contains(join(out), "选择项目") {
		t.Fatalf("选工具后应进入选项目步骤: %v", out)
	}
	// 选项目 → 模型菜单
	out = c.Handle("1")
	if !strings.Contains(join(out), "选择模型") {
		t.Fatalf("选项目后应进入选模型步骤: %v", out)
	}
	// 选模型 → 创建并发送暂存正文
	sent := c.Handle("1")
	if len(fe.createCalls) != 1 || fe.createCalls[0] != [3]string{"claude", "ws1", "sonnet"} {
		t.Fatalf("CreateNew 参数不符: %v", fe.createCalls)
	}
	if len(fe.sendCalls) != 1 || fe.sendCalls[0] != [2]string{"c-new", "任务正文"} {
		t.Fatalf("创建成功后应发送暂存正文: %v", fe.sendCalls)
	}
	joined := join(sent)
	if !strings.Contains(joined, "会话已就绪") || !strings.Contains(joined, "任务移交完成") {
		t.Fatalf("新建移交应回复欢迎词与移交确认: %v", sent)
	}
	okSent := false
	for _, r := range sent {
		if r.Sent {
			okSent = true
			break
		}
	}
	if !okSent {
		t.Fatalf("成功投递应标记 Sent: %+v", sent)
	}
}

// 待选选「新建」创建失败：暂存正文丢弃，不误发。
func TestPickTargetNewCreateFailDropsPending(t *testing.T) {
	fe := &fakeExecutor{
		tools:      []MenuOption{{ID: "claude", Label: "Claude"}},
		workspaces: []MenuOption{{ID: "ws1", Label: `D:\proj`}},
		createErr:  errors.New("boom"),
	}
	c := NewConsole(fe)
	if _, pending := c.PickTarget("正文"); !pending {
		t.Fatal("应进入待选")
	}
	c.Handle("1") // 新建 → 工具
	c.Handle("1") // 工具 → 项目
	c.Handle("1") // 项目（无模型 → 直接创建）
	if len(fe.createCalls) != 1 {
		t.Fatalf("应触发创建: %v", fe.createCalls)
	}
	if len(fe.sendCalls) != 0 {
		t.Fatalf("创建失败不得发送暂存正文: %v", fe.sendCalls)
	}
}

// 无会话：仍进待选，仅展示「新建」入口（Pending=true，便于清空草稿）。
func TestPickTargetOnlyEasyNewWhenNoSessions(t *testing.T) {
	fe := &fakeExecutor{
		tools:      []MenuOption{{ID: "claude", Label: "Claude"}},
		workspaces: []MenuOption{{ID: "ws1", Label: `D:\proj`}},
		createRef:  wsRef(0, "c-new", "", `D:\ws\a`, "claude", "新会话", false),
	}
	c := NewConsole(fe)
	out, pending := c.PickTarget("  随便看看  ")
	if !pending {
		t.Fatal("无会话仅新建入口时也应进入待选菜单")
	}
	if !strings.Contains(join(out), "1. 新建") {
		t.Fatalf("无会话应仅列出新建入口: %v", out)
	}
	c.Handle("1") // 新建 → 工具
	c.Handle("1") // 工具 → 项目
	c.Handle("1") // 项目 → 创建
	if len(fe.sendCalls) != 1 || fe.sendCalls[0] != [2]string{"c-new", "随便看看"} {
		t.Fatalf("新建后应发送: %v", fe.sendCalls)
	}
}

// 任意命令（#c 除外）让待选正文失效：后续数字不得再发送旧正文。
func TestCommandInvalidatesPendingPick(t *testing.T) {
	fe := &fakeExecutor{listSessions: []SessionRef{
		wsRef(0, "c1", "", `D:\ws\a`, "claude", "会话A", false),
		wsRef(0, "c2", "", `D:\ws\b`, "codex", "会话B", false),
	}}
	c := NewConsole(fe)
	if _, pending := c.PickTarget("旧正文"); !pending {
		t.Fatal("应先进入待选菜单")
	}
	c.Handle("#l") // 显式查看列表：旧待选正文失效
	c.Handle("1")
	if len(fe.sendCalls) != 0 {
		t.Fatalf("命令后的数字不得发送旧正文: %v", fe.sendCalls)
	}
	if len(fe.switchCalls) != 1 || fe.switchCalls[0] != "c1" {
		t.Fatalf("命令后的数字应正常切换: %v", fe.switchCalls)
	}
}

// 其他菜单（#n/#m/#f）进行中：PickTarget 不覆盖它，正文不标记为已暂存。
func TestPickTargetBusyWithOtherMenu(t *testing.T) {
	fe := &fakeExecutor{
		tools: []MenuOption{{ID: "claude", Label: "Claude Code"}},
	}
	c := NewConsole(fe)
	c.Handle("#n")
	out, pending := c.PickTarget("正文")
	if pending {
		t.Fatal("#n 进行中不应建立待选菜单")
	}
	if !strings.Contains(join(out), "请先完成") {
		t.Fatalf("应提示先完成当前选择: %v", out)
	}
	if c.menu == nil || c.menu.kind != menuNew {
		t.Fatal("#n 菜单不应被覆盖")
	}
}

// PickTarget 只 trim，不改写正文（全角空格必须原样投递）。
func TestPickTargetPreservesOriginalText(t *testing.T) {
	fe := &fakeExecutor{listSessions: []SessionRef{
		wsRef(0, "c1", "", `D:\ws\a`, "claude", "会话A", false),
	}}
	c := NewConsole(fe)
	c.PickTarget("你好　世界")
	c.Handle("2") // 编号 1 是「新建」，c1 在编号 2
	if len(fe.sendCalls) != 1 || fe.sendCalls[0][1] != "你好　世界" {
		t.Fatalf("暂存正文应保持原文: %v", fe.sendCalls)
	}
}

// TryCancelPick：锁空闲时清掉 menuPick 并报告成功；锁被占用（Handle 持锁回调
// Executor 发送的形态）时返回 false 且不动菜单——调用方据此放弃，无损语义。
func TestTryCancelPick(t *testing.T) {
	fe := &fakeExecutor{listSessions: []SessionRef{
		wsRef(0, "c1", "", `D:\ws\a`, "claude", "会话A", false),
	}}
	c := NewConsole(fe)
	if _, pending := c.PickTarget("待发正文"); !pending {
		t.Fatal("应先进入待选菜单")
	}
	// 模拟 Handle 持锁：先占住锁再调 TryCancelPick。
	c.mu.Lock()
	if c.TryCancelPick() {
		c.mu.Unlock()
		t.Fatal("锁被占用时应报告失败")
	}
	if c.menu == nil || c.menu.kind != menuPick {
		c.mu.Unlock()
		t.Fatal("锁被占用时不得动菜单")
	}
	c.mu.Unlock()
	if !c.TryCancelPick() {
		t.Fatal("锁空闲时应报告成功")
	}
	if c.menu != nil {
		t.Fatal("成功后待选菜单应已清空")
	}
}

// 列表对终端运行中的会话追加〔终端〕标记（规则2：保留展示但明确不可投递）。
func TestHandleListMarksTerminalActive(t *testing.T) {
	ex := &fakeExecutor{listSessions: []SessionRef{
		{ChatID: "c1", Tool: "claude", Title: "普通会话"},
		{SessionID: "s2", Tool: "claude", Title: "终端会话", TerminalActive: true},
	}}
	c := NewConsoleLang(ex, "zh-CN")
	joined := join(c.Handle("#l"))
	if !strings.Contains(joined, "终端会话 〔终端〕") {
		t.Fatalf("终端会话应带〔终端〕标记: %q", joined)
	}
	if strings.Contains(joined, "普通会话 〔终端〕") {
		t.Fatalf("普通会话不应带标记: %q", joined)
	}
}

// errText：Executor 返回的 err.hub.* wire key 翻成命令层双语文案。
func TestConsoleErrTextTranslatesWireKeys(t *testing.T) {
	ex := &fakeExecutor{}
	c := NewConsoleLang(ex, "zh-CN")
	got := c.errText(errors.New("err.hub.terminal_active"))
	if got == "err.hub.terminal_active" || got == "" {
		t.Fatalf("wire key 应翻成文案, got %q", got)
	}
	if got := c.errText(errors.New("err.other.raw")); got != "err.other.raw" {
		t.Fatalf("未知 key 原样返回, got %q", got)
	}
}
