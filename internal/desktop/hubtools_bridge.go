package desktop

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/yangk/kshell/internal/chat"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/hubtools"
	"github.com/yangk/kshell/internal/requirements"
)

// hubSystemPrompt 是工具化链路的系统提示词占位：Task 6 换真实现（意图路由 + 完整人设）。
const hubSystemPrompt = "你是 kshell 的助手，可以调用工具查询本机需求、工作区与运行中会话。"

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

// hubRouter 承载 AgentHub 输入的工具化处理。会话生命周期 = 一条用户输入的
// 处理期 + 确认等待期：新输入到来时整体替换（旧 pending 因此失效）。
// 取舍：同屏只允许一条在途工具化对话，避免多会话确认卡错乱；用户在确认前
// 再发一条消息即视为放弃旧操作。
type hubRouter struct {
	app *App
	cfg hubtools.Config

	mu      sync.Mutex
	session *hubtools.Session        // 当前在途会话；HandlePrompt 开始时替换
	pending *hubtools.PendingConfirm // 本轮挂起的写操作（nil = 无）
}

// hubTools 返回输入路由器（App 级懒初始化单例）；未就绪（模型三元组缺失）
// 或显式关闭（RouteMode=agent）时返回 nil，调用方据此落回原直连链路。
func (a *App) hubTools() *hubRouter {
	a.hubMu.Lock()
	defer a.hubMu.Unlock()
	if a.hubRouter != nil {
		return a.hubRouter
	}
	cfg, ok := hubtools.FromModelConfig(a.snapshot().Config.Model)
	if !ok {
		return nil
	}
	if a.snapshot().Config.AgentHub.RouteMode == config.RouteModeAgent {
		return nil
	}
	a.hubRouter = &hubRouter{app: a, cfg: cfg}
	return a.hubRouter
}

// HandlePrompt 处理一条用户输入：构造会话调 LLM 工具循环 → 落盘 assistant
// 消息或 tool_confirm 卡片。返回 handled=true 表示输入已被工具化链路消费；
// handled=false 表示 LLM 调用失败，调用方落回原直连链路。
//
// user 消息落盘时机（取舍）：hub.Store 无删除 API，若先落盘后失败会出现
// 「原链路再落一条」的重复。因此这里延迟到 LLM 成功后才落 user 消息，
// 失败时 store 完全无痕，降级路径与未启用工具化完全一致。
// force_auto / auto 共用本入口；auto 的 LLM 意图判定由 Task 6 的 router.go
// 在此之前插入（当前 auto 暂直接进工具循环）。
func (h *hubRouter) HandlePrompt(sendID, text string) (bool, error) {
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
		{Role: "system", Content: hubSystemPrompt},
		{Role: "user", Content: text},
	}
	resp, err := hubtools.NewClient(h.cfg).ChatWithExecutor(
		context.Background(), msgs, reg.Tools(), exec)
	if err != nil {
		// LLM 失败：user 消息尚未落盘（延迟写），store 无痕，直接降级。
		return false, nil
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
	a.recordHubMessage(hub.Message{Kind: "agent", ChatID: a.originalChatID(chatID), Text: text})
	return nil
}
