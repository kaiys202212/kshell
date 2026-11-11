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
//   - status: 合成 "#st" 命令经 c.Handle 执行（编号语境由 lastList 提供时才
//     有效；lastList 过期回 no_mapping 属正确降级，不特殊处理）。
//   - expand: 合成 "#gx N [count]" 经 c.Handle。
//   - ask: 单条回执 = hub.CmdText(lang, "route_ask_prefix") + Question。
func executeRoute(a *App, d hubtools.RouteDecision, original string) ([]hub.Reply, bool) {
	_ = original // 降级路径由调用方投递用户原文；本函数不消费。

	// 编号映射与 #l 同源：ListSessions 顺序即编号 1..N（SessionRef.Num 由
	// 展示层赋值，ListSessions 本身不带 Num）。
	sessions := newWXExecutor(a).ListSessions()
	byNum := make(map[int]hub.SessionRef, len(sessions))
	for i := range sessions {
		ref := sessions[i]
		ref.Num = i + 1
		byNum[ref.Num] = ref
	}

	c := a.wxHubConsole()
	switch d.Action {
	case "group":
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
			refs = append(refs, ref)
			texts = append(texts, routeTextFor(d, n))
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
