package desktop

import (
	"fmt"
	"strings"

	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/hub"
)

// wxSessionsCap 是微信 #l/#la 可见会话的合成列表总上限（活跃 + 磁盘合并后截断）。
const wxSessionsCap = 30

// wxExecutor 把微信命令层的抽象动作落到 App 既有 API 上。
// 与前端 AgentHub 共用同一套后端调用，不复制业务逻辑；不持有 App 以外的状态。
type wxExecutor struct{ a *App }

// newWXExecutor 装配微信命令层的桌面端执行后端。
func newWXExecutor(a *App) *wxExecutor { return &wxExecutor{a: a} }

// 编译期保证 wxExecutor 满足命令层的 Executor 接口。
var _ hub.Executor = (*wxExecutor)(nil)

// wxChatInfo 是会话合成列表的中间形态：活跃 chat 或磁盘会话二选一。
type wxChatInfo struct {
	chatID    string // 活跃 chat ID；磁盘会话为空
	sessionID string // 磁盘会话 ID（活跃 chat 也可能绑定）
	tool      string
	workspace string
	title     string
}

// collectSessions 合成两路会话列表：活跃 chat 在前（ListChats 顺序），磁盘会话
// 补充在后。同 SessionID 只保留活跃版（磁盘重复项丢弃）；总数截到 wxSessionsCap。
func (w *wxExecutor) collectSessions() []wxChatInfo {
	var out []wxChatInfo
	seenSession := make(map[string]bool)
	for _, info := range w.a.ListChats() {
		out = append(out, wxChatInfo{
			chatID:    info.ID,
			sessionID: info.SessionID,
			tool:      info.ToolID,
			workspace: info.Workspace,
			title:     info.Title,
		})
		if info.SessionID != "" {
			seenSession[info.SessionID] = true
		}
	}
	for _, s := range w.a.GetSessions() {
		if s.ID == "" || seenSession[s.ID] {
			continue // 活跃优先：已激活的磁盘会话不再重复出现
		}
		seenSession[s.ID] = true
		out = append(out, wxChatInfo{
			sessionID: s.ID,
			tool:      s.ToolID,
			workspace: s.Workspace,
			title:     s.Title,
		})
	}
	if len(out) > wxSessionsCap {
		out = out[:wxSessionsCap]
	}
	return out
}

// ListSessions 返回命令层可见的可切换会话（活跃在前、组内按现返回顺序）。
func (w *wxExecutor) ListSessions() []hub.SessionRef {
	list := w.collectSessions()
	out := make([]hub.SessionRef, 0, len(list))
	for _, s := range list {
		out = append(out, hub.SessionRef{
			ChatID:    s.chatID,
			SessionID: s.sessionID,
			Workspace: s.workspace,
			Tool:      s.tool,
			ToolName:  w.a.toolDisplayName(s.tool),
			Title:     s.title,
		})
	}
	return out
}

// CurrentSession 从合成列表里找 hub 最后活跃会话；无 hub 记录时回落第一个活跃 chat。
func (w *wxExecutor) CurrentSession() (hub.SessionRef, bool) {
	refs := w.ListSessions()
	if len(refs) == 0 {
		return hub.SessionRef{}, false
	}
	// hub 落盘 ChatID 是前端 ID 空间（链头），与 ListSessions 里的 ChatID 同口径。
	var lastChatID string
	if store := w.a.snapshot().HubStore; store != nil {
		if msgs := store.List(); len(msgs) > 0 {
			lastChatID = msgs[len(msgs)-1].ChatID
		}
	}
	if lastChatID != "" {
		for _, r := range refs {
			if r.ChatID == lastChatID {
				return r, true
			}
		}
	}
	// 回落：ListChats 第一个（refs 活跃在前，等价于首个 ChatID 非空的条目）。
	if refs[0].ChatID != "" {
		return refs[0], true
	}
	return hub.SessionRef{}, false
}

// SwitchTo 轻量校验目标 chat 存在即成功（微信切上下文不需要真的在桌面端弹窗，
// 真正的活跃性检查由 SendChatPrompt 内的 ensureChatAlive 负责）。
func (w *wxExecutor) SwitchTo(chatID string) error {
	m := w.a.chats()
	if m == nil {
		return errNotReady
	}
	resolved, _ := w.a.resolveChatID(chatID)
	if _, ok := m.Get(resolved); !ok {
		return chatGoneErr
	}
	return nil
}

// OpenSession 激活一个尚未打开的磁盘会话（走 OpenSessionACP），返回可投递引用。
func (w *wxExecutor) OpenSession(sessionID string) (hub.SessionRef, error) {
	o, err := w.a.OpenSessionACP(sessionID)
	if err != nil {
		return hub.SessionRef{}, err
	}
	if o.Kind != "chat" || o.Chat == nil {
		return hub.SessionRef{}, fmt.Errorf("err.weixin.open_not_chat|%s", o.Kind)
	}
	return hub.SessionRef{
		ChatID:    w.a.originalChatID(o.Chat.ID),
		SessionID: o.Chat.SessionID,
		Workspace: o.Chat.Workspace,
		Tool:      o.Chat.ToolID,
		ToolName:  w.a.toolDisplayName(o.Chat.ToolID),
		Title:     o.Chat.Title,
	}, nil
}

// easyNewToolID 选快速新建的工具：配置里的 AgentHub 默认工具优先（已安装
// 校验，失效回落），未配置回落第一个已安装工具。
func (w *wxExecutor) easyNewToolID() string {
	toolID := w.a.snapshot().Config.AgentHub.DefaultTool
	if toolID != "" && !w.toolInstalled(toolID) {
		toolID = "" // 配置的工具已卸载：回落第一个已安装
	}
	if toolID == "" {
		for _, t := range w.a.GetTools() {
			if t.Installed {
				toolID = t.ID
				break
			}
		}
	}
	return toolID
}

// EasyNew 快速新建：默认工具 = easyNewToolID（配置默认优先），默认工作区 =
// 第一个工作区；模型不指定（launch 层自行用默认）。
func (w *wxExecutor) EasyNew() (hub.SessionRef, error) {
	toolID := w.easyNewToolID()
	if toolID == "" {
		return hub.SessionRef{}, fmt.Errorf("err.weixin.no_tools")
	}
	ws := w.a.GetWorkspaces()
	if len(ws) == 0 {
		return hub.SessionRef{}, fmt.Errorf("err.weixin.no_workspaces")
	}
	return w.CreateNew(toolID, ws[0].Path, "")
}

// toolInstalled 报告工具 ID 是否在已安装列表里。
func (w *wxExecutor) toolInstalled(id string) bool {
	for _, t := range w.a.GetTools() {
		if t.ID == id {
			return t.Installed
		}
	}
	return false
}

// NewMenuOptions 返回 #n 分步新建的选项：工具（已安装）、工作区（末段路径展示）、
// 模型恒空（新建时不指定模型，由 launch 默认）。
func (w *wxExecutor) NewMenuOptions() (tools, workspaces, models []hub.MenuOption) {
	for _, t := range w.a.GetTools() {
		if !t.Installed {
			continue
		}
		label := t.ID
		if t.Name != "" {
			label = t.Name
		}
		tools = append(tools, hub.MenuOption{ID: t.ID, Label: label})
	}
	for _, ws := range w.a.GetWorkspaces() {
		workspaces = append(workspaces, hub.MenuOption{ID: ws.Path, Label: wxLastBase(ws.Path)})
	}
	return tools, workspaces, nil
}

// CreateNew 在指定工作区以指定工具新建 ACP 会话；model 忽略（launch 默认）。
func (w *wxExecutor) CreateNew(toolID, wsID, model string) (hub.SessionRef, error) {
	_ = model
	o, err := w.a.OpenWorkspaceACP(wsID, toolID)
	if err != nil {
		return hub.SessionRef{}, err
	}
	if o.Kind != "chat" || o.Chat == nil {
		return hub.SessionRef{}, fmt.Errorf("err.weixin.open_not_chat|%s", o.Kind)
	}
	return hub.SessionRef{
		ChatID:    w.a.originalChatID(o.Chat.ID),
		SessionID: o.Chat.SessionID,
		Workspace: o.Chat.Workspace,
		Tool:      o.Chat.ToolID,
		ToolName:  w.a.toolDisplayName(o.Chat.ToolID),
		Title:     o.Chat.Title,
	}, nil
}

// Stop 中断指定会话当前轮次。
func (w *wxExecutor) Stop(chatID string) error {
	return w.a.CancelChat(chatID)
}

// ListModels 返回会话可选模型；agent 未报告模型信息（ok=false）时返回空列表
//（console 对空列表回「该会话无可选模型」），不算错误。
func (w *wxExecutor) ListModels(chatID string) ([]hub.MenuOption, string, error) {
	v, err := w.a.ListChatModels(chatID)
	if err != nil {
		return nil, "", err
	}
	out := make([]hub.MenuOption, 0, len(v.Models))
	for _, m := range v.Models {
		label := m.Name
		if label == "" {
			label = m.ID
		}
		out = append(out, hub.MenuOption{ID: m.ID, Label: label})
	}
	return out, v.Current, nil
}

// SetModel 运行时切换会话模型。
func (w *wxExecutor) SetModel(chatID, model string) error {
	return w.a.SetChatModel(chatID, model)
}

// SearchSessions 在合成列表里按 Title/Workspace/Tool 不区分大小写包含匹配。
func (w *wxExecutor) SearchSessions(kw string) []hub.SessionRef {
	kw = strings.ToLower(strings.TrimSpace(kw))
	if kw == "" {
		return nil
	}
	var out []hub.SessionRef
	for _, r := range w.ListSessions() {
		hay := strings.ToLower(r.Title + "\n" + r.Workspace + "\n" + r.Tool)
		if strings.Contains(hay, kw) {
			out = append(out, r)
		}
	}
	return out
}

// Send 向指定会话发送一轮用户输入（含 hub 落盘、忙时入队等既有链路）。
func (w *wxExecutor) Send(chatID, text string) error {
	return w.a.SendChatPrompt(chatID, text)
}

// GrantPermission 处理权限请求：允许优先 allow_once（与 manager 自动放行同一
// 优先序，这里固定单次允许，常授权仍由桌面端弹窗选择），拒绝用 ACP 标准的
// reject_once。optionID 不存在时 manager 报错，console 层按失效处理。
func (w *wxExecutor) GrantPermission(chatID, reqID string, allow bool) error {
	if allow {
		return w.a.RespondChatPermission(chatID, reqID, "allow_once")
	}
	return w.a.RespondChatPermission(chatID, reqID, "reject_once")
}

// Status 汇总状态快照。Undelivered 由装配方注入（微信桥出站队列长度），
// 本适配器不掌握该状态，恒返回 0。
func (w *wxExecutor) Status() hub.ConsoleStatus {
	running := 0
	for _, info := range w.a.ListChats() {
		if info.Status == chat.StatusRunning {
			running++
		}
	}
	st := hub.ConsoleStatus{Running: running}
	if cur, ok := w.CurrentSession(); ok {
		st.CurrentTitle = cur.Title
	}
	return st
}

// RecentMessages 取 AgentHub 最近 n 条消息（时间正序），供 #h 命令。
// store 未装配（未就绪/测试）或无历史时返回空。
func (w *wxExecutor) RecentMessages(n int) []hub.Message {
	store := w.a.snapshot().HubStore
	if store == nil || n <= 0 {
		return nil
	}
	msgs := store.List()
	if len(msgs) > n {
		msgs = msgs[len(msgs)-n:]
	}
	out := make([]hub.Message, len(msgs))
	copy(out, msgs)
	return out
}

// wxLastBase 取路径末段：与 hub 包内 lastBase 同一实现（跨渠道文本里的路径
// 分隔不保证与运行平台一致），desktop 包不便导出复用，这里独立一份。
func wxLastBase(p string) string {
	if p == "" {
		return ""
	}
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}
