// Package agenthook 接收 agent CLI 的 hook/notify 事件并落盘到通知收件箱。
//
// 本进程由 agent CLI（claude code 的 Stop/Notification hook、codex 的 notify、
// opencode 插件等）拉起，因此有一条铁律：任何失败都静默返回 0——hook 非零
// 退出会向 agent 报错，打扰正在工作的 agent。
//
// 归因机制：只有 kshell 启动的 agent 会话才会被注入 KSHELL_TERM_KEY 环境变量，
// 缺失（或为空）一律静默退出，保证用户手工开的 agent 会话不产生通知。
package agenthook

import (
	"encoding/json"
	"io"
	"os"
	"strings"
)

// Handle 是 agent-hook 隐藏子命令的入口，args 为去掉二进制名后的 os.Args[1:]。
// 当前所有路径都返回 0：hook 非零退出会被 agent CLI 当作失败上报，打扰对话。
func Handle(args []string) int {
	return handle(args, os.Stdin)
}

func handle(args []string, stdin io.Reader) int {
	// 空值与未设置同等对待：防止父进程误配空变量导致所有会话都命中
	if os.Getenv("KSHELL_TERM_KEY") == "" {
		return 0
	}
	if len(args) == 0 {
		return 0
	}

	tool, raw := readEventJSON(args, stdin)
	if raw == "" {
		return 0
	}
	event, summary, ok := extractFields(raw)
	if !ok {
		// 坏 JSON：静默丢弃，收件箱只保留能被桌面端解析的事件
		return 0
	}

	p := Payload{
		Tool:      tool,
		Event:     event,
		TermKey:   os.Getenv("KSHELL_TERM_KEY"),
		Workspace: os.Getenv("KSHELL_WORKSPACE"),
		Summary:   summary,
		Raw:       raw,
	}
	if err := writeInbox(p); err != nil {
		return 0
	}
	return 0
}

// readEventJSON 返回 (tool 名, 原始 JSON)。JSON 来源约定：
//   - 末位 argv 以 { 开头 → codex notify / opencode 插件把 JSON 追加为最后一个参数，
//     此前如有参数则第一个是 tool 名；
//   - 否则读 stdin（claude/codebuddy/cursor 的 hook：argv 只有 tool 名，JSON 走管道）。
func readEventJSON(args []string, stdin io.Reader) (string, string) {
	rest := args[1:]
	if len(rest) > 0 && looksLikeJSON(rest[len(rest)-1]) {
		tool := ""
		if len(rest) >= 2 {
			tool = rest[0]
		}
		return tool, rest[len(rest)-1]
	}
	tool := ""
	if len(rest) > 0 {
		tool = rest[0]
	}
	b, err := io.ReadAll(stdin)
	if err != nil {
		return tool, ""
	}
	return tool, strings.TrimSpace(string(b))
}

func looksLikeJSON(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), "{")
}

// extractFields 启发式提取事件名与摘要：
//   - claude/codebuddy/cursor 的 hook JSON 带 hook_event_name（如 "Stop"/"Notification"）；
//   - codex notify 的 JSON 带 type（如 "agent-turn-complete"）；
//   - Notification 场景可能带 message 作为摘要。
//
// ok=false 表示 JSON 无法解析成对象，调用方应静默丢弃。
func extractFields(raw string) (event, summary string, ok bool) {
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return "", "", false
	}
	if v, ok := m["hook_event_name"].(string); ok {
		event = v
	} else if v, ok := m["type"].(string); ok {
		event = v
	}
	if v, ok := m["message"].(string); ok {
		summary = v
	}
	return event, summary, true
}
