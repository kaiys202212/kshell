package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

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

// RecentMessages 从 hub store 取最近 n 条纯文本消息映射为提示词摘要。
func (b *desktopBridge) RecentMessages(n int) []hubtools.ChatLogEntry {
	store := b.app.snapshot().HubStore
	if store == nil {
		return nil
	}
	all := store.List()
	out := make([]hubtools.ChatLogEntry, 0, n)
	for _, m := range all {
		// 只取纯文本对话（排除 ack 卡片、确认卡等事件消息）。
		if m.Event != "" || m.Status != "" || m.Text == "" {
			continue
		}
		sender := "助手"
		if m.Kind == "user" {
			sender = "用户"
		}
		agent := m.Tool
		if m.SessionTitle != "" {
			agent += "「" + m.SessionTitle + "」"
		}
		text := []rune(m.Text)
		if len(text) > 200 {
			text = text[:200]
		}
		out = append(out, hubtools.ChatLogEntry{Agent: agent, Sender: sender, Text: string(text), ChatID: m.ChatID})
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

// wxAgentExecutor 返回命令层执行后端：agent 会话工具与 #l/#st/#g 同源
//（编号语境、ACP 能力过滤、激活链路都复用 wxExecutor，不复制业务）。
func (b *desktopBridge) wxAgentExecutor() *wxExecutor { return newWXExecutor(b.app) }

// ListAgents 把命令层会话列表 + 状态合成映射为 agent 视图：编号 = ListSessions
// 索引+1（与 #l/智能路由同源），状态取 SessionStatuses（活跃 chat 精确命中），
// 磁盘未激活会话退可投递性粗判（终端占用 → gone，其余 idle）。
func (b *desktopBridge) ListAgents() ([]hubtools.AgentView, error) {
	w := b.wxAgentExecutor()
	refs := w.ListSessions()
	byKey := make(map[string]hub.SessionStatus)
	for _, s := range w.SessionStatuses() {
		for _, k := range []string{s.Ref.ChatID, s.Ref.Key, s.Ref.SessionID} {
			if k != "" {
				byKey[k] = s
			}
		}
	}
	out := make([]hubtools.AgentView, 0, len(refs))
	for i, r := range refs {
		v := hubtools.AgentView{
			Num:           i + 1,
			ChatID:        r.ChatID,
			Tool:          r.Tool,
			ToolName:      r.ToolName,
			WorkspaceName: wxLastBase(r.Workspace),
			WorkspacePath: r.Workspace,
			Title:         r.Title,
			Status:        "idle",
		}
		if r.TerminalActive {
			v.Status = "gone"
		}
		for _, k := range []string{r.ChatID, r.Key, r.SessionID} {
			if s, ok := byKey[k]; ok {
				v.Status = s.State
				v.Summary = s.Summary
				break
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// agentByNum 解析编号字符串为命令层会话引用（与 list_agents 编号同源）。
func (b *desktopBridge) agentByNum(id string) (hub.SessionRef, error) {
	n, err := strconv.Atoi(strings.TrimSpace(id))
	if err != nil {
		return hub.SessionRef{}, fmt.Errorf("会话编号必须是数字: %s", id)
	}
	w := b.wxAgentExecutor()
	refs := w.ListSessions()
	if n < 1 || n > len(refs) {
		return hub.SessionRef{}, fmt.Errorf("会话编号 %d 不在 1..%d 内", n, len(refs))
	}
	return refs[n-1], nil
}

// GetAgentStatus 查单个会话状态；recent>0 时附该会话最近 recent 条可见消息
//（活跃 chat 才有 hub 历史，磁盘未激活会话无 recent）。
func (b *desktopBridge) GetAgentStatus(id string, recent int) (*hubtools.AgentStatusView, error) {
	agents, err := b.ListAgents()
	if err != nil {
		return nil, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(id))
	if err != nil {
		return nil, fmt.Errorf("会话编号必须是数字: %s", id)
	}
	for _, v := range agents {
		if v.Num != n {
			continue
		}
		out := &hubtools.AgentStatusView{Agent: v}
		if recent > 0 && v.ChatID != "" {
			// recent 上限 20（与 schema 描述一致），防模型传大数拖垮提示词。
			if recent > 20 {
				recent = 20
			}
			for _, m := range b.wxAgentExecutor().SessionRecentMessages(v.ChatID, recent) {
				sender := "助手"
				if m.Kind == "user" {
					sender = "用户"
				}
				text := []rune(m.Text)
				if len(text) > 200 {
					text = text[:200]
				}
				out.Recent = append(out.Recent, hubtools.ChatLogEntry{
					Agent: v.Title, Sender: sender, Text: string(text), ChatID: v.ChatID,
				})
			}
		}
		return out, nil
	}
	return nil, nil
}

// agentWorkspacePath 把 create_agent 的 workspace 参数解析为工作区路径：
// 空 → 第一个工作区；非空按 Name/Path 精确（不区分大小写）优先、包含次之
// 模糊匹配（如「策略」命中名称含策略的工作区）。无命中报错并附可选列表，
// 由模型转述给用户确认。
func (b *desktopBridge) agentWorkspacePath(workspace string) (string, error) {
	ws := b.app.GetWorkspaces()
	if len(ws) == 0 {
		return "", fmt.Errorf("err.weixin.no_workspaces")
	}
	if strings.TrimSpace(workspace) == "" {
		return ws[0].Path, nil
	}
	kw := strings.ToLower(strings.TrimSpace(workspace))
	type hit struct {
		path  string
		score int // 2=精确命中 1=包含
	}
	var best *hit
	var fuzzy []string // score=1 的命中路径（用于多义时报错反问）
	for _, w := range ws {
		name := strings.ToLower(w.Name)
		path := strings.ToLower(w.Path)
		s := 0
		if name == kw || path == kw {
			s = 2
		} else if strings.Contains(name, kw) || strings.Contains(path, kw) {
			s = 1
		}
		if s == 0 {
			continue
		}
		if s == 1 {
			fuzzy = append(fuzzy, w.Path)
		}
		if best == nil || s > best.score {
			best = &hit{path: w.Path, score: s}
		}
	}
	if best != nil {
		// 多个包含命中且无精确命中：不替用户猜，报候选让模型反问。
		if best.score == 1 && len(fuzzy) > 1 {
			return "", fmt.Errorf("工作区 %q 匹配到多个项目，请用户确认: %s", workspace, strings.Join(fuzzy, ", "))
		}
		return best.path, nil
	}
	names := make([]string, 0, len(ws))
	for _, w := range ws {
		names = append(names, w.Name)
	}
	return "", fmt.Errorf("工作区匹配不到 %q，可选: %s", workspace, strings.Join(names, ", "))
}

// agentToolID 解析 create_agent 的 tool 参数：空 → AgentHub 默认工具（失效
// 回落第一个已安装，与 EasyNew 同口径）；非空校验已安装。
func (b *desktopBridge) agentToolID(tool string) (string, error) {
	w := b.wxAgentExecutor()
	if strings.TrimSpace(tool) == "" {
		id := w.easyNewToolID()
		if id == "" {
			return "", fmt.Errorf("err.weixin.no_tools")
		}
		return id, nil
	}
	if !w.toolInstalled(tool) {
		return "", fmt.Errorf("工具未安装或不存在: %s", tool)
	}
	return tool, nil
}

// CreateAgent 新建会话：workspace/tool/model 解析后走命令层 CreateNew
//（与 #n 新建同链路，含 hub:busy 事件与 SetModel 对齐）。
func (b *desktopBridge) CreateAgent(workspace, tool, model string) (hubtools.AgentView, error) {
	wsPath, err := b.agentWorkspacePath(workspace)
	if err != nil {
		return hubtools.AgentView{}, err
	}
	toolID, err := b.agentToolID(tool)
	if err != nil {
		return hubtools.AgentView{}, err
	}
	ref, err := b.wxAgentExecutor().CreateNew(toolID, wsPath, model)
	if err != nil {
		return hubtools.AgentView{}, err
	}
	// 新会话即最新列表末位（活跃 chat 补充在磁盘之后或按 ListChats 顺序），
	// 编号语境易漂移，返回 num=0 让模型以 ChatID 语义感知「刚创建」，
	// 下一轮 list_agents 重新取编号。
	return hubtools.AgentView{
		ChatID:        ref.ChatID,
		Tool:          ref.Tool,
		ToolName:      ref.ToolName,
		WorkspaceName: wxLastBase(ref.Workspace),
		WorkspacePath: ref.Workspace,
		Title:         ref.Title,
		Status:        "running",
	}, nil
}

// SendToAgent 投递消息到指定编号会话：活跃 chat 直接走命令层 Send（含
// ensureChatAlive/hub 落盘/忙时入队）；未激活磁盘会话先 OpenSession 激活。
// 终端占用（gone）不可投递，显性报错。
func (b *desktopBridge) SendToAgent(id, text string) (string, error) {
	ref, err := b.agentByNum(id)
	if err != nil {
		return "", err
	}
	w := b.wxAgentExecutor()
	if ref.TerminalActive {
		return "", fmt.Errorf("会话 #%s 正在终端中运行，不可投递", id)
	}
	if ref.ChatID == "" {
		if ref.SessionID == "" {
			return "", fmt.Errorf("会话 #%s 无可激活目标", id)
		}
		ref, err = w.OpenSession(ref.SessionID)
		if err != nil {
			return "", err
		}
	}
	if err := w.Send(ref.ChatID, text); err != nil {
		return "", err
	}
	label := ref.Tool
	if ref.ToolName != "" {
		label = ref.ToolName
	}
	if ref.Title != "" {
		label += "「" + ref.Title + "」"
	}
	return label, nil
}

// StopAgent 停止指定会话当前轮次（mutating：确认后经此重放）。chatID 非空
// 是挂起时钉住的快照：直接按它执行，规避挂起窗口内编号漂移；空（快照缺失）
// 退回按编号现解析。
func (b *desktopBridge) StopAgent(id, chatID string) error {
	if chatID != "" {
		return b.wxAgentExecutor().Stop(chatID)
	}
	ref, err := b.agentByNum(id)
	if err != nil {
		return err
	}
	if ref.ChatID == "" {
		return fmt.Errorf("会话 #%s 未激活，无需停止", id)
	}
	return b.wxAgentExecutor().Stop(ref.ChatID)
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
	if !ok {
		// 未就绪原因可观测：平台化失配回归（顶层字段被清空）曾因此长期潜伏。
		hubtools.Log("hubtools: 模型三元组未就绪（平台/端点/密钥/ActiveModel），输入直连 ACP")
		return nil
	}
	if a.snapshot().Config.AgentHub.RouteMode == config.RouteModeAgent {
		hubtools.Log("hubtools: RouteMode=agent，工具化关闭，输入直连 ACP")
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
		hubtools.Log("hubtools: 已有在途对话，输入降级原链路")
		return false, nil
	}
	defer h.inflight.Unlock()

	// 聊天记录上下文：取最近 10 条纯文本消息作为记忆渲染进系统提示词
	//（仅语境参考；会话定位改由 list_agents 编号承担）。
	recent := (&desktopBridge{app: h.app}).RecentMessages(10)
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
			hubtools.Log("hubtools: 写操作 %s 挂起待确认（%s）", pc.ToolName, pc.Summary)
			h.mu.Lock()
			h.pending = pc
			h.mu.Unlock()
			return "该写操作已提交用户确认，等待用户决定，请勿重复调用。", nil
		}
		return result, err
	}

	msgs := []hubtools.Message{
		{Role: "system", Content: hubtools.BuildSystemPrompt(reg.Tools(), h.routeAware(), recent)},
		{Role: "user", Content: text},
	}
	ctx, cancel := context.WithTimeout(context.Background(), hubPromptTimeout)
	defer cancel()
	resp, err := hubtools.NewClient(h.cfg).ChatWithExecutor(
		ctx, msgs, reg.Tools(), exec)
	if err != nil {
		// LLM 失败：user 消息尚未落盘（延迟写），store 无痕，直接降级；
		// 同时清除在途会话与挂起，防止从未展示确认卡的操作仍可被 ConfirmHubTool 消费。
		hubtools.Log("hubtools: LLM 失败降级原链路: %v", err)
		h.clearTurn()
		return false, nil
	}

	// 模型完成但无挂起时的降级判定（须在落盘前做，此时 user 消息尚未落盘，
	// store 无痕，原链路接管后不重复）：
	// 1) 空文本 = 路由未产出结果；
	// 2) auto 模式下模型输出 route=agent = 意图判定为非数据操作，原文转投原链路。
	if h.currentPending() == nil {
		if resp.Text == "" {
			hubtools.Log("hubtools: 模型空回复，降级原链路")
			h.clearTurn()
			return false, nil
		}
		if h.routeAware() && hubtools.ParseRouteAgent(resp.Text) {
			hubtools.Log("hubtools: 意图路由 route=agent，降级原链路")
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
		hubtools.Log("hubtools: 确认卡落盘 tool=%s pending=%s", pc.ToolName, pc.PendingID)
		h.app.recordHubMessage(hub.Message{
			Kind: "agent", Event: "tool_confirm", ChatID: chatID,
			Tool: userMsg.Tool, Workspace: userMsg.Workspace, SessionTitle: userMsg.SessionTitle,
			Text: pc.Summary + "\n" + string(tail),
		})
		return true, nil
	}

	hubtools.Log("hubtools: 工具化回复落盘 len=%d", len(resp.Text))
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
	hubtools.Log("hubtools: 确认卡处理 pending=%s approved=%v", pendingID, approved)
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
		{Role: "system", Content: hubtools.BuildSystemPrompt(nil, false, nil)},
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
