package desktop

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/hubtools"
)

// executeRoute 把路由决策落成 Console 动作。校验失败/无法执行时返回
// handled=false，调用方降级直投（投递的永远是用户原文，original 仅供调用方
// 降级使用，本函数不改写它）。
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
// original 为用户原文，仅供调用方降级直投使用；本函数不消费也不改写它。
func executeRoute(a *App, d hubtools.RouteDecision, original string) ([]hub.Reply, bool) {
	c := a.wxHubConsole()
	// sessionNumIndex 构建编号映射（与 #l 同源：ListSessions 顺序即编号
	// 1..N，SessionRef.Num 由展示层赋值，ListSessions 本身不带 Num）。
	// 仅 group/single 分支需要编号映射，status/expand/ask 不做 ListSessions。
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
			text := routeTextFor(d, n)
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
		text := routeTextFor(d, n)
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

// routeTextFor 取某编号目标的投递文本：PerTargetText 命中且非空用改写文本，
// 否则退决策里的通用 Text。空改写视同未命中，避免把空串投出去。
func routeTextFor(d hubtools.RouteDecision, n int) string {
	if t, ok := d.PerTargetText[strconv.Itoa(n)]; ok && strings.TrimSpace(t) != "" {
		return t
	}
	return d.Text
}

// routeBriefs 把命令层会话列表映射为路由器快照：编号 = ListSessions 索引+1
// （与 #l / bridge executeRouteText / executeRoute 的编号语境同源）。Status
// 只做可投递性粗判：终端占用的会话记 gone（不可投递），其余记 idle——路由器
// 只需要可投递目标，无需精确运行态。
func routeBriefs(refs []hub.SessionRef) []hubtools.SessionBrief {
	out := make([]hubtools.SessionBrief, 0, len(refs))
	for i := range refs {
		status := "idle"
		if refs[i].TerminalActive {
			status = "gone"
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
