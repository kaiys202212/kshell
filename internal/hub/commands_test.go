package hub

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

// fmtS 整数转字符串的简写。
func fmtS(n int) string { return strconv.Itoa(n) }

// fakeExecutor 是 Executor 的可配置假实现：各方法返回值/错误可注入，
// 并记录关键调用参数供断言。
type fakeExecutor struct {
	// 可配置返回值
	listSessions   []SessionRef
	currentSession SessionRef
	hasCurrent     bool
	switchErr      error
	easyNewRef     SessionRef
	easyNewErr     error
	tools          []MenuOption
	workspaces     []MenuOption
	models         []MenuOption
	createRef      SessionRef
	createErr      error
	stopErr        error
	listModels     []MenuOption
	listModelsCur  string
	listModelsErr  error
	setModelErr    error
	searchResult   []SessionRef
	sendErr        error
	grantErr       error
	status         ConsoleStatus
	openSessionRef SessionRef
	openSessionErr error
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

func (f *fakeExecutor) CurrentSession() (SessionRef, bool) {
	return f.currentSession, f.hasCurrent
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

func TestHelpWithCurrent(t *testing.T) {
	c := NewConsole(&fakeExecutor{
		hasCurrent:     true,
		currentSession: wsRef(0, "c1", "", `D:\work\demo`, "claude", "修复登录", true),
	})
	out := join(c.Handle("#?"))
	if !strings.Contains(out, "命令列表") || !strings.Contains(out, "#l") || !strings.Contains(out, "#st") {
		t.Fatalf("帮助应含命令表: %s", out)
	}
	if !strings.Contains(out, "当前会话: 修复登录 (claude)") {
		t.Fatalf("帮助应含当前会话行: %s", out)
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
		listSessions:   refs,
		hasCurrent:     true,
		currentSession: refs[2], // Current 由 ex.CurrentSession 比对得出
	}
	c := NewConsole(fe)

	out := join(c.Handle("#l"))
	lines := strings.Split(out, "\n")
	if len(lines) != 10 {
		t.Fatalf("应只列前 10 个，得到 %d 行: %q", len(lines), out)
	}
	if !strings.HasPrefix(lines[2], "▶3. ") {
		t.Fatalf("第 3 行应以 ▶3. 开头: %q", lines[2])
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
		listSessions:   []SessionRef{wsRef(0, "c1", "", `D:\a\b`, "codex", "标题", false)},
		hasCurrent:     true,
		currentSession: wsRef(0, "c1", "", `D:\a\b`, "codex", "标题", true),
	}
	c := NewConsole(fe)
	out := join(c.Handle("#l"))
	// c1 是当前会话，应标 ▶（由 #l 内 refreshCurrent 比对 ChatID 得出）。
	if !strings.HasPrefix(out, "▶1. [b] codex 标题") {
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
	if len(lines) != 12 {
		t.Fatalf("#la 应列全量 12 个，得到 %d 行", len(lines))
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
	if !strings.Contains(out, "已创建: [proj] claude 新会话") {
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
	out = join(c.Handle("2")) // codex
	if !strings.Contains(out, "选择项目") || !strings.Contains(out, "2. E:\\lib") {
		t.Fatalf("step2 应列项目: %s", out)
	}
	out = join(c.Handle("1")) // ws1
	if !strings.Contains(out, "选择模型") {
		t.Fatalf("step3 应列模型: %s", out)
	}
	out = join(c.Handle("2")) // opus
	if !strings.Contains(out, "已创建") {
		t.Fatalf("创建成功应回已创建: %s", out)
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
	if !strings.Contains(out, "已创建") {
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
		hasCurrent:     true,
		currentSession: wsRef(0, "c1", "sid1", "", "claude", "A", true),
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
		hasCurrent:     true,
		currentSession: wsRef(0, "", "sid-only", "", "claude", "磁盘会话", true),
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
		hasCurrent:     true,
		currentSession: wsRef(0, "c1", "", "", "claude", "A", true),
		listModels:     []MenuOption{{ID: "sonnet", Label: "Sonnet"}, {ID: "opus", Label: "Opus"}},
		listModelsCur:  "opus",
	}
	c := NewConsole(fe)
	out := join(c.Handle("#m"))
	if !strings.Contains(out, "选择模型") || !strings.Contains(out, "▶2. Opus") {
		t.Fatalf("#m 应列模型并标当前: %s", out)
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
		hasCurrent:     true,
		currentSession: wsRef(0, "c1", "", "", "claude", "A", true),
		listModels:     []MenuOption{{ID: "sonnet", Label: "Sonnet"}},
		setModelErr:    errors.New("nope"),
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
		hasCurrent:     true,
		currentSession: wsRef(0, "c2", "", "", "codex", "B", true),
	}
	c := NewConsole(fe)
	c.Handle("#l") // #g 依据上次列表的编号映射
	out := join(c.Handle("#g 1 hello world"))
	if !strings.Contains(out, "已发送到: A") {
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
	want := "当前会话: 修复登录\n运行中任务: 2\n未送达消息: 5"
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
		hasCurrent:     true,
		currentSession: wsRef(0, "c1", "", "", "claude", "A", true),
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
		hasCurrent:     true,
		currentSession: wsRef(0, "c1", "", "", "claude", "A", true),
	}
	c := NewConsole(fe)
	if got := c.Handle("你好"); len(got) != 0 {
		t.Fatalf("普通文本发送成功不应回显: %v", got)
	}
	if len(fe.sendCalls) != 1 || fe.sendCalls[0] != [2]string{"c1", "你好"} {
		t.Fatalf("Send 参数不符: %v", fe.sendCalls)
	}
	// 无 current
	out := join(NewConsole(&fakeExecutor{}).Handle("你好"))
	if !strings.Contains(out, "当前无活跃会话") {
		t.Fatalf("无 current 普通文本应提示: %q", out)
	}
	// 发送失败
	fe2 := &fakeExecutor{
		hasCurrent:     true,
		currentSession: wsRef(0, "c1", "", "", "claude", "A", true),
		sendErr:        errors.New("offline"),
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
		hasCurrent:     true,
		currentSession: wsRef(0, "c1", "s1", `D:\ws\a`, "claude", "活跃会话", true),
		openSessionRef: wsRef(0, "c9", "s2", `D:\ws\b`, "codex", "磁盘会话", false),
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
