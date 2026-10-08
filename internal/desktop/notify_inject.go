package desktop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/launcher"
	"github.com/yangk/kshell/internal/providers"
)

// 归因环境变量：hook/notifier 子进程继承后回传，桌面端据此定位页签。
const (
	envTermKey   = "KSHELL_TERM_KEY"
	envWorkspace = "KSHELL_WORKSPACE"
	// opencode 插件注入：指向临时配置目录（与用户全局配置合并而非替换，
	// 官方文档 https://opencode.ai/docs/config 确认）。用户已有该变量时跳过注入。
	envOpenCodeConfigDir = "OPENCODE_CONFIG_DIR"
)

// applyNotifyInject 把「agent 完成通知」所需注入加到内嵌终端启动规格上：
// 归因环境变量（所有工具）+ 按工具的 hook 参数（claude/codebuddy/codex）
// 或临时插件配置目录（opencode）。注入失败静默跳过：通知是锦上添花，绝不能影响会话启动。
func applyNotifyInject(spec *launcher.Spec, toolID, termKey, workspace string) {
	if spec == nil {
		return
	}
	spec.Env = append(spec.Env, envTermKey+"="+termKey, envWorkspace+"="+workspace)
	exe, err := os.Executable()
	if err != nil {
		return
	}
	spec.Args = materializeInlineSettings(notifyArgs(spec.Args, toolID, exe))
	if toolID == "opencode" {
		if dir := openCodePluginDir(exe, envValue(spec.Env, envOpenCodeConfigDir)); dir != "" {
			spec.Env = append(spec.Env, envOpenCodeConfigDir+"="+dir)
		}
	}
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
	l.Args = materializeInlineSettings(notifyArgs(l.Args, toolID, exe))
	if toolID == "opencode" {
		if dir := openCodePluginDir(exe, l.Env[envOpenCodeConfigDir]); dir != "" {
			l.Env[envOpenCodeConfigDir] = dir
		}
	}
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
		// gemini/cursor/generic 无会话级 hook 机制：跳过
		// （opencode 不在此注入参数，走 openCodePluginDir 写临时插件 + env）
		return args
	}
}

// settingsTempPrefix 是 claude/codebuddy --settings 临时 JSON 的文件名前缀。
// 内联 JSON 含 \"路径\" 时经 Windows cmd /S /C 会被拆坏（Invalid JSON），必须落盘传路径。
const settingsTempPrefix = "kshell-settings-"

// materializeInlineSettings 把 args 里内联的 --settings JSON 对象写到临时文件，
// 并把参数值换成绝对路径；已是文件路径或非法 JSON 时原样返回。失败静默（保留内联）。
func materializeInlineSettings(args []string) []string {
	for i := len(args) - 2; i >= 0; i-- {
		if args[i] != "--settings" {
			continue
		}
		v := strings.TrimSpace(args[i+1])
		if !strings.HasPrefix(v, "{") {
			return args
		}
		if !json.Valid([]byte(v)) {
			return args
		}
		f, err := os.CreateTemp("", settingsTempPrefix+"*.json")
		if err != nil {
			return args
		}
		path := f.Name()
		if _, err := f.WriteString(v); err != nil {
			f.Close()
			os.Remove(path)
			return args
		}
		if err := f.Close(); err != nil {
			os.Remove(path)
			return args
		}
		args[i+1] = path
		return args
	}
	return args
}

// openCodeTempDirPrefix 是 opencode 插件临时目录的名称前缀（与 MkdirTemp 的 pattern 一致）。
const openCodeTempDirPrefix = "kshell-opencode-"

// openCodeTempDirMaxAge 是插件临时目录的保留时长：超过说明对应会话早已结束
//（每次会话启动都新建目录），属于无主残留，桌面端启动时统一清理。
const openCodeTempDirMaxAge = 24 * time.Hour

// cleanupOpenCodeTempDirs 清理系统 %TEMP% 下历史遗留的 opencode 插件临时目录。
func cleanupOpenCodeTempDirs() {
	cleanupOpenCodeTempDirsIn(os.TempDir())
	cleanupSettingsTempFilesIn(os.TempDir())
}

// cleanupSettingsTempFilesIn 删除 root 下超龄的 kshell-settings-*.json。
func cleanupSettingsTempFilesIn(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), settingsTempPrefix) || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) <= openCodeTempDirMaxAge {
			continue
		}
		os.Remove(filepath.Join(root, e.Name()))
	}
}

// cleanupOpenCodeTempDirsIn 扫描 root 下 openCodeTempDirPrefix 前缀的目录，
// 删除修改时间早于 openCodeTempDirMaxAge 的；任何错误静默——清理是锦上添花。
// root 参数化是为了测试注入，避免测试真删系统 TEMP。
func cleanupOpenCodeTempDirsIn(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), openCodeTempDirPrefix) {
			continue
		}
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) <= openCodeTempDirMaxAge {
			continue
		}
		os.RemoveAll(filepath.Join(root, e.Name()))
	}
}

// openCodePluginDir 生成 opencode 通知插件临时目录并返回其路径；失败或无需注入返回空串。
// 机制：OPENCODE_CONFIG_DIR 指向临时目录后，opencode 会搜索其中的 plugin/ 子目录加载插件，
// 且与用户全局配置（~/.config/opencode）合并而非替换——用户自己的模型/MCP 配置不受影响。
// 已有 OPENCODE_CONFIG_DIR（显式 env 或进程环境）时返回空串跳过，避免覆盖用户自定义配置目录。
// 临时目录留在系统 %TEMP% 下，桌面端启动时由 cleanupOpenCodeTempDirs 清理超龄残留。
func openCodePluginDir(exePath, existing string) string {
	if existing != "" || os.Getenv(envOpenCodeConfigDir) != "" {
		return ""
	}
	dir, err := os.MkdirTemp("", openCodeTempDirPrefix)
	if err != nil {
		return ""
	}
	// 后续任一步失败都回收已建目录，避免 %TEMP% 无界累积
	pluginDir := filepath.Join(dir, "plugin")
	if err := os.MkdirAll(pluginDir, 0o700); err != nil {
		os.RemoveAll(dir)
		return ""
	}
	// exe 路径用 JSON 编码成 JS 字符串字面量；插件跑在 Bun 里，其 child_process.spawn
	// 会把 Windows 反斜杠当转义符吞掉，必须先转成正斜杠。
	exe, err := json.Marshal(filepath.ToSlash(exePath))
	if err != nil {
		os.RemoveAll(dir)
		return ""
	}
	content := fmt.Sprintf(openCodePluginTpl, exe)
	if err := os.WriteFile(filepath.Join(pluginDir, "kshell-notify.js"), []byte(content), 0o600); err != nil {
		os.RemoveAll(dir)
		return ""
	}
	return dir
}

// openCodePluginTpl 是生成的 opencode 插件：session.idle（一轮回复结束）时以
// 末位 argv JSON 形态唤起 kshell agent-hook（internal/agenthook 约定）。
// await 子进程退出：agent-hook 毫秒级返回，等待无代价，且避免 detached 子进程
// 随 opencode 退出被连带终止（实测踩坑）；任何失败静默吞掉，通知绝不打扰 agent。
// 唯一 %s 是 kshell 自身 exe 路径（JSON 字符串字面量，正斜杠）。
const openCodePluginTpl = `// kshell 生成的 opencode 通知插件（临时目录，勿手改）。
export const KshellNotify = async () => ({
  event: async ({ event }) => {
    if (!event || event.type !== "session.idle") return
    try {
      const { spawn } = await import("node:child_process")
      await new Promise((resolve) => {
        const child = spawn(%s, ["agent-hook", "opencode", JSON.stringify(event)], {
          stdio: "ignore",
          windowsHide: true,
        })
        child.on("error", () => resolve())
        child.on("exit", () => resolve())
      })
    } catch {}
  },
})
`

// envValue 从 KEY=VALUE 形式的环境变量列表里取指定键的值。
func envValue(env []string, key string) string {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return kv[len(prefix):]
		}
	}
	return ""
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
