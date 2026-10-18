package desktop

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/hubtools"
	"github.com/yangk/kshell/internal/requirements"
)

// desktopBridge 把 App 既有能力适配为 hubtools.Bridge：只做视图映射，不重复实现查询。
type desktopBridge struct {
	app *App
}

// toRequirementView 把 requirements.Item 映射为 hubtools 视图（字段同名对齐）。
func toRequirementView(it requirements.Item) hubtools.RequirementView {
	return hubtools.RequirementView{
		ID:           it.ID,
		Text:         it.Text,
		Priority:     it.Priority,
		Tags:         it.Tags,
		Project:      it.Project,
		Status:       it.Status,
		DispatchedTo: it.DispatchedTo,
		SessionTitle: it.SessionTitle,
	}
}

func (b *desktopBridge) ListRequirements() ([]hubtools.RequirementView, error) {
	items := b.app.ListRequirements()
	out := make([]hubtools.RequirementView, 0, len(items))
	for _, it := range items {
		out = append(out, toRequirementView(it))
	}
	return out, nil
}

func (b *desktopBridge) GetRequirement(id string) (*hubtools.RequirementView, error) {
	for _, it := range b.app.ListRequirements() {
		if it.ID == id {
			v := toRequirementView(it)
			return &v, nil
		}
	}
	return nil, nil
}

func (b *desktopBridge) CreateRequirement(project, text, priority string, tags []string) (hubtools.RequirementView, error) {
	it, err := b.app.AddRequirement(project, text, priority, tags)
	if err != nil {
		return hubtools.RequirementView{}, err
	}
	return toRequirementView(it), nil
}

func (b *desktopBridge) UpdateRequirement(id, project, text, priority string, tags []string) (hubtools.RequirementView, error) {
	it, err := b.app.UpdateRequirement(id, project, text, priority, tags)
	if err != nil {
		return hubtools.RequirementView{}, err
	}
	return toRequirementView(it), nil
}

func (b *desktopBridge) DeleteRequirement(id string) error {
	return b.app.DeleteRequirement(id)
}

// workspaceViewID 复用 presentWorkspaces 的标识规则：local 用 Path，ssh 的 Path
// 已被填成 ssh:// Ref（ConnID+RemotePath），二者天然一致，统一取 Path。
func workspaceViewID(w discovery.Workspace) string { return w.Path }

func toWorkspaceView(w discovery.Workspace) hubtools.WorkspaceView {
	kind := w.Kind
	if kind == "" {
		kind = discovery.KindLocal
	}
	lastUsed := ""
	if !w.LastUsed.IsZero() {
		lastUsed = w.LastUsed.Format("2006-01-02 15:04:05")
	}
	return hubtools.WorkspaceView{
		ID:           workspaceViewID(w),
		Name:         w.Name,
		Path:         w.Path,
		SessionCount: w.SessionCount,
		ToolCounts:   w.ToolCounts,
		LastUsed:     lastUsed,
		Kind:         kind,
		ConnName:     w.ConnName,
	}
}

func (b *desktopBridge) ListWorkspaces() ([]hubtools.WorkspaceView, error) {
	list := b.app.GetWorkspaces()
	out := make([]hubtools.WorkspaceView, 0, len(list))
	for _, w := range list {
		out = append(out, toWorkspaceView(w))
	}
	return out, nil
}

func (b *desktopBridge) GetWorkspace(id string) (*hubtools.WorkspaceView, error) {
	for _, w := range b.app.GetWorkspaces() {
		if workspaceViewID(w) == id {
			v := toWorkspaceView(w)
			return &v, nil
		}
	}
	return nil, nil
}

// runningTasks 枚举运行中会话并映射为任务视图；List/Get 共用。
// starting/running（正在干活）才算任务；exited/ready 不进列表。
func (b *desktopBridge) runningTasks() []hubtools.TaskView {
	m := b.app.chats()
	if m == nil {
		return nil
	}
	var out []hubtools.TaskView
	for _, info := range m.List() {
		if info.Status != chat.StatusRunning && info.Status != chat.StatusStarting {
			continue
		}
		out = append(out, hubtools.TaskView{
			ID:        info.ID,
			Title:     info.Title,
			ToolID:    info.ToolID,
			Workspace: info.Workspace,
			Status:    info.Status,
		})
	}
	return out
}

func (b *desktopBridge) ListRunningTasks() ([]hubtools.TaskView, error) {
	return b.runningTasks(), nil
}

func (b *desktopBridge) GetTaskDetail(id string) (*hubtools.TaskView, error) {
	for _, tk := range b.runningTasks() {
		if tk.ID == id {
			v := tk
			return &v, nil
		}
	}
	return nil, nil
}

// hubPromptTimeout 是单条输入完整工具循环（含 LLM 多轮请求）的超时上限。
const hubPromptTimeout = 90 * time.Second

// hubRouter 承载 AgentHub 输入的工具化处理。会话生命周期 = 一条用户输入的
// 处理期 + 确认等待期：新输入到来时整体替换（旧 pending 因此失效）。
// 取舍：同屏只允许一条在途工具化对话（inflight 串行化），避免多会话确认卡
// 错乱；在途期间新输入直接降级原链路（ACP 忙时排队语义接管）。
type hubRouter struct {
	app *App
	cfg hubtools.Config

	inflight sync.Mutex // HandlePrompt 串行化（TryLock）

	mu      sync.Mutex
	session *hubtools.Session        // 当前在途会话；HandlePrompt 开始时替换
	pending *hubtools.PendingConfirm // 本轮挂起的写操作（nil = 无）
}

// hubTools 返回输入路由器；未就绪（模型三元组缺失）或显式关闭（RouteMode=agent）
// 时返回 nil，调用方据此落回原直连链路。
// 配置时效：每次调用重读配置判断就绪与模式——关闭态不缓存（回改配置立即恢复），
// 就绪态配置变更时重建路由器（旧 session/pending 一并失效，即用户改模型配置
// 会丢弃未确认的写操作，属可接受的显式行为）。
func (a *App) hubTools() *hubRouter {
	a.hubMu.Lock()
	defer a.hubMu.Unlock()
	cfg, ok := hubtools.FromModelConfig(a.snapshot().Config.Model)
	if !ok || a.snapshot().Config.AgentHub.RouteMode == config.RouteModeAgent {
		return nil
	}
	if a.hubRouter != nil && a.hubRouter.cfg == cfg {
		return a.hubRouter
	}
	a.hubRouter = &hubRouter{app: a, cfg: cfg}
	return a.hubRouter
}

// HandlePrompt 处理一条用户输入：构造会话调 LLM 工具循环 → 落盘 assistant
// 消息或 tool_confirm 卡片。返回 handled=true 表示输入已被工具化链路消费；
// handled=false 表示应落回原直连链路（LLM 失败 / 意图路由判为非数据操作 /
// 空回复 / 已有在途对话）。
//
// user 消息落盘时机（取舍）：hub.Store 无删除 API，若先落盘后失败会出现
// 「原链路再落一条」的重复。因此这里延迟到 LLM 成功后才落 user 消息，
// 失败时 store 完全无痕，降级路径与未启用工具化完全一致。
// force_auto / auto 共用本入口：auto 模式系统提示词含路由规则，模型输出
// route=agent 时降级原链路；force_auto 跳过路由判断直接进工具循环。
func (h *hubRouter) HandlePrompt(sendID, text string) (bool, error) {
	// 串行化：已有在途工具化对话时直接降级原链路，不触碰其 session/pending
	//（否则会互相污染挂起状态与确认卡归属）。
	if !h.inflight.TryLock() {
		return false, nil
	}
	defer h.inflight.Unlock()

	reg := hubtools.NewRegistry(&desktopBridge{app: h.app})

	// 会话生命周期：每条输入一轮，替换旧会话（旧 pending 失效）
	h.mu.Lock()
	s := hubtools.NewSession(h.cfg, reg, nil)
	h.session = s
	h.pending = nil
	h.mu.Unlock()

	// executor：经 Session.Execute 执行（只读直执行，Mutating 挂起登记）。
	// 挂起时给模型回文本说明（非 error：错误会被 llm.go 再包一层 tool error）。
	exec := func(ctx context.Context, name, argsJSON string) (string, error) {
		result, pc, err := s.Execute(ctx, name, argsJSON)
		if pc != nil {
			h.mu.Lock()
			h.pending = pc
			h.mu.Unlock()
			return "该写操作已提交用户确认，等待用户决定，请勿重复调用。", nil
		}
		return result, err
	}

	msgs := []hubtools.Message{
		{Role: "system", Content: hubtools.BuildSystemPrompt(reg.Tools(), h.routeAware())},
		{Role: "user", Content: text},
	}
	ctx, cancel := context.WithTimeout(context.Background(), hubPromptTimeout)
	defer cancel()
	resp, err := hubtools.NewClient(h.cfg).ChatWithExecutor(
		ctx, msgs, reg.Tools(), exec)
	if err != nil {
		// LLM 失败：user 消息尚未落盘（延迟写），store 无痕，直接降级；
		// 同时清除在途会话与挂起，防止从未展示确认卡的操作仍可被 ConfirmHubTool 消费。
		h.clearTurn()
		return false, nil
	}

	// 模型完成但无挂起时的降级判定（须在落盘前做，此时 user 消息尚未落盘，
	// store 无痕，原链路接管后不重复）：
	// 1) 空文本 = 路由未产出结果；
	// 2) auto 模式下模型输出 route=agent = 意图判定为非数据操作，原文转投原链路。
	if h.currentPending() == nil {
		if resp.Text == "" {
			h.clearTurn()
			return false, nil
		}
		if h.routeAware() && hubtools.ParseRouteAgent(resp.Text) {
			h.clearTurn()
			return false, nil
		}
	}

	// LLM 成功：先落 user 消息（归因快照），再落结果
	userMsg := hub.Message{Kind: "user", ChatID: h.app.originalChatID(sendID), Text: text}
	if m := h.app.chats(); m != nil {
		if info, ok := m.Get(sendID); ok {
			userMsg.Tool = info.ToolID
			userMsg.Workspace = info.Workspace
			userMsg.SessionTitle = info.Title
			userMsg.SessionID = info.SessionID
		}
	}
	h.app.recordHubMessage(userMsg)

	chatID := userMsg.ChatID
	if h.currentPending() != nil {
		// Mutating 工具挂起：落 tool_confirm 卡片（Event 标记 + 尾部 JSON 供前端回传）
		h.mu.Lock()
		pc := h.pending
		h.mu.Unlock()
		tail, _ := json.Marshal(map[string]string{"pending": pc.PendingID, "tool": pc.ToolName})
		h.app.recordHubMessage(hub.Message{
			Kind: "agent", Event: "tool_confirm", ChatID: chatID,
			Tool: userMsg.Tool, Workspace: userMsg.Workspace, SessionTitle: userMsg.SessionTitle,
			Text: pc.Summary + "\n" + string(tail),
		})
		return true, nil
	}

	h.app.recordHubMessage(hub.Message{
		Kind: "agent", ChatID: chatID,
		Tool: userMsg.Tool, Workspace: userMsg.Workspace, SessionTitle: userMsg.SessionTitle,
		Text: resp.Text,
	})
	return true, nil
}

// clearTurn 清除当前在途会话与挂起操作（降级/异常收尾时调用）。
func (h *hubRouter) clearTurn() {
	h.mu.Lock()
	h.session = nil
	h.pending = nil
	h.mu.Unlock()
}

// routeAware 报告当前是否启用意图路由判定（auto 默认启用；force_auto 跳过，
// 输入无条件进工具循环）。动态读配置，与 hubTools 的配置时效语义一致。
func (h *hubRouter) routeAware() bool {
	return h.app.snapshot().Config.AgentHub.RouteMode != config.RouteModeForceAuto
}

// currentPending 取当前挂起操作（可能为 nil）。
func (h *hubRouter) currentPending() *hubtools.PendingConfirm {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.pending
}

// ConfirmHubTool 处理用户对挂起写操作的确认/拒绝：路由到当前在途会话的
// 确认状态机，结果以 assistant 消息落盘。会话已被新输入替换（pending 失效）
// 或无在途会话时返回未知挂起错误。前端 Task 7 接绑定。
func (a *App) ConfirmHubTool(chatID, pendingID string, approved bool) error {
	h := a.hubTools()
	if h == nil {
		return errNotReady
	}
	h.mu.Lock()
	s := h.session
	h.mu.Unlock()
	if s == nil {
		return hubtools.ErrUnknownPending
	}
	text, err := s.Confirm(context.Background(), pendingID, approved)
	if err != nil {
		return err
	}
	// 设计要求：确认结果喂回 LLM 组织成自然语言再落盘；LLM 失败/空回复时
	// 降级直接落工具结果（用户仍能看到发生了什么，只是少了润色）。
	if approved {
		if final := h.summarizeConfirm(text); final != "" {
			text = final
		}
	}
	a.recordHubMessage(hub.Message{Kind: "agent", ChatID: a.originalChatID(chatID), Text: text})
	return nil
}

// summarizeConfirm 把写操作的工具结果 JSON 交回 LLM 组织成一句自然语言确认。
// 失败或空回复返回空串（调用方降级落原始结果）。
func (h *hubRouter) summarizeConfirm(result string) string {
	msgs := []hubtools.Message{
		{Role: "system", Content: hubtools.BuildSystemPrompt(nil, false)},
		{Role: "user", Content: "写操作已由用户确认并执行完成，工具返回结果：\n" + result +
			"\n\n请用一句简洁中文向用户说明执行结果，不要输出 JSON。"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), hubPromptTimeout)
	defer cancel()
	resp, err := hubtools.NewClient(h.cfg).Chat(ctx, msgs, nil)
	if err != nil {
		return ""
	}
	return resp.Text
}
