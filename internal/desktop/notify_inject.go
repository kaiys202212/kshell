package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/yangk/kshell/internal/launcher"
	"github.com/yangk/kshell/internal/providers"
)

// 归因环境变量：hook/notifier 子进程继承后回传，桌面端据此定位页签。
const (
	envTermKey   = "KSHELL_TERM_KEY"
	envWorkspace = "KSHELL_WORKSPACE"
)

// applyNotifyInject 把「agent 完成通知」所需注入加到内嵌终端启动规格上：
// 归因环境变量（所有工具）+ 按工具的 hook 参数（仅 claude/codebuddy/codex）。
// 注入失败静默跳过：通知是锦上添花，绝不能影响会话启动。
func applyNotifyInject(spec *launcher.Spec, toolID, termKey, workspace string) {
	if spec == nil {
		return
	}
	spec.Env = append(spec.Env, envTermKey+"="+termKey, envWorkspace+"="+workspace)
	exe, err := os.Executable()
	if err != nil {
		return
	}
	spec.Args = notifyArgs(spec.Args, toolID, exe)
}

// applyNotifyLaunch 与 applyNotifyInject 同口径，作用于外部 PowerShell 窗口的启动描述
// （launchWindow 路径：env 是 map，最终由 psStatement 展开成 $env: 行）。
func applyNotifyLaunch(l *providers.Launch, toolID, termKey, workspace string) {
	if l == nil {
		return
	}
	l.Env = providers.MergeEnv(l.Env, map[string]string{
		envTermKey:   termKey,
		envWorkspace: workspace,
	})
	exe, err := os.Executable()
	if err != nil {
		return
	}
	l.Args = notifyArgs(l.Args, toolID, exe)
}

// notifyArgs 按工具追加通知 hook 参数；无机制的工具原样返回。
// exePath 是 kshell 自身可执行文件路径（hook 的 notifier 就是我们自己的子命令）。
func notifyArgs(args []string, toolID, exePath string) []string {
	switch toolID {
	case "claude":
		// claude hook 由 cmd 兼容方式执行：路径保持反斜杠，双引号包裹。
		return mergeSettingsHooks(args, `"`+exePath+`" agent-hook claude`)
	case "codebuddy":
		// codebuddy 的 hook 强制走 Git Bash 执行：反斜杠路径会被吞掉，必须转正斜杠。
		return mergeSettingsHooks(args, `"`+filepath.ToSlash(exePath)+`" agent-hook codebuddy`)
	case "codex":
		// codex 的 notify 是全局单值 TOML 配置，-c 数组整体作为一个 argv 元素。
		// 会覆盖用户自己的 notify 配置，但仅限 kshell 启动的会话进程，可接受。
		return append(args, "-c", "notify=["+tomlQuote(exePath)+`,"agent-hook","codex"]`)
	default:
		// gemini/opencode/cursor/generic 无会话级 hook 机制：跳过
		return args
	}
}

// mergeSettingsHooks 把 hooks 配置合并进 args 里已有的 --settings JSON
// （kshell 主题注入产生的形态：--settings {"theme":"dark"}），没有则新增一组。
// 已有值不是 JSON 对象（如文件路径）时静默放弃注入。
func mergeSettingsHooks(args []string, command string) []string {
	for i := len(args) - 2; i >= 0; i-- {
		if args[i] != "--settings" {
			continue
		}
		v := strings.TrimSpace(args[i+1])
		if !strings.HasPrefix(v, "{") {
			return args
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(v), &m); err != nil || m == nil {
			return args
		}
		m["hooks"] = mergeHooks(m["hooks"], notifyHooks(command))
		b, err := json.Marshal(m)
		if err != nil {
			return args
		}
		args[i+1] = string(b)
		return args
	}
	b, err := json.Marshal(map[string]any{"hooks": notifyHooks(command)})
	if err != nil {
		return args
	}
	return append(args, "--settings", string(b))
}

// notifyHooks 构造 claude/codebuddy settings 的 hooks 字段：
// Stop → 一轮任务完成；Notification → 等待用户确认（结构与官方 hooks.json 一致）。
func notifyHooks(command string) map[string]any {
	wrap := func() []any {
		return []any{map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": command}},
		}}
	}
	return map[string]any{
		"Stop":         wrap(),
		"Notification": wrap(),
	}
}

// mergeHooks 把 kshell 注入的事件合并进既有 hooks 对象，保留用户已有的事件键；
// 同名事件键（如 Stop）以 kshell 注入为准——kshell 启动的会话需要保证通知可达。
// 既有值不是对象（形态异常）时整体以注入为准，不报错。
func mergeHooks(existing, inject any) any {
	em, ok := existing.(map[string]any)
	if !ok {
		return inject
	}
	merged := make(map[string]any, len(em)+2)
	for k, v := range em {
		merged[k] = v
	}
	for k, v := range inject.(map[string]any) {
		merged[k] = v
	}
	return merged
}

// tomlQuote 把字符串编码为 TOML 基本字符串；Windows 路径只需处理反斜杠与引号。
func tomlQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
