package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/yangk/kshell/internal/agenthook"
	"github.com/yangk/kshell/internal/applang"
	"github.com/yangk/kshell/internal/terminal"
)

// notifyPollInterval 是通知收件箱的轮询间隔。notifier 侧毫秒级落盘，
// 500ms 足够「任务完成 → 气泡出现」的体感，空轮询成本只是扫一次目录。
const notifyPollInterval = 500 * time.Millisecond

// notifyInboxMaxAge 是收件箱残留的保留时长：桌面端没在跑时 hook 照样写文件，
// 超过一天的残留只可能是无主垃圾，启动时统一清理。
const notifyInboxMaxAge = 24 * time.Hour

// windowIsMinimised 是 runtime.WindowIsMinimised 的包级变量抽象：测试注入用
//（对齐 windowHide / windowShow 的做法）。
var windowIsMinimised = runtime.WindowIsMinimised

// windowIsForeground 报告本进程是否拥有前台窗口；测试注入用。
// 默认实现见 window_foreground_*.go。
var windowIsForeground = defaultWindowIsForeground

// windowUnminimise 是 runtime.WindowUnminimise 的包级变量抽象：气泡点击恢复主窗用。
var windowUnminimise = runtime.WindowUnminimise

// notifyToast 是 showAgentToast 的包级变量抽象：测试注入用（已停用系统 toast 路径，保留便于断言未调用）。
var notifyToast = showAgentToast

// notifyBubble 是 showAgentBubble 的包级变量抽象：测试注入用。
var notifyBubble = showAgentBubble

// bubbleActivate 是原生气泡被点击时的回调；Startup 绑到 App.activateFromBubble。
var bubbleActivate = func(termKey string) {}

// dispatchNotifyLoop 轮询通知收件箱并分发事件，随进程存活（与 reapLoop 同生命周期）。
func (a *App) dispatchNotifyLoop() {
	dir, err := agenthook.InboxDir()
	if err != nil {
		return // 无主目录时无从收通知，静默放弃
	}
	ticker := time.NewTicker(notifyPollInterval)
	defer ticker.Stop()
	for range ticker.C {
		a.dispatchNotifyOnce(dir)
	}
}

// dispatchNotifyOnce 扫一遍收件箱：合法 payload 删文件后分发；解析失败的文件
// 直接删除跳过。单条异常不影响本轮其余文件。
func (a *App) dispatchNotifyOnce(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // 目录还没被 notifier 创建过是常态
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue // 写方是原子 rename，读失败多为瞬时竞争，下一轮再看
		}
		var p agenthook.Payload
		if err := json.Unmarshal(data, &p); err != nil || p.TermKey == "" {
			// 解析失败，或缺 termKey（无法归因到页签）：删除并跳过
			os.Remove(path)
			continue
		}
		// 先删再分发：分发耗时不可控，重复 emit 比丢一次事件更难解释
		if err := os.Remove(path); err != nil {
			continue // Windows 上文件被占用时删不掉，下一轮再试
		}
		a.handleAgentNotify(p)
	}
}

// handleAgentNotify 分发一条 agent 通知：就绪期等待确认噪音直接过滤（见
// dropUnpromptedWaiting）；窗口内重复事件丢弃（防气泡连刷）；始终推给前端
// （非前台期间前端照常入队）；主窗非前台/不可见时弹独立置顶气泡（非系统
// toast）；前台时只靠前端 NotificationBubble。
func (a *App) handleAgentNotify(p agenthook.Payload) {
	if a.dropUnpromptedWaiting(p) {
		return
	}
	if !a.notifyDedupFor().allow(p) {
		return
	}
	// 落 AgentHub 历史：与 ACP 通道（emitChatNotify）共用同一条写入路径，
	// 过滤/去重通过后才记，保证 Hub 只留用户真正会被通知的事件。
	a.recordHubAgentMessage(p)
	a.Emit("notify:agent", p)
	if a.windowVisible() {
		return
	}
	title, body := notifyToastText(p)
	// bubble 失败静默降级为仅 emit：没有独立气泡不影响应用内队列
	_ = notifyBubble(title, body, p.TermKey)
}

// dropUnpromptedWaiting 报告该通知是否应丢弃：等待确认类事件（claude/codebuddy
// 的 Notification、gemini 的 attention）若来自「新建会话且用户尚未发过首条输入」，
// 属就绪期噪音——agent 刚就绪必然在等输入，此时弹气泡只会打扰。恢复会话与
// 查不到的 key（外部窗口路径，输入不经 kshell 无从判断）一律放行，宁可多弹
// 不可漏弹。只查终端管理器：ACP chat 通知不经本函数（emitChatNotify 自行
// 接入去重器后直发），且 chat 与终端的 new:<n> key 是两套自增序列，查 chat
// 会撞 key 误杀。
func (a *App) dropUnpromptedWaiting(p agenthook.Payload) bool {
	if p.Event != "Notification" && p.Event != "attention" {
		return false
	}
	m := a.terminals()
	if m == nil {
		return false
	}
	for _, info := range m.List() {
		if info.Key == p.TermKey {
			return info.Kind == terminal.KindNew && !info.Prompted
		}
	}
	return false
}

// activateFromBubble 是原生气泡点击回调：恢复主窗并通知前端聚焦对应页签。
// 显示方式按窗口状态分支：任务栏最小化走 SC_RESTORE 恢复（等价任务栏点击，
// 保留最大化状态）；托盘隐藏 / 非前台被遮挡只前置（对齐托盘菜单）。
// 二者不可连调：windowUnminimise 的 SC_RESTORE 对最大化窗口会解除最大化，
// 无条件调用会改变窗体尺寸。
func (a *App) activateFromBubble(termKey string) {
	a.setWindowHidden(false)
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()
	if ctx != nil {
		if windowIsMinimised(ctx) {
			windowUnminimise(ctx)
		} else {
			windowShow(ctx)
		}
	}
	a.Emit("notify:focus", map[string]string{"termKey": termKey})
}

// windowVisible 报告主窗口是否适合用应用内气泡（用户能立刻看到）。
// 托盘隐藏 / 最小化 / 非前台 → false，改走独立气泡。
// wails v2.16 没有 WindowIsVisible：托盘用 hidden 标记；最小化用 runtime；
// 前台用 windowIsForeground。ctx 未就绪按可见处理（启动前安全默认）。
func (a *App) windowVisible() bool {
	a.mu.Lock()
	hidden, ctx := a.windowHidden, a.ctx
	a.mu.Unlock()
	if hidden {
		return false
	}
	if ctx == nil {
		return true
	}
	if windowIsMinimised(ctx) {
		return false
	}
	return windowIsForeground()
}

// setWindowHidden 维护主窗口「收进托盘」状态，供 BeforeClose / 托盘与二次启动的
// 显示路径调用。
func (a *App) setWindowHidden(hidden bool) {
	a.mu.Lock()
	a.windowHidden = hidden
	a.mu.Unlock()
}

// toolDisplayNames 是 toast 标题用的工具展示名映射（前端气泡有自己的展示，不共用）。
var toolDisplayNames = map[string]string{
	"claude":    "Claude Code",
	"codebuddy": "CodeBuddy",
	"codex":     "Codex",
	"opencode":  "OpenCode",
	"gemini":    "Gemini",
	"cursor":    "Cursor",
}

// notifyToastText 把 payload 映射成 toast 标题与正文。
// 标题 = 工具名 + 事件描述；正文 = 项目名（workspace 最后一段）。
// 不展示 Summary：agent 回复全文已进 AgentHub 对话框，通知再重复铺开
// 超长正文只会刷屏（2026-10-14 用户反馈）。
func notifyToastText(p agenthook.Payload) (string, string) {
	name := toolDisplayNames[p.Tool]
	if name == "" {
		name = p.Tool
	}
	if name == "" {
		name = "Agent"
	}
	event := applang.T("toast.task_done")
	switch p.Event {
	case "error":
		// chat 错误事件：与前端气泡的「任务出错」映射保持一致，两通道不能矛盾
		event = applang.T("toast.task_error")
	case "Notification", "attention":
		// claude hooks 的 Notification 与 OSC 扫描发出的 attention 都表示
		// agent 在等待用户确认，与「任务完成」语义必须区分
		event = applang.T("toast.waiting_confirm")
	}
	body := ""
	if ws := strings.TrimSpace(p.Workspace); ws != "" {
		// filepath.Base("") 返回 "."，需先排除空工作区；base 与前端
		// workspaceName（取最后一段、忽略末尾分隔符）语义一致
		if base := filepath.Base(ws); base != "." && base != string(filepath.Separator) {
			body = base
		}
	}
	return name + " " + event, body
}
