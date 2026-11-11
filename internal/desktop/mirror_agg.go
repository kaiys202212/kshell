package desktop

import (
	"fmt"
	"sort"
	"strings"

	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/weixin"
)

// 微信出站(agent 镜像)聚合:群发目标集活跃时,单目标 agent 回复不再逐条镜像,
// 改投一条聚合摘要行(agg_line + 逐目标状态明细)。共享 MirrorPolicy 实例的
// 装配见 weixin.go 的 startWeixin / wxHubConsole。

// mirrorHubPolicy 取 App 级共享聚合策略(惰性创建, mu 内完成, 供 startWeixin
// 等未持锁场景)。已持 a.mu 的调用点(wxHubConsole)须直接判 nil 初始化字段,
// 不能调这里(重入死锁)。
func (a *App) mirrorHubPolicy() *hub.MirrorPolicy {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mirrorPolicy == nil {
		a.mirrorPolicy = hub.NewMirrorPolicy()
	}
	return a.mirrorPolicy
}

// mirrorAggregated 判断并产出聚合摘要行:策略活跃且 m 命中目标集时,把本条
// agent 回复折叠进摘要并出站(mirror 不转发原文)。返回 true 表示已消费
// (调用方跳过逐条镜像);store 落盘不受影响(调用方在写入之后才走到这里)。
// 明细覆盖全部目标:命中的用 SessionStatuses 的实况行,未命中的合成 gone 伪行
//(Title 用登记值),一把编号 1..N(渲染时语境,同 #st 做法)。
func (a *App) mirrorAggregated(r *weixin.Runtime, m hub.Message) bool {
	// 锁内只取指针快照(调用方 recordHubMessage 不持 a.mu, 写入方均持锁),
	// 后续 p.Active()/Match 等用 MirrorPolicy 自身锁, 不在此长时间持 a.mu。
	a.mu.Lock()
	p := a.mirrorPolicy
	a.mu.Unlock()
	if p == nil || !p.Active() {
		return false
	}
	if !p.Match(m.ChatID, m.TermKey, m.SessionID) {
		return false
	}
	// SessionStatuses 按活跃 chat 逐行;LastTs 是归因双键命中的最新水位。
	rows := newWXExecutor(a).SessionStatuses()
	byChat := make(map[string]hub.SessionStatus, len(rows))
	for _, row := range rows {
		if row.Ref.ChatID != "" {
			byChat[row.Ref.ChatID] = row
		}
	}
	since := p.SinceTs()
	targets := p.Targets()
	// 稳定顺序:标题(空在后)→ ChatID → SessionID。Targets 来自 map,无序。
	sort.SliceStable(targets, func(i, j int) bool {
		ti, tj := targets[i].Title != "", targets[j].Title != ""
		if ti != tj {
			return ti
		}
		if targets[i].Title != targets[j].Title {
			return targets[i].Title < targets[j].Title
		}
		if targets[i].ChatID != targets[j].ChatID {
			return targets[i].ChatID < targets[j].ChatID
		}
		return targets[i].SessionID < targets[j].SessionID
	})
	replied := 0
	detail := make([]hub.SessionStatus, 0, len(targets))
	for i, t := range targets {
		row, ok := byChat[t.ChatID] // 群发目标必为 ACP 可投递会话,按链头 ChatID 命中
		if !ok {
			// 兜底(chat 重建后链头 ChatID 变了):按触发消息的 termKey /
			// sessionID 匹配实况行,任一命中即触发会话自身,避免合成 gone。
			for _, cand := range rows {
				if cand.Ref.Key == m.TermKey || (m.SessionID != "" && cand.Ref.SessionID == m.SessionID) {
					row, ok = cand, true
					break
				}
			}
		}
		if !ok {
			// 未命中(会话已关/未产出状态):合成 gone 伪行。
			row = hub.SessionStatus{
				Ref:   hub.SessionRef{Num: i + 1, Title: t.Title, SessionID: t.SessionID},
				State: "gone",
			}
		} else {
			row.Ref.Num = i + 1 // 写回本摘要的编号语境
			if row.LastTs >= since {
				replied++
			}
		}
		detail = append(detail, row)
	}
	if replied == 0 {
		// 有目标消息但还没统计到回复(如首条正卡在 since 边界):吞掉本条,
		// 不发一行空骨架(「0/N 会话有回复」无信息量)。
		return true
	}
	// 自然终结汇报:全部目标到达终态(无 running 且无 perm——perm 是「卡住
	// 待人」的暂停态而非完成态,全 perm 出「全部完成」会误导)且至少一条已
	// 回复 → 出汇总报告替代摘要行,并结束本轮聚合周期(报告即终点)。明细
	// 复用 aggDetailRows,编号语境与摘要行一致(report_hint 的 #gx 编号即明细序号)。
	allTerminal := true
	for _, row := range detail {
		if row.State == "running" || row.State == "perm" {
			allTerminal = false
			break
		}
	}
	if replied > 0 && allTerminal {
		lang := r.Lang()
		line := fmt.Sprintf(hub.CmdText(lang, "report_header"), strings.Join(aggDetailRows(lang, detail), ", "))
		line += "\n\n" + hub.CmdText(lang, "report_hint")
		r.OnHubMessage(hub.Message{Kind: "agent", Ts: m.Ts, Text: line})
		p.Discard()
		return true
	}
	r.OnHubMessage(hub.Message{Kind: "agent", Ts: m.Ts, Text: composeAggLine(r.Lang(), detail, replied, len(targets))})
	return true
}

// composeAggLine 组装聚合摘要行: agg_line(%1=replied %2=total %3=明细)。
// 词源 hub.CmdText(lang,key) 与 Console 文案同源;明细由 aggDetailRows 提供。
func composeAggLine(lang string, rows []hub.SessionStatus, replied, total int) string {
	return fmt.Sprintf(hub.CmdText(lang, "agg_line"), replied, total, strings.Join(aggDetailRows(lang, rows), "，"))
}

// aggDetailRows 组装逐目标明细行(摘要行与全终态报告共用):一行一目标
// `N【标题】状态词`; idle 在聚合语义=已完成(word_done), gone=已断开,
// perm 额外带摘要(truncateFirstLine 截到 40)。
func aggDetailRows(lang string, rows []hub.SessionStatus) []string {
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		word := hub.CmdText(lang, "stat_word_"+r.State)
		if strings.HasPrefix(word, "stat_word_") {
			// 未命中文案表时会原样返回 key(cmdText 语义):退回状态本身。
			word = "?" + r.State
		}
		if r.State == "idle" {
			// 聚合行语义:目标已在汇报轮里「说完话」,即已完成。
			word = hub.CmdText(lang, "word_done")
		}
		line := sessionTagOf(r.Ref)
		if line != "" && r.Ref.Title == "" {
			// 无标题标签(裸编号/工具名):状态词空一格,与 #st 列行同形。
			line += " "
		}
		line += word
		if r.State == "perm" && r.Summary != "" {
			line += " — " + truncateFirstLine(r.Summary, 40)
		}
		parts = append(parts, line)
	}
	return parts
}

// sessionTagOf desktop 镜像 hub 的编号标签形态(gTag 族):有 Title →
// "N【Title】"(Num<=0 时省略编号,语境由调用方在渲染时编号),无 Title
// 有编号 → "N",再无 → Tool,全空返回空串。单元格不落空,状态词逐行可归属。
func sessionTagOf(r hub.SessionRef) string {
	if r.Title != "" {
		if r.Num > 0 {
			return fmt.Sprintf("%d【%s】", r.Num, r.Title)
		}
		return "【" + r.Title + "】"
	}
	if r.Num > 0 {
		return fmt.Sprintf("%d", r.Num)
	}
	return r.Tool
}
