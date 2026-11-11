package desktop

import (
	"context"
	"fmt"
	"strings"

	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/hubtools"
)

// smartRouterFunc 是智能路由闭包的形态（与 weixin.InputRouter 同签名），
// 便于测试注入桩；生产默认使用 weixinInputRouter。
type smartRouterFunc func(ctx context.Context, input string) (hubtools.RouteDecision, bool)

// HubSmartSend 是桌面端 Hub 面板「自由文本无精确目标」的智能路由入口：
// 路由成功→执行决策返回回执（Pending=false，前端回显并清草稿）；逃生门/
// 待选菜单挂起/反问超限/未配置/路由不消费/决策不可执行→Pending=true，让
// 前端回落既有待选菜单（原文由前端草稿暂存保留，不丢用户正文）。
//
// 轮次语义与微信桥 dispatchConsole 对齐：ask 决策累加轮次并返回单条反问；
// 非 ask 决策/降级路径清零，保证反问链被打断后不悬挂。
//
// 在途守卫：同一 App 内路由调用全局串行（smartBusy 置位，锁外做 router 调用），
// 前端重复提交/并发调用时后到的直接返回 Pending=true，不清轮次、不重复投递。
func (a *App) HubSmartSend(text string) (HubPickView, error) {
	if !a.snapshot().ready() {
		return HubPickView{}, errNotReady
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return HubPickView{Pending: true}, nil
	}
	// 在途守卫置位/清理：忙时后到的调用直接回落待选（Pending=true），轮次不动。
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
	// 逃生门（#/@/continue/explain 及用户自定义词）与命令层菜单挂起时不接管，
	// 交前端回落既有确定性路径；两种情形都视为打断反问链，轮次清零。
	if hub.IsDirectPass(text, a.agenthubDirectPatterns()) || a.wxHubConsole().Busy() {
		a.setSmartAskRound(0)
		return HubPickView{Pending: true}, nil
	}
	// 反问链已满：本轮不再路由，轮次清零并回落待选（与微信桥一致）。
	a.mu.Lock()
	round := a.smartAskRound
	if round >= hubtools.AskMaxRounds {
		a.smartAskRound = 0
	}
	a.mu.Unlock()
	if round >= hubtools.AskMaxRounds {
		return HubPickView{Pending: true}, nil
	}
	d, handled := a.smartRoute(a.desktopContext(), text)
	if !handled {
		a.setSmartAskRound(0)
		return HubPickView{Pending: true}, nil
	}
	replies, ok := executeRoute(a, d)
	if !ok || len(replies) == 0 {
		a.setSmartAskRound(0)
		return HubPickView{Pending: true}, nil
	}
	if d.Action == "ask" {
		a.incSmartAskRound()
	} else {
		a.setSmartAskRound(0)
	}
	return HubPickView{Replies: replies, Pending: false}, nil
}

// smartRoute 取路由闭包并执行一次决策：注入桩优先，nil 时惰性用
// weixinInputRouter（实时读配置，与微信桥同一快照来源）。故意不缓存闭包——
// 无状态、实时读配置，缓存会把配置变更/换模型挡在旧快照之外。
func (a *App) smartRoute(ctx context.Context, input string) (hubtools.RouteDecision, bool) {
	a.mu.Lock()
	fn := a.smartRouter
	a.mu.Unlock()
	if fn == nil {
		fn = smartRouterFunc(a.weixinInputRouter())
	}
	return fn(ctx, input)
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

// setSmartAskRound 写反问轮次（mu 保护）。
func (a *App) setSmartAskRound(n int) {
	a.mu.Lock()
	a.smartAskRound = n
	a.mu.Unlock()
}

// incSmartAskRound 反问轮次自增（mu 保护）。
func (a *App) incSmartAskRound() {
	a.mu.Lock()
	a.smartAskRound++
	a.mu.Unlock()
}

// executeRoute 把路由决策落成 Console 动作。校验失败/无法执行时返回
// handled=false，调用方降级直投（调用方自行持用户原文决定降级投递内容）。
//
//   - group: snapshot 编号→SessionRef（ListSessions 全集映射，与 #l 同源）
//     → c.SendGroupTexts(refs, texts)；texts 优先 PerTargetText[num] 退 Text。
//   - single 非空 targets: 恰 1 个合法编号 → 由编号映射 ref 后直接投递
//     （等价单发路径，不走 Console.Handle 的编号语义）。
//   - single 空 targets: 返回 handled=false（调用方回落 current 直投）。
//   - status: 合成 "#st" 命令经 c.Handle 执行；#st 自行按状态列表渲染，
//     不读 lastList 编号语境，也不会触发 no_mapping。
//   - expand: 合成 "#gx N [count]" 经 c.Handle（#gx 依赖 lastList 编号语境，
//     lastList 过期回 no_mapping 属正确降级，不特殊处理）。
//   - ask: 单条回执 = hub.CmdText(lang, "route_ask_prefix") + Question。
//
func executeRoute(a *App, d hubtools.RouteDecision) ([]hub.Reply, bool) {
	c := a.wxHubConsole()
	// sessionNumIndex 构建编号映射（与 #l 同源：ListSessions 顺序即编号
	// 1..N，SessionRef.Num 由展示层赋值，ListSessions 本身不带 Num）。
	// 仅 group/single 分支需要编号映射，status/expand/ask 不做 ListSessions。
	// 已知暴露面（编号 TOCTOU）：路由快照与执行映射是两次 ListSessions，窗口
	// 内会话增删会使编号漂移；后续可改为决策随快照返回 refs（同 bridge 侧）。
	sessionNumIndex := func() map[int]hub.SessionRef {
		sessions := newWXExecutor(a).ListSessions()
		byNum := make(map[int]hub.SessionRef, len(sessions))
		for i := range sessions {
			ref := sessions[i]
			ref.Num = i + 1
			byNum[ref.Num] = ref
		}
		return byNum
	}
	switch d.Action {
	case "group":
		byNum := sessionNumIndex()
		// 包内已校验 ≥2，此处仍防御：去重后不足 2 个目标无法执行群发语义。
		refs := make([]hub.SessionRef, 0, len(d.Targets))
		texts := make([]string, 0, len(d.Targets))
		seen := make(map[int]bool, len(d.Targets))
		for _, n := range d.Targets {
			if seen[n] {
				continue
			}
			seen[n] = true
			ref, ok := byNum[n]
			if !ok {
				return nil, false
			}
			text := d.TextFor(n)
			// 任一目标文本为空则整批降级（发送前判定，不做部分投递），
			// 避免向某个目标投出空串且回执计成功。
			if strings.TrimSpace(text) == "" {
				return nil, false
			}
			refs = append(refs, ref)
			texts = append(texts, text)
		}
		if len(refs) < 2 {
			return nil, false
		}
		return c.SendGroupTexts(refs, texts), true

	case "single":
		if len(d.Targets) != 1 {
			// 空 targets = 投当前会话，交由调用方回落 current 直投；>1 非法。
			return nil, false
		}
		byNum := sessionNumIndex()
		n := d.Targets[0]
		ref, ok := byNum[n]
		if !ok {
			return nil, false
		}
		text := d.TextFor(n)
		if strings.TrimSpace(text) == "" {
			return nil, false
		}
		// 走单发语义（与 #g 单目标契约同源：成功回 sent_to 并退出聚合）。
		return c.SendGroupTexts([]hub.SessionRef{ref}, []string{text}), true

	case "status":
		return c.Handle("#st"), true

	case "expand":
		if d.ExpandNum <= 0 {
			return nil, false
		}
		if d.ExpandCount > 0 {
			return c.Handle(fmt.Sprintf("#gx %d %d", d.ExpandNum, d.ExpandCount)), true
		}
		return c.Handle(fmt.Sprintf("#gx %d", d.ExpandNum)), true

	case "ask":
		q := strings.TrimSpace(d.Question)
		if q == "" {
			return nil, false
		}
		lang := normalizeLanguage(a.snapshot().Config.Language)
		return []hub.Reply{{Text: hub.CmdText(lang, "route_ask_prefix") + q}}, true
	}
	return nil, false
}

// routeBriefs 把命令层会话列表映射为路由器快照：编号 = ListSessions 索引+1
// （与 #l / bridge executeRouteText / executeRoute 的编号语境同源），不重排
// 不增删。Status 取 SessionStatuses 的真实状态（running/perm/idle/gone）：
// 按活跃 chat 的 ChatID（退 SessionID/Key）对齐；列表里但不在 statuses 的
// 会话（磁盘会话无活跃 chat）保留可投递性粗判兜底：终端占用 → gone（不可
// 投递），其余 → idle。
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
