package desktop

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/weixin"
)

// 微信出站(agent 镜像)聚合:群发目标集活跃时,单目标 agent 回复不再逐条镜像,
// 改投一条聚合摘要行(agg_line + 逐目标状态明细)。共享 MirrorPolicy 实例的
// 装配见 weixin.go 的 startWeixin / wxHubConsole。

// aggHeartbeats 跟踪聚合周期内各目标的最近一次心跳/活动基线(纳秒,与 hub
// 消息 Ts 同单位)。mirrorAggregated 在 recordHubMessage 路径不持 App 锁,
// 故自持 sync.Mutex,不经 a.mu。
//
// 语义:目标首次以 running 出现时记录 now 为基线且本轮不判定(避免 now-0
// 巨大导致 Enter 即超时);目标有新回复(LastTs 超过基线)时基线前移到
// LastTs 重置窗口;距基线达到阈值则本轮出一次心跳并把基线推进到 now。
type aggHeartbeats struct {
	mu    sync.Mutex
	cycle int64            // 当前周期的策略时间基准(SinceTs);变化即清表
	marks map[string]int64 // key: SessionID 退 ChatID → 最近基线 Ts(纳秒)
}

// begin 绑定当前聚合周期的基准。周期切换(含 Enter 重设 SinceTs)时清空
// 跟踪表,避免上一轮基线或已不在目标集的目标残留。
func (h *aggHeartbeats) begin(cycle int64) {
	h.mu.Lock()
	if h.cycle != cycle {
		h.cycle = cycle
		h.marks = nil
	}
	h.mu.Unlock()
}

// tick 判定某目标本轮是否出心跳并推进基线。now/lastTs/threshold 均为纳秒。
// 返回 (是否触发, 距基线时长)。
func (h *aggHeartbeats) tick(key string, now, lastTs, threshold int64) (bool, int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.marks == nil {
		h.marks = map[string]int64{}
	}
	base, ok := h.marks[key]
	if !ok {
		// 首次见到该目标 running:从此刻起计时,本轮不判定。
		h.marks[key] = now
		return false, 0
	}
	if lastTs > base {
		// 有新回复(活动水位前进):窗口重置,不出心跳。
		h.marks[key] = lastTs
		return false, 0
	}
	if now-base >= threshold {
		elapsed := now - base
		h.marks[key] = now
		return true, elapsed
	}
	return false, 0
}

// reset 清空跟踪表(聚合周期结束,如报告 Discard)。
func (h *aggHeartbeats) reset() {
	h.mu.Lock()
	h.cycle = 0
	h.marks = nil
	h.mu.Unlock()
}

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
// (Title 用登记值),一把编号 1..N(渲染时语境,同 #st 做法)。
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
	a.aggHb.begin(since) // 周期切换时清空心跳跟踪表
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
	// 待人」的暂停态而非完成态,全 perm 出「全部完成」会误导)→ 出汇总报告
	// 替代摘要行,并结束本轮聚合周期(报告即终点)。此处已过 replied==0 守门
	// (上方吞行分支保证至少一条已回复),无需再判。明细复用 aggDetailLines,
	// 编号语境与摘要行一致(report_hint 的 #gx 编号即明细序号)。
	allTerminal := true
	for _, row := range detail {
		if row.State == "running" || row.State == "perm" {
			allTerminal = false
			break
		}
	}
	lang := r.Lang()
	if allTerminal {
		line := fmt.Sprintf(hub.CmdText(lang, "report_header"), strings.Join(aggDetailLines(lang, detail), ", "))
		line += "\n\n" + hub.CmdText(lang, "report_hint")
		r.OnHubMessage(hub.Message{Kind: "agent", Ts: m.Ts, Text: line})
		p.Discard()
		a.aggHb.reset() // 报告即终点:心跳跟踪表随之清空
		return true
	}
	// 摘要行出站处附加心跳:与摘要共用同一条消息(空行分隔),不另发一条,
	// 避免微信侧刷两条气泡。报告分支在上方已返回,不会走到这里(报告时无
	// running 目标,心跳天然无从触发)。
	text := composeAggLine(lang, detail, replied, len(targets))
	if hb := a.aggHeartbeatLines(lang, detail); hb != "" {
		text += "\n\n" + hb
	}
	r.OnHubMessage(hub.Message{Kind: "agent", Ts: m.Ts, Text: text})
	return true
}

// aggHeartbeatLines 产出本次摘要的心跳附加行:逐 running 目标按
// HeartbeatMinutes 阈值判定,触发的以 hb_line(%s=标签 %s=时长)成行,多条以
// 换行接龙。标签复用 sessionTagOf(与明细行同形),时长取纯分钟 "Nm"。
// 阈值实时读配置快照(缺省/钳制由 config 归一保证)。
func (a *App) aggHeartbeatLines(lang string, rows []hub.SessionStatus) string {
	minutes := a.snapshot().Config.AgentHub.Normalized().HeartbeatMinutes
	threshold := int64(minutes) * int64(time.Minute)
	now := time.Now().UnixNano()
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.State != "running" {
			continue
		}
		key := row.Ref.SessionID
		if key == "" {
			key = row.Ref.ChatID
		}
		if key == "" {
			continue
		}
		fired, elapsed := a.aggHb.tick(key, now, row.LastTs, threshold)
		if !fired {
			continue
		}
		dur := fmt.Sprintf("%dm", int64(elapsed)/int64(time.Minute))
		lines = append(lines, fmt.Sprintf(hub.CmdText(lang, "hb_line"), sessionTagOf(row.Ref), dur))
	}
	return strings.Join(lines, "\n")
}

// composeAggLine 组装聚合摘要行: agg_line(%1=replied %2=total %3=明细)。
// 词源 hub.CmdText(lang,key) 与 Console 文案同源;明细由 aggDetailLines 提供。
func composeAggLine(lang string, rows []hub.SessionStatus, replied, total int) string {
	return fmt.Sprintf(hub.CmdText(lang, "agg_line"), replied, total, strings.Join(aggDetailLines(lang, rows), "，"))
}

// aggDetailLines 组装逐目标明细行(摘要行与全终态报告共用):一行一目标
// `N【标题】状态词`; idle 在聚合语义=已完成(word_done), gone=已断开,
// perm 额外带摘要(truncateFirstLine 截到 40)。
func aggDetailLines(lang string, rows []hub.SessionStatus) []string {
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
