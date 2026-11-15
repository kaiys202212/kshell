package desktop

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

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

// hubContinuationMax 是 desktop 续轮衔接判定的输入长度上限（rune），与
// agentapi 的 shortInputMax 对齐：AskPending 语境下只有短输入才显式传
// Continuation=true，长输入不传标志、由 agentapi 内部清 pending 开新轮。
const hubContinuationMax = 20

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
// 循环成功（含 AskPending 的选项回执）→ Pending=false，前端回显并清草稿；
// 捷径直投成功同样返回确认回执；逃生门/待选菜单挂起/未配置模型/循环失败 →
// Pending=true，让前端回落既有待选菜单（原文由前端草稿暂存保留，不丢用户正文）。
//
// 处理顺序：待选菜单挂起让位 → 捷径（继续类短确认 + 唯一等待会话，先于
// 逃生门：英文 continue 本身是逃生门词，必须先判才能直投）→ 逃生门（#/@ 等
// 其余词）→ 管理者循环 → 降级回落。
//
// reqID 是本轮请求标识：用于推送 hub:progress 阶段事件与支持中途取消；
// 空串时行为一致（不推进度、不可取消）。
//
// 反问轮次语义由 agentapi 的 pendingAsk 接管（ask 工具落地记语境、续轮消费、
// AskMaxRounds 门禁在循环内判），desktop 不再维护轮次计数。
//
// 在途守卫：同一 App 内循环调用全局串行（smartBusy 置位，锁外做 LLM 调用），
// 前端重复提交/并发调用时后到的直接返回 Pending=true，不清续轮标记、不重复投递。
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
	// 续轮衔接：上次 Run 返回 AskPending 且本条是短输入 → 显式传 Continuation，
	// 让循环带上上一轮反问语境（选项/候选编号）。置位时刻超过 PendingAskTTL
	// 视为语境失效：不传 Continuation 并清零标记——显式标志会绕过 agentapi Run
	// 内部的 TTL 检查（loop.go 对显式 Continuation 直接采用），过期判定必须在
	// desktop 侧完成，避免隔了几小时的「继续」把陈年反问语境注入模型。
	// 长输入不传标志，agentapi 内部清 pending 按新需求处理。
	a.mu.Lock()
	askPending := a.hubAskPending
	askedAt := a.hubAskPendingAt
	a.mu.Unlock()
	cont := false
	if askPending {
		if time.Since(askedAt) <= agentapi.PendingAskTTL {
			cont = hub.IsContinueConfirm(text) || len([]rune(text)) <= hubContinuationMax
		} else {
			a.mu.Lock()
			a.hubAskPending = false
			a.mu.Unlock()
		}
	}

	// 捷径（先于逃生门）：非续轮语境的「继续类短确认」+ 恰好一个等待会话 →
	// 直接投递原文，不经 LLM。必须排在 IsDirectPass 之前——「continue」本身
	// 是默认逃生门词，否则英文确认词永远到不了捷径；#/@ 等其它词不受影响
	//（词表无 # 前缀，# 命令不会误触捷径）。零/多个等待会话不进捷径，
	// continue 语境词仍按逃生门回落待选（正确降级）。
	if !cont && hub.IsContinueConfirm(text) {
		if view, handled := a.shortcutSend(text); handled {
			return view, nil
		}
	}
	// 逃生门（#/@/continue 及用户自定义词）不接管，交前端走命令路径。
	if hub.IsDirectPass(text, a.agenthubDirectPatterns()) {
		return HubPickView{Pending: true}, nil
	}
	// 可取消 ctx：用户点「中断」时整轮（含模型往返）立即结束。
	ctx, cancel := context.WithCancel(a.desktopContext())
	defer cancel()
	unregister := a.registerSmartCancel(reqID, cancel)
	defer unregister()

	a.emitHubProgress(reqID, hubStepRoute)
	res, err := a.runHubLoop(ctx, text, agentapi.RunOpts{
		Channel:      "hub",
		Continuation: cont,
		Progress: func(p agentapi.LoopProgress) {
			a.emitHubToolProgress(reqID, p.Tool, p.Round)
		},
	})
	if err != nil {
		// 续轮标记维护：ErrLLMNotReady 时 agentapi 保留 pending（用户配好模型
		// 后原语境仍在），desktop 同步保留；其余错误（含取消）语境已断，清零。
		if !errors.Is(err, agentapi.ErrLLMNotReady) {
			a.mu.Lock()
			a.hubAskPending = false
			a.mu.Unlock()
		}
		// 取消在途中触发：返回 Cancelled（不回落待选、不重复投递）。
		if ctx.Err() != nil {
			return HubPickView{Cancelled: true}, nil
		}
		a.emitHubProgress(reqID, hubStepPick)
		return HubPickView{Pending: true}, nil
	}
	// 记录 AskPending 结果供下一条输入续轮（选项回执本身 Pending=false），
	// 置位时刻一并记录供 TTL 判定。
	a.mu.Lock()
	a.hubAskPending = res.AskPending
	if res.AskPending {
		a.hubAskPendingAt = time.Now()
	}
	a.mu.Unlock()
	replies := make([]hub.Reply, 0, len(res.Replies))
	for _, r := range res.Replies {
		replies = append(replies, hub.Reply{Text: r})
	}
	return HubPickView{Replies: replies, Pending: false}, nil
}

// shortcutSend 处理「继续类短确认」的确定性捷径：SessionStatuses 里恰好一个
// 等待会话时，把用户原文经 wxExecutor 的 Send 既有链路直接投递给该会话，
// 返回单条确认回执（含会话名）。等待会话收敛为 perm 状态，不做问号启发
//（问号启发在 agentapi waitingUser，仅服务循环内的模型语境）。零个/多个等待
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
