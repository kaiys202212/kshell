package desktop

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/yangk/kshell/internal/agentapi"
	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/hubtools"
)

// hubRunnerFunc 是管理者循环闭包的形态（与 agentapi.Run 同签名），便于测试
// 注入桩；生产默认使用 AgentAPI().Run。
type hubRunnerFunc func(ctx context.Context, input string, opts agentapi.RunOpts) (agentapi.RunResult, error)

// 智能入口的进度里程碑：稳定 wire key，前端经 i18n 翻译后作为「思考中」气泡
// 的阶段文案。前端在等首 token 期间靠它看到「正在做什么」，而不是一片空白。
const (
	hubStepRoute = "ui.hub.thinking_route" // 输入已交管理者循环处理
	hubStepTool  = "ui.hub.thinking_tool"  // 循环内一次工具调用（payload 带 tool/round）
	hubStepPick  = "ui.hub.thinking_pick"  // 未接管/降级，转为列会话待用户选
)

// hubProgress 是 hub:progress 事件的 payload（前端 HubProgress 同构）。
// Tool/Round 是循环工具调用的扩展字段（旧 step 字段保留，前端向后兼容）。
type hubProgress struct {
	ID    string `json:"id"`
	Step  string `json:"step"`
	Tool  string `json:"tool,omitempty"`
	Round int    `json:"round,omitempty"`
}

// emitHubProgress 推送一轮处理的阶段变化（未指定 reqID 时不推：无主可归）。
func (a *App) emitHubProgress(reqID, step string) {
	if reqID == "" {
		return
	}
	a.Emit("hub:progress", hubProgress{ID: reqID, Step: step})
}

// emitHubToolProgress 推送循环内一次工具调用的进度帧。
func (a *App) emitHubToolProgress(reqID, tool string, round int) {
	if reqID == "" {
		return
	}
	a.Emit("hub:progress", hubProgress{ID: reqID, Step: hubStepTool, Tool: tool, Round: round})
}

// hubCanceller 是一轮在途处理的取消句柄。用指针身份比对，避免同一 reqID
// 被后来的请求覆盖时，旧请求的注销把新句柄删掉（func 值不可比较）。
type hubCanceller struct {
	cancel context.CancelFunc
}

// registerSmartCancel 登记一轮处理的取消句柄，返回注销闭包（defer 用）。
// 同 reqID 重复登记时后者覆盖前者：前端每次发送都生成新 ID，重复即重复提交。
func (a *App) registerSmartCancel(reqID string, cancel context.CancelFunc) func() {
	if reqID == "" {
		return func() {}
	}
	handle := &hubCanceller{cancel: cancel}
	a.mu.Lock()
	if a.smartCancels == nil {
		a.smartCancels = make(map[string]*hubCanceller)
	}
	a.smartCancels[reqID] = handle
	a.mu.Unlock()
	return func() {
		a.mu.Lock()
		// 只删自己登记的那一个：若已被后来的请求覆盖，删掉别人的会让新一轮
		// 失去中断能力，因此必须比对指针身份而不是只看 key 存在。
		if a.smartCancels[reqID] == handle {
			delete(a.smartCancels, reqID)
		}
		a.mu.Unlock()
	}
}

// CancelHubSmartSend 中断一轮进行中的处理：取消该轮 ctx，循环请求随即以
// context.Canceled 失败，HubSmartSend 识别后返回 Cancelled（不回落待选、
// 不清草稿、不重复投递）。reqID 无对应在途轮次时静默忽略（已完成/从未开始）。
func (a *App) CancelHubSmartSend(reqID string) {
	reqID = strings.TrimSpace(reqID)
	if reqID == "" {
		return
	}
	a.mu.Lock()
	handle := a.smartCancels[reqID]
	a.mu.Unlock()
	if handle != nil {
		handle.cancel()
	}
}

// HubSmartSend 是桌面端 Hub 面板「自由文本无精确目标」的智能入口：
// 循环成功（含挂起反问的回执）→ Pending=false，前端回显并清草稿；捷径直投
// 成功同样返回确认回执；逃生门/待选菜单挂起/未配置模型/循环失败 →
// Pending=true，让前端回落既有待选菜单（原文由前端草稿暂存保留，不丢用户正文）。
//
// 处理顺序：待选菜单挂起让位 → 挂起反问语境判定（裸数字守卫，编号是 ask
// 菜单的「回答」）→ 捷径（继续类短确认 + 唯一等待会话，先于逃生门：英文
// continue 本身是逃生门词，必须先判才能直投）→ 逃生门（#/@ 等其余词）→
// 管理者循环 → 降级回落。
//
// 反问采用挂起-续跑语义（T1）：ask 落地由 agentapi 记挂起态（SuspendTTL 内
// 同渠道短输入自动续跑），desktop 只经 HasSuspendedAsk 查询语境，不再维护
// 续轮标记与 TTL。
//
// reqID 是本轮请求标识：用于推送 hub:progress 阶段事件与支持中途取消；
// 空串时行为一致（不推进度、不可取消）。
//
// 在途守卫：同一 App 内循环调用全局串行（smartBusy 置位，锁外做 LLM 调用），
// 前端重复提交/并发调用时后到的直接返回 Pending=true，不重复投递。
func (a *App) HubSmartSend(reqID, text string) (HubPickView, error) {
	if !a.snapshot().ready() {
		return HubPickView{}, errNotReady
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return HubPickView{Pending: true}, nil
	}
	// 在途守卫置位/清理：忙时后到的调用直接回落待选（Pending=true）。
	a.mu.Lock()
	if a.smartBusy {
		a.mu.Unlock()
		return HubPickView{Pending: true}, nil
	}
	a.smartBusy = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.smartBusy = false
		a.mu.Unlock()
	}()
	// 待选菜单挂起时让位：命令层未决交互期间所有文本不接管，交前端回落既有
	// 确定性路径（捷径也不例外——直投会让 pending pick 草稿失效）。
	if a.wxHubConsole().Busy() {
		return HubPickView{Pending: true}, nil
	}
	// 挂起反问语境（探针查询 agentapi 挂起态）：裸数字/「继续」类短确认属于
	// 反问语境的「回答」，不进捷径、不让位命令层，由 Run 自动续跑消费。
	suspend := a.hubAskSuspended()

	// 裸数字守卫：数字只在反问续轮语境接管（编号是 ask 菜单的「回答」）。
	// 无语境数字是命令层语义（#l 编号切换/待选菜单/no_mapping），让位回前端
	// 命令路径——既有数字曾被前端无条件劫持进命令层、续轮收不到答案；反向
	// 修正后也不能把裸数字当新需求喂给 LLM。
	if hub.IsBareNumber(text) && !suspend {
		return HubPickView{Pending: true}, nil
	}

	// 捷径（先于逃生门）：非反问语境的「继续类短确认」+ 恰好一个等待会话 →
	// 直接投递原文，不经 LLM。必须排在 IsDirectPass 之前——「continue」本身
	// 是默认逃生门词，否则英文确认词永远到不了捷径；#/@ 等其它词不受影响
	//（词表无 # 前缀，# 命令不会误触捷径）。零/多个等待会话不进捷径，
	// continue 语境词仍按逃生门回落待选（正确降级）。
	if !suspend && hub.IsContinueConfirm(text) {
		if view, handled := a.shortcutSend(text); handled {
			return view, nil
		}
	}
	// 逃生门（#/@/continue 及用户自定义词）不接管，交前端走命令路径。
	if hub.IsDirectPass(text, a.agenthubDirectPatterns()) {
		return HubPickView{Pending: true}, nil
	}
	// 用户原话落盘（问题 1）：进入管理者循环前把原文写进 hub 历史——循环内
	// send 工具投递的是模型改写文本，落盘的是那份；只查状态/反问时此前完全不落。
	// 原话无投递目标（ChatID 空 → hub 自身归因）。捷径分支上面已让位：
	// 捷径直投的原文由发送链 sendAndRecord 落盘，这里不再重复。
	a.recordHubMessageWithID(hub.Message{Kind: "user", Text: text}, "")
	// 可取消 ctx：用户点「中断」时整轮（含模型往返）立即结束。
	ctx, cancel := context.WithCancel(a.desktopContext())
	defer cancel()
	unregister := a.registerSmartCancel(reqID, cancel)
	defer unregister()

	// 循环步骤累计（问题 3）：思考时间线随 hub:progress 推给前端的同时留底，
	// 循环结束后作为 hub_manager_think 消息结构化落盘，重启后仍可读。
	var mgrSteps []hub.Step
	a.emitHubProgress(reqID, hubStepRoute)
	// 发送前是否有挂起反问：有且本轮正常返回（无论结论还是再次反问），
	// 说明用户已用自由文本回答了旧卡——先关旧卡再落新卡，防止误标。
	suspendWas := a.hubAskSuspended()
	res, err := a.runHubLoop(ctx, text, agentapi.RunOpts{
		Channel: "hub",
		Progress: func(p agentapi.LoopProgress) {
			mgrSteps = append(mgrSteps, hub.Step{Tool: p.Tool, Round: p.Round})
			a.emitHubToolProgress(reqID, p.Tool, p.Round)
		},
	})
	if err != nil {
		// 挂起态归 agentapi 所有：瞬时错误（如 ErrLLMNotReady）保留挂起，
		// 用户重试会自动续跑；desktop 无需也不得清理。
		// 取消在途中触发：返回 Cancelled（不回落待选、不重复投递）。
		if ctx.Err() != nil {
			return HubPickView{Cancelled: true}, nil
		}
		a.emitHubProgress(reqID, hubStepPick)
		return HubPickView{Pending: true}, nil
	}
	if suspendWas {
		a.markLastAskAnswered()
	}
	// 思考与结论落盘（问题 3）：此前回执只走前端瞬态气泡，重拉/重启即丢。
	// 挂起反问落 hub_manager_ask 卡片，结论逐条落 hub_manager_reply。
	return a.finishManagerLoop(res, mgrSteps), nil
}

// managerToolNames 是管理者循环工具名 → 中文显示名映射（思考链摘要用）；
// 未收录的工具保留原名。
var managerToolNames = map[string]string{
	"list_sessions":    "查会话列表",
	"send":             "投递",
	"create_session":   "建会话",
	"open_session":     "开会话",
	"stop":             "停止",
	"set_model":        "切模型",
	"session_messages": "读消息",
	"search_memory":    "查记忆",
	"status":           "查状态",
	"ask":              "反问",
}

// summarizeManagerSteps 把思考链步骤聚合成一行中文摘要：按顺序遍历，相邻同名
// 步骤合并为「名称×N」（挂起-续跑跨轮后同一工具可能多次出现，合并相邻即可
// 还原「先做什么、再做什么」的主线）；工具名经映射表转中文，未知工具显示原名。
// 调用方保证 steps 非空（finishManagerLoop 有 len>0 门禁），空输入返回空摘要。
func summarizeManagerSteps(steps []hub.Step) string {
	if len(steps) == 0 {
		return ""
	}
	groups := make([]string, 0, len(steps))
	for i := 0; i < len(steps); {
		j := i
		for j+1 < len(steps) && steps[j+1].Tool == steps[i].Tool {
			j++
		}
		name := steps[i].Tool
		if zh, ok := managerToolNames[name]; ok {
			name = zh
		}
		groups = append(groups, fmt.Sprintf("%s×%d", name, j-i+1))
		i = j + 1
	}
	return fmt.Sprintf("思考 %d 步：%s", len(steps), strings.Join(groups, " · "))
}

// shortcutSend 处理「继续类短确认」的确定性捷径：SessionStatuses 里恰好一个
// 等待会话时，把用户原文经 wxExecutor 的 Send 既有链路直接投递给该会话，
// 返回单条确认回执（含会话名）。等待会话收敛为 perm 状态，不做问号启发
//（问号启发在 agentapi questionWaiting，仅服务循环内的模型语境）。零个/多个等待
// 会话返回 handled=false 交管理者循环；Send 失败返回 Pending=true 让前端回落
// 待选（草稿保留，不做部分投递、不重复进循环二次投递）。
func (a *App) shortcutSend(text string) (HubPickView, bool) {
	ex := newWXExecutor(a)
	var waiting hub.SessionRef
	n := 0
	for _, s := range ex.SessionStatuses() {
		if s.State == "perm" {
			n++
			waiting = s.Ref
		}
	}
	if n != 1 {
		return HubPickView{}, false
	}
	if err := ex.Send(waiting.ChatID, text); err != nil {
		log.Printf("hub 捷径直投失败（chat=%s）: %v", waiting.ChatID, err)
		return HubPickView{Pending: true}, true
	}
	lang := normalizeLanguage(a.snapshot().Config.Language)
	reply := fmt.Sprintf(hub.CmdText(lang, "shortcut_sent"), text, waiting.Title)
	// 捷径回执同样落盘（hub_manager_reply）：重启后历史仍可读。
	a.recordHubMessage(hub.Message{Kind: "agent", Event: "hub_manager_reply", Text: reply})
	return HubPickView{Replies: []hub.Reply{{Text: reply}}, Pending: false}, true
}

// runHubLoop 取管理者循环闭包并执行一次：注入桩优先，nil 时用 AgentAPI().Run
//（LLM 配置每次访问实时刷新，与微信桥路由同口径）。
func (a *App) runHubLoop(ctx context.Context, input string, opts agentapi.RunOpts) (agentapi.RunResult, error) {
	a.mu.Lock()
	fn := a.hubRunner
	a.mu.Unlock()
	if fn != nil {
		return fn(ctx, input, opts)
	}
	return a.AgentAPI().Run(ctx, input, opts)
}

// desktopContext 取 Wails 启动上下文（未启动/测试为 Background）。
func (a *App) desktopContext() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// routeBriefs 把命令层会话列表映射为路由器快照：编号 = ListSessions 索引+1
// （与 #l / bridge executeRouteText 的编号语境同源），不重排不增删。Status 取
// SessionStatuses 的真实状态（running/perm/idle/gone）：按活跃 chat 的 ChatID
// （退 SessionID/Key）对齐；列表里但不在 statuses 的会话（磁盘会话无活跃
// chat）保留可投递性粗判兜底：终端占用 → gone（不可投递），其余 → idle。
// 供 weixin.go 的 RouteInput 降级路径使用（desktop 主路径已换管理者循环）。
func routeBriefs(refs []hub.SessionRef, statuses []hub.SessionStatus) []hubtools.SessionBrief {
	byRef := make(map[string]hub.SessionStatus, len(statuses))
	for _, s := range statuses {
		for _, k := range []string{s.Ref.ChatID, s.Ref.SessionID, s.Ref.Key} {
			if k != "" {
				byRef[k] = s
			}
		}
	}
	out := make([]hubtools.SessionBrief, 0, len(refs))
	for i := range refs {
		status := "idle"
		if refs[i].TerminalActive {
			status = "gone"
		}
		for _, k := range []string{refs[i].ChatID, refs[i].SessionID, refs[i].Key} {
			if s, ok := byRef[k]; ok {
				status = s.State
				break
			}
		}
		tool := refs[i].Tool
		if tool == "" {
			tool = refs[i].ToolName
		}
		out = append(out, hubtools.SessionBrief{
			Num:    i + 1,
			Tool:   tool,
			Title:  refs[i].Title,
			Status: status,
		})
	}
	return out
}
