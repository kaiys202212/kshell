package desktop

// ask.go：管理者反问卡片的交互答复入口（HubAskAnswer）与挂起-续跑语义的
// desktop 侧接线。挂起态本身归 agentapi 所有（Run 记挂起、Resume/短输入
// Run 消费），desktop 只负责语境查询、落盘与门禁。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/yangk/kshell/internal/agentapi"
	"github.com/yangk/kshell/internal/hub"
)

// hubSuspendProbe 查询指定渠道是否存在可续跑的挂起反问。生产实现走
// AgentAPI().HasSuspendedAsk（每次访问实时刷新 LLM 配置，与 runHubLoop 同
// 口径）；包级变量仅为测试注入——挂起态在 agentapi API 内部（unexported），
// desktop 循环桩返回 Ask 并不会真实置位挂起，测试用探针模拟语境。
var hubSuspendProbe = func(a *App, channel string) bool {
	return a.AgentAPI().HasSuspendedAsk(channel)
}

// hubAskSuspended 报告 hub 渠道挂起反问在场：裸数字守卫与「继续」捷径让位
// 共用同一判定，保证两者对「数字/短确认是反问回答」的口径一致。
func (a *App) hubAskSuspended() bool {
	return hubSuspendProbe(a, "hub")
}

// resumeHubLoop 续跑挂起反问：注入桩优先（复用 hubRunner——测试在同一桩内
// 按输入区分 Run/Resume 语义），nil 时用 AgentAPI().Resume（无挂起/超时/
// 渠道不符由 agentapi 校验，返回 ErrNoSuspend）。
func (a *App) resumeHubLoop(input string, opts agentapi.RunOpts) (agentapi.RunResult, error) {
	a.mu.Lock()
	fn := a.hubRunner
	a.mu.Unlock()
	if fn != nil {
		return fn(context.Background(), input, opts)
	}
	return a.AgentAPI().Resume("hub", input, opts)
}

// finishManagerLoop 落盘管理者循环产出并转 HubPickView（HubSmartSend 与
// HubAskAnswer 共用，避免落盘口径分叉）：
//   - 思考链非空 → hub_manager_think（Text 聚合摘要 + Steps 结构化留底）；
//   - 挂起反问 → hub_manager_ask 卡片（问题/选项/模式），问题不再落
//     hub_manager_reply——卡片才是交互载体，答复经卡片按钮（HubAskAnswer）
//     或自由文本（HubSmartSend）进入；
//   - 结论 → hub_manager_reply 逐条落盘。
//
// 返回值始终 Pending=false：落盘已发生、hub:changed 已推送，前端清草稿并
// 重拉历史渲染卡片；Replies 原样回显保持既有回执契约。
func (a *App) finishManagerLoop(res agentapi.RunResult, steps []hub.Step) HubPickView {
	if len(steps) > 0 {
		a.recordHubMessage(hub.Message{
			Kind:  "agent",
			Event: "hub_manager_think",
			Text:  summarizeManagerSteps(steps),
			Steps: steps,
		})
	}
	if res.Suspended && res.Ask != nil {
		a.recordHubMessage(hub.Message{
			Kind:  "agent",
			Event: "hub_manager_ask",
			Text:  res.Ask.Question,
			Ask: &hub.AskWire{
				Question: res.Ask.Question,
				Options:  res.Ask.Options,
				Mode:     res.Ask.Mode,
			},
		})
	} else {
		for _, r := range res.Replies {
			a.recordHubMessage(hub.Message{Kind: "agent", Event: "hub_manager_reply", Text: r})
		}
	}
	replies := make([]hub.Reply, 0, len(res.Replies))
	for _, r := range res.Replies {
		replies = append(replies, hub.Reply{Text: r})
	}
	return HubPickView{Replies: replies, Pending: false}
}

// markLastAskAnswered 把最后一张未答复的 hub_manager_ask 卡片标记为已答复
//（SetStatus 落盘）。找不到不报错：卡片可能已归档或历史被清理，答复链路
// 不应因标记失败而中断。
func (a *App) markLastAskAnswered() {
	store := a.snapshot().HubStore
	if store == nil {
		return
	}
	var lastID int64
	for _, m := range store.List() {
		if m.Event == "hub_manager_ask" && m.Status != hub.StatusAnswered {
			lastID = m.ID
		}
	}
	if lastID != 0 {
		store.SetStatus(lastID, hub.StatusAnswered)
	}
}

// HubAskAnswer 处理反问卡片的交互答复：Resume 从挂起态续跑并把产出落盘，
// 同时把最近一张反问卡片标记为已答复。答复不落 user 消息——卡片交互不进
// hub 历史 user 流（卡片本身已呈现问题，再落一份答复会造成重复视觉）；
// 自由文本答复走 HubSmartSend 才正常落盘。
//
// 空答复报错；在途处理（smartBusy，与 HubSmartSend 共用门禁）静默回落
// Pending=true；无挂起（超时/已被消费/渠道不符）返回 err.hub.ask_expired
//（wrap ErrNoSuspend 供归因），其余错误原样透传。
func (a *App) HubAskAnswer(answer string) (HubPickView, error) {
	if !a.snapshot().ready() {
		return HubPickView{}, errNotReady
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return HubPickView{}, errors.New("err.hub.ask_empty")
	}
	// busy 门禁与 HubSmartSend 共用：卡片连点或与自由发送并发时，后到的
	// 直接回落（前端保留输入），防同一挂起态被重复消费。
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
	var mgrSteps []hub.Step
	res, err := a.resumeHubLoop(answer, agentapi.RunOpts{
		Channel: "hub",
		Progress: func(p agentapi.LoopProgress) {
			mgrSteps = append(mgrSteps, hub.Step{Tool: p.Tool, Round: p.Round})
		},
	})
	if err != nil {
		if errors.Is(err, agentapi.ErrNoSuspend) {
			// 卡片已过期：wire key 供前端翻译成「反问已过期」toast，ErrNoSuspend
			// 留在错误链上供测试/日志归因。
			return HubPickView{}, fmt.Errorf("err.hub.ask_expired|%w", agentapi.ErrNoSuspend)
		}
		return HubPickView{}, err
	}
	// 先标记再落盘：续跑可能立刻再次反问，先关旧卡才不会把新卡误标已答。
	a.markLastAskAnswered()
	return a.finishManagerLoop(res, mgrSteps), nil
}
