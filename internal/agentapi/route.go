package agentapi

import (
	"context"
	"fmt"

	"github.com/yangk/kshell/internal/hubtools"
)

// RoutePlan 是投递路由的翻译结果：保留 hubtools 的原始决策（编号语境），
// 并把编号翻译回会话。Sessions[i] 对应编号 i+1（与 RouteDecide 合成快照
// 的顺序一致）。
type RoutePlan struct {
	hubtools.RouteDecision
	Sessions []Session
}

// RouteDecide 调 LLM 对一条用户消息做投递决策：应发给哪些会话（single/group）、
// 需要反问（ask）、还是只是查状态/展开详情（status/expand，调用方用
// List/Messages 原语消化）。LLM 未就绪返回 ErrLLMNotReady。
func (a *API) RouteDecide(ctx context.Context, text string) (RoutePlan, error) {
	cfg := a.llmCfg()
	if !hubtools.Ready(cfg) {
		return RoutePlan{}, ErrLLMNotReady
	}
	active, err := a.List(ScopeActive)
	if err != nil {
		return RoutePlan{}, err
	}
	briefs := make([]hubtools.SessionBrief, len(active))
	for i, s := range active {
		briefs[i] = hubtools.SessionBrief{Num: i + 1, Tool: s.Tool, Title: s.Title, Status: string(s.Status)}
	}
	resp, err := a.chat(ctx, []hubtools.Message{
		{Role: "system", Content: hubtools.BuildRoutePrompt(briefs, text)},
		{Role: "user", Content: text},
	})
	if err != nil {
		return RoutePlan{}, err
	}
	d, err := hubtools.ParseRouteDecision(resp.Text, briefs)
	if err != nil {
		return RoutePlan{}, err
	}
	plan := RoutePlan{RouteDecision: d, Sessions: active}
	return plan, nil
}

// RouteSend 执行 single/group 投递：按编号取改写文本（per_target 优先，
// 回落通用 text），逐个 Backend.Send。ask/status/expand 无投递语义，显式报错。
func (a *API) RouteSend(plan RoutePlan) error {
	switch plan.Action {
	case "single", "group":
	default:
		return fmt.Errorf("agentapi: action %q 无投递语义，请改用 List/Messages 原语", plan.Action)
	}
	for _, n := range plan.Targets {
		if n < 1 || n > len(plan.Sessions) {
			return fmt.Errorf("agentapi: 目标编号 %d 超出会话快照", n)
		}
		s := plan.Sessions[n-1]
		if s.ID == "" {
			return fmt.Errorf("%w: %s", ErrNotFound, s.SessionID)
		}
		if err := a.b.Send(s.ID, plan.TextFor(n)); err != nil {
			return err
		}
	}
	return nil
}
