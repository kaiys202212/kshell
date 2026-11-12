package hubtools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// hubRouteTimeout 是路由单发请求的降级时限：超时即降级直投，不阻塞用户。
const hubRouteTimeout = 2 * time.Second

// AskMaxRounds 是反问链的连续轮数上限（桌面 Hub 与微信桥共用）：达到后下一
// 条自由文本不再进路由，直接回落确定性路径并清零轮次（避免模型反问无限套娃）。
const AskMaxRounds = 2

// 与 #gx 同规范的展开条数默认值与上限，避免两处口径漂移。
const (
	routeExpandDefault = 5
	routeExpandMax     = 20
)

// SessionBrief 是路由器可见的会话快照一行（来自 Executor.ListSessions 编号语境）。
type SessionBrief struct {
	Num    int    `json:"num"`
	Tool   string `json:"tool"`
	Title  string `json:"title"`
	Status string `json:"status"` // running/perm/idle/gone
}

// RouteDecision 是路由模型的结构化决策（一次调用内联产出，含按目标改写）。
type RouteDecision struct {
	Action        string            `json:"action"`               // group | single | status | expand | ask
	Targets       []int             `json:"targets,omitempty"`    // group/single 时的编号（SessionBrief.Num）
	Text          string            `json:"text,omitempty"`       // 群发/单投正文（未指定 per_target 时全目标共用）
	PerTargetText map[string]string `json:"per_target,omitempty"` // 编号→改写文本（群发改写内联产出，一次调用）
	ExpandNum     int               `json:"expand_num,omitempty"`
	ExpandCount   int               `json:"expand_count,omitempty"` // <=0=默认5
	Question      string            `json:"question,omitempty"`     // ask 时的自由式反问
}

// TextFor 取某编号目标的投递文本：PerTargetText 命中且非空白用改写文本，
// 否则退决策里的通用 Text。空改写视同未命中，避免把空串投出去。
// bridge（internal/weixin）与 desktop（internal/desktop）两处执行路径共用。
func (d RouteDecision) TextFor(n int) string {
	if t, ok := d.PerTargetText[strconv.Itoa(n)]; ok && strings.TrimSpace(t) != "" {
		return t
	}
	return d.Text
}

// RouteInput 调全局活跃模型做一次路由决策。prompt 含会话快照+用户原文+JSON
// 契约；响应经流式 JSON 解析（仿 ParseRouteAgent）+代码校验
// （编号必须存在于 snapshot、action 枚举合法）。任何失败返回 error，调用方降级。
func RouteInput(ctx context.Context, cfg Config, sessions []SessionBrief, input string) (RouteDecision, error) {
	ctx, cancel := context.WithTimeout(ctx, hubRouteTimeout)
	defer cancel()
	c := NewClient(cfg)
	resp, err := c.Chat(ctx, []Message{
		{Role: "system", Content: BuildRoutePrompt(sessions, input)},
		{Role: "user", Content: input},
	})
	if err != nil {
		return RouteDecision{}, err
	}
	return parseRouteDecision(resp.Text, sessions)
}

// parseRouteDecision 从模型回复中取出首个 JSON 对象并做代码侧校验：
// action 枚举、目标编号必须存在于快照、group 至少 2 个目标、single 至多 1 个、
// expand 编号合法并钳制条数、ask 必须有具体问题、per_target 键必须是快照编号。
func parseRouteDecision(text string, sessions []SessionBrief) (RouteDecision, error) {
	i := strings.Index(text, "{")
	if i < 0 {
		// 只在“完全不含 JSON 对象”分支附模型原文，避免把 JSON 截丢。
		return RouteDecision{}, fmt.Errorf("路由响应不含 JSON 对象，模型原文: %.200s", text)
	}
	var d RouteDecision
	// 仿 ParseRouteAgent：从首个 { 起流式解码，容忍 JSON 前后夹杂的自然语言。
	if err := json.NewDecoder(strings.NewReader(text[i:])).Decode(&d); err != nil {
		return RouteDecision{}, fmt.Errorf("解析路由 JSON: %w", err)
	}

	known := make(map[int]bool, len(sessions))
	for _, s := range sessions {
		known[s.Num] = true
	}

	// group/single 至少要有 text 或 per_target 之一，否则无内容可投。
	if d.Action == "group" || d.Action == "single" {
		if strings.TrimSpace(d.Text) == "" && len(d.PerTargetText) == 0 {
			return RouteDecision{}, fmt.Errorf("%s 缺少投递内容：text 与 per_target 至少其一非空", d.Action)
		}
	}

	switch d.Action {
	case "group":
		// 去重后再校验下限：重复编号（如 [1,1]）视为非法。
		d.Targets = dedupTargets(d.Targets)
		if len(d.Targets) < 2 {
			return RouteDecision{}, fmt.Errorf("group 去重后需要至少 2 个目标，得到 %d 个", len(d.Targets))
		}
		if err := checkTargets(d.Targets, known); err != nil {
			return RouteDecision{}, err
		}
	case "single":
		// 空 targets 合法：投递到当前会话。
		if len(d.Targets) > 1 {
			return RouteDecision{}, fmt.Errorf("single 至多 1 个目标，得到 %d 个", len(d.Targets))
		}
		if err := checkTargets(d.Targets, known); err != nil {
			return RouteDecision{}, err
		}
	case "status":
		// 无附加校验。
	case "expand":
		if !known[d.ExpandNum] {
			return RouteDecision{}, fmt.Errorf("expand_num %d 不在会话快照内", d.ExpandNum)
		}
		if d.ExpandCount <= 0 {
			d.ExpandCount = routeExpandDefault
		}
		if d.ExpandCount > routeExpandMax {
			d.ExpandCount = routeExpandMax
		}
	case "ask":
		if strings.TrimSpace(d.Question) == "" {
			return RouteDecision{}, errors.New("ask 缺少 question")
		}
	default:
		return RouteDecision{}, fmt.Errorf("未知路由 action: %q", d.Action)
	}

	// per_target 的键必须是快照内编号的字符串形式；group/single 时键还必须
	// 是 targets 的子集（在快照内但不在 targets 内也报错，严格优先）。
	inTargets := make(map[int]bool)
	for _, n := range d.Targets {
		inTargets[n] = true
	}
	for k := range d.PerTargetText {
		n, err := strconv.Atoi(k)
		if err != nil {
			return RouteDecision{}, fmt.Errorf("per_target 编号格式非法: %q", k)
		}
		if !known[n] {
			return RouteDecision{}, fmt.Errorf("per_target 含快照外编号: %q", k)
		}
		if (d.Action == "group" || d.Action == "single") && !inTargets[n] {
			return RouteDecision{}, fmt.Errorf("per_target 编号 %q 不在 targets 内", k)
		}
	}
	return d, nil
}

// checkTargets 校验目标编号全部存在于快照。
func checkTargets(targets []int, known map[int]bool) error {
	for _, n := range targets {
		if !known[n] {
			return fmt.Errorf("目标编号 %d 不在会话快照内", n)
		}
	}
	return nil
}

// dedupTargets 去除相邻/重复目标编号，保持首次出现顺序。
func dedupTargets(targets []int) []int {
	seen := make(map[int]bool, len(targets))
	out := targets[:0]
	for _, n := range targets {
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// BuildRoutePrompt 构造路由系统提示词：会话快照 + 用户原文 + 输出 JSON 契约 +
// 决策指引。快照行形态为「编号 工具 标题 状态」，供模型按编号输出 targets。
func BuildRoutePrompt(sessions []SessionBrief, input string) string {
	var b strings.Builder
	b.WriteString("你是 AgentHub 的消息路由器：根据会话快照和用户消息，决定这条消息应投递给哪些会话，或先向用户澄清。\n")
	b.WriteString("只输出一个 JSON 对象，不要输出解释、Markdown 代码块或其他任何文本。\n\n")

	b.WriteString("会话快照（每行：编号 工具 标题 状态；状态释义：running=运行中 perm=等待授权 idle=空闲 gone=会话已丢失）：\n")
	for _, s := range sessions {
		// 折叠标题换行，保证一行一个会话，编号行结构不被破坏。
		title := strings.ReplaceAll(s.Title, "\n", " ")
		b.WriteString(fmt.Sprintf("%d %s %s %s\n", s.Num, s.Tool, title, s.Status))
	}

	b.WriteString("\n用户消息：\n")
	b.WriteString(input)
	b.WriteString("\n\n输出 JSON 契约：\n")
	b.WriteString("- action：必填，取 group | single | status | expand | ask。\n")
	b.WriteString("- targets：目标会话编号数组；action=group 时至少 2 个。\n")
	b.WriteString("- single 时 targets 留空=投当前会话；非空则必须恰好 1 个合法编号。\n")
	b.WriteString("- text：群发/单投的正文；未指定 per_target 时所有目标共用。group/single 时 text 必填，或由 per_target 全量覆盖（两者至少其一非空）。\n")
	b.WriteString("- per_target：编号（字符串）到改写文本的映射；各会话语境不同时按编号分别改写。\n")
	b.WriteString("- expand_num：action=expand 时要展开的会话编号。\n")
	b.WriteString("- expand_count：展开条数，0 表示默认 5 条，最多 20 条。\n")
	b.WriteString("- question：action=ask 时的具体反问，不能为空。\n")

	b.WriteString("\n指引：\n")
	b.WriteString("1. 单会话语境（用户说“这个/继续”等）用 single，targets 留空即投当前会话。\n")
	b.WriteString("2. 多会话任务用 group，targets 至少 2 个；各会话语境不同时用 per_target 按编号给出改写文本，一次调用内联产出。\n")
	b.WriteString("3. 不确定意图或目标时用 ask，question 提出一个具体问题，不要猜测性群发。\n")
	b.WriteString("4. 用户询问各会话当前运行/等待授权/闲置状态时用 status，不需要其他字段。\n")
	b.WriteString("5. 用户要求查看某会话最近消息详情时用 expand，expand_num 为会话编号（可带 expand_count 条数）。\n")
	return b.String()
}
