package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/yangk/kshell/internal/agenthook"
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

// notifyToast 是 showAgentToast 的包级变量抽象：测试注入用。
var notifyToast = showAgentToast

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

// handleAgentNotify 分发一条 agent 通知：始终推给前端（隐藏期间前端照常入队，
// 窗口重现后可见）；窗口不可见时额外补一条系统 toast。
func (a *App) handleAgentNotify(p agenthook.Payload) {
	a.Emit("notify:agent", p)
	if a.windowVisible() {
		return
	}
	title, body := notifyToastText(p)
	// toast 失败静默降级为仅 emit：没有系统通知不影响应用内气泡
	_ = notifyToast(title, body)
}

// windowVisible 报告主窗口当前是否对用户可见。
// wails v2.16 没有 WindowIsVisible：托盘隐藏是我们自己的 BeforeClose 干的，
// 用 hidden 标记跟踪；普通最小化用 runtime 查询。查询不可用（ctx 未就绪）按可见处理。
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
	return !windowIsMinimised(ctx)
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
// 标题 = 工具名 + 事件描述；正文 = 摘要，缺摘要时退回工作区路径。
func notifyToastText(p agenthook.Payload) (string, string) {
	name := toolDisplayNames[p.Tool]
	if name == "" {
		name = p.Tool
	}
	if name == "" {
		name = "Agent"
	}
	event := "任务完成"
	switch p.Event {
	case "error":
		// chat 错误事件：与前端气泡的「任务出错」映射保持一致，两通道不能矛盾
		event = "任务出错"
	case "Notification", "attention":
		// claude hooks 的 Notification 与 OSC 扫描发出的 attention 都表示
		// agent 在等待用户确认，与「任务完成」语义必须区分
		event = "等待确认"
	}
	body := p.Summary
	if body == "" {
		body = p.Workspace
	}
	return name + " " + event, body
}
