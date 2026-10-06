package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/launcher"
	"github.com/yangk/kshell/internal/providers"
)

// settingsValue 取 args 中最后一组 --settings 的 JSON 值并解析成对象；
// 找不到或不是 JSON 时直接 Fatal（测试辅助，不处理业务分支）。
func settingsValue(t *testing.T, args []string) map[string]any {
	t.Helper()
	for i := len(args) - 2; i >= 0; i-- {
		if args[i] == "--settings" {
			var m map[string]any
			if err := json.Unmarshal([]byte(args[i+1]), &m); err != nil {
				t.Fatalf("--settings 值不是合法 JSON: %v（%q）", err, args[i+1])
			}
			return m
		}
	}
	t.Fatalf("args 中没有 --settings: %v", args)
	return nil
}

// hookCommand 从 settings 对象里取出指定事件的 hook 命令。
func hookCommand(t *testing.T, settings map[string]any, event string) string {
	t.Helper()
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("settings 中没有 hooks 对象: %v", settings)
	}
	entries, ok := hooks[event].([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("hooks.%s 不是单元素数组: %v", event, hooks[event])
	}
	inner, ok := entries[0].(map[string]any)["hooks"].([]any)
	if !ok || len(inner) != 1 {
		t.Fatalf("hooks.%s[0].hooks 不是单元素数组: %v", event, entries[0])
	}
	h := inner[0].(map[string]any)
	if h["type"] != "command" {
		t.Fatalf("hook type 应为 command，得到 %v", h["type"])
	}
	cmd, _ := h["command"].(string)
	return cmd
}

// claude 无既有 --settings：新增一组，Stop 与 Notification 都挂上 hook。
func TestNotifyArgsClaudeAppendsSettings(t *testing.T) {
	args := notifyArgs(nil, "claude", `C:\tools\kshell.exe`)
	if len(args) != 2 || args[0] != "--settings" {
		t.Fatalf("应追加一组 --settings，得到 %v", args)
	}
	m := settingsValue(t, args)
	if got := hookCommand(t, m, "Stop"); got != `"C:\tools\kshell.exe" agent-hook claude` {
		t.Fatalf("Stop hook 命令不符: %q", got)
	}
	if got := hookCommand(t, m, "Notification"); got != `"C:\tools\kshell.exe" agent-hook claude` {
		t.Fatalf("Notification hook 命令不符: %q", got)
	}
}

// claude 已有主题注入的 --settings JSON：hooks 合并进同一个 JSON，主题保留。
func TestNotifyArgsClaudeMergesExistingSettings(t *testing.T) {
	args := notifyArgs(
		[]string{"--resume", "abc", "--settings", `{"theme":"dark"}`},
		"claude", `C:\tools\kshell.exe`)
	if got := len(args); got != 4 {
		t.Fatalf("合并后应仍是 4 个参数，得到 %d: %v", got, args)
	}
	if args[0] != "--resume" || args[1] != "abc" {
		t.Fatalf("既有参数应原样保留: %v", args)
	}
	m := settingsValue(t, args)
	if m["theme"] != "dark" {
		t.Fatalf("主题字段应保留，得到 %v", m["theme"])
	}
	if got := hookCommand(t, m, "Stop"); got != `"C:\tools\kshell.exe" agent-hook claude` {
		t.Fatalf("Stop hook 命令不符: %q", got)
	}
}

// 既有 --settings JSON 已带 hooks（含 kshell 不注入的事件如 PreToolUse）：
// 合并后其他事件保留，kshell 需要的 Stop/Notification 注入进去。
func TestNotifyArgsMergesExistingHooks(t *testing.T) {
	args := notifyArgs(
		[]string{"--settings", `{"hooks":{"PreToolUse":[{"matcher":"Bash"}]}}`},
		"claude", `C:\tools\kshell.exe`)
	m := settingsValue(t, args)
	hooks, ok := m["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("hooks 应为对象: %v", m["hooks"])
	}
	if _, ok := hooks["PreToolUse"]; !ok {
		t.Fatalf("既有 PreToolUse 事件应保留，得到 %v", hooks)
	}
	if got := hookCommand(t, m, "Stop"); got != `"C:\tools\kshell.exe" agent-hook claude` {
		t.Fatalf("Stop hook 命令不符: %q", got)
	}
	if got := hookCommand(t, m, "Notification"); got != `"C:\tools\kshell.exe" agent-hook claude` {
		t.Fatalf("Notification hook 命令不符: %q", got)
	}
}

// 既有 hooks 已有同名事件（Stop）：以 kshell 注入为准，整体替换该事件键。
func TestNotifyArgsExistingStopOverridden(t *testing.T) {
	args := notifyArgs(
		[]string{"--settings", `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo old"}]}]}}`},
		"claude", `C:\tools\kshell.exe`)
	m := settingsValue(t, args)
	if got := hookCommand(t, m, "Stop"); got != `"C:\tools\kshell.exe" agent-hook claude` {
		t.Fatalf("同名事件应以 kshell 注入为准: %q", got)
	}
}

// codebuddy 的 hook 强制走 Git Bash 执行：命令路径必须用正斜杠。
func TestNotifyArgsCodeBuddyForwardSlash(t *testing.T) {
	args := notifyArgs(nil, "codebuddy", `C:\tools\kshell.exe`)
	m := settingsValue(t, args)
	want := `"C:/tools/kshell.exe" agent-hook codebuddy`
	if got := hookCommand(t, m, "Stop"); got != want {
		t.Fatalf("codebuddy hook 命令应为正斜杠路径，得到 %q", got)
	}
}

// codex 用 -c notify=[...] TOML 数组整体作为一个 argv 元素。
func TestNotifyArgsCodexTomlArray(t *testing.T) {
	args := notifyArgs([]string{"resume", "abc"}, "codex", `C:\tools\kshell.exe`)
	if len(args) != 4 {
		t.Fatalf("应追加 2 个参数，得到 %v", args)
	}
	if args[0] != "resume" || args[1] != "abc" {
		t.Fatalf("既有参数应原样保留: %v", args)
	}
	if args[2] != "-c" {
		t.Fatalf("倒数第二参数应为 -c: %v", args)
	}
	want := `notify=["C:\\tools\\kshell.exe","agent-hook","codex"]`
	if args[3] != want {
		t.Fatalf("TOML 数组不符:\n 得到 %q\n 期望 %q", args[3], want)
	}
}

// 无 hook 参数机制的工具原样返回，不添加任何参数（opencode 的注入走 env，不改 args）。
func TestNotifyArgsSkippedTools(t *testing.T) {
	for _, toolID := range []string{"gemini", "opencode", "cursor", "generic", ""} {
		args := notifyArgs([]string{"--foo"}, toolID, `C:\tools\kshell.exe`)
		if len(args) != 1 || args[0] != "--foo" {
			t.Fatalf("toolID %q 不应被注入，得到 %v", toolID, args)
		}
	}
}

// 已有 --settings 是文件路径（非 JSON 对象）时静默放弃注入，参数不变。
func TestNotifyArgsFilePathSettingsSkipped(t *testing.T) {
	args := notifyArgs([]string{"--settings", `C:\x\settings.json`}, "claude", "exe")
	if len(args) != 2 || args[1] != `C:\x\settings.json` {
		t.Fatalf("文件路径形态的 --settings 不应被改动，得到 %v", args)
	}
}

// applyNotifyInject 给 Spec 追加归因环境变量；无机制工具不动参数。
func TestApplyNotifyInjectEnv(t *testing.T) {
	spec := &launcher.Spec{Env: []string{"A=1"}, Args: []string{"--foo"}}
	applyNotifyInject(spec, "gemini", "session:s1", `D:\ws`)
	env := map[string]bool{}
	for _, kv := range spec.Env {
		env[kv] = true
	}
	if !env["KSHELL_TERM_KEY=session:s1"] || !env["KSHELL_WORKSPACE=D:\\ws"] {
		t.Fatalf("归因环境变量未追加: %v", spec.Env)
	}
	if !env["A=1"] {
		t.Fatalf("既有环境变量应保留: %v", spec.Env)
	}
	if len(spec.Args) != 1 || spec.Args[0] != "--foo" {
		t.Fatalf("gemini 不应注入参数: %v", spec.Args)
	}
}

// applyNotifyInject 对 claude 会把 hooks 合并进既有 --settings（真实 exe 路径不参与断言）。
func TestApplyNotifyInjectClaude(t *testing.T) {
	spec := &launcher.Spec{Env: []string{"X=1"}, Args: []string{"--settings", `{"theme":"dark"}`}}
	applyNotifyInject(spec, "claude", "new:1", `D:\ws`)
	m := settingsValue(t, spec.Args)
	if m["theme"] != "dark" {
		t.Fatalf("主题应保留: %v", m)
	}
	cmd := hookCommand(t, m, "Stop")
	if cmd == "" || !endsWithAgentHook(cmd, "claude") {
		t.Fatalf("hook 命令应以 agent-hook claude 结尾: %q", cmd)
	}
}

// applyNotifyLaunch 给外部窗口的启动描述注入 env 与 hook 参数（codex 形态）。
func TestApplyNotifyLaunch(t *testing.T) {
	l := providers.Launch{Env: map[string]string{"A": "B"}, Args: []string{"resume", "x"}}
	applyNotifyLaunch(&l, "codex", "window:t", `D:\ws`)
	if l.Env["KSHELL_TERM_KEY"] != "window:t" || l.Env["KSHELL_WORKSPACE"] != `D:\ws` {
		t.Fatalf("env 注入不符: %v", l.Env)
	}
	if l.Env["A"] != "B" {
		t.Fatalf("既有 env 应保留: %v", l.Env)
	}
	if len(l.Args) != 4 || l.Args[2] != "-c" {
		t.Fatalf("codex 参数注入不符: %v", l.Args)
	}
}

// opencode：生成临时插件目录并注入 OPENCODE_CONFIG_DIR；args 不变。
// 先清空继承的进程环境：kshell 启动的 opencode 会话自带该变量，
// 不隔离会让 openCodePluginDir 按设计跳过注入，测试在真实会话里误报。
func TestApplyNotifyInjectOpenCode(t *testing.T) {
	t.Setenv(envOpenCodeConfigDir, "")
	spec := &launcher.Spec{Env: []string{"A=1"}, Args: []string{"--foo"}}
	applyNotifyInject(spec, "opencode", "session:s1", `D:\ws`)
	cleanupOpenCodeEnv(t, spec.Env)

	env := map[string]string{}
	for _, kv := range spec.Env {
		i := strings.Index(kv, "=")
		env[kv[:i]] = kv[i+1:]
	}
	dir := env["OPENCODE_CONFIG_DIR"]
	if dir == "" {
		t.Fatalf("OPENCODE_CONFIG_DIR 未注入: %v", spec.Env)
	}
	if env["A"] != "1" || env["KSHELL_TERM_KEY"] != "session:s1" {
		t.Fatalf("既有 env 不符: %v", env)
	}
	if len(spec.Args) != 1 || spec.Args[0] != "--foo" {
		t.Fatalf("opencode 不应改动 args: %v", spec.Args)
	}
	content, err := os.ReadFile(filepath.Join(dir, "plugin", "kshell-notify.js"))
	if err != nil {
		t.Fatalf("插件文件缺失: %v", err)
	}
	s := string(content)
	if !strings.Contains(s, `"agent-hook"`) || !strings.Contains(s, `"opencode"`) {
		t.Fatalf("插件应调用 agent-hook opencode: %s", s)
	}
	if !strings.Contains(s, "session.idle") {
		t.Fatalf("插件应监听 session.idle: %s", s)
	}
}

// opencode 外部窗口路径：env map 注入 OPENCODE_CONFIG_DIR，插件文件同样生成。
// 同 TestApplyNotifyInjectOpenCode：先隔离继承的进程环境。
func TestApplyNotifyLaunchOpenCode(t *testing.T) {
	t.Setenv(envOpenCodeConfigDir, "")
	l := providers.Launch{Env: map[string]string{}, Args: []string{}}
	applyNotifyLaunch(&l, "opencode", "window:t", `D:\ws`)
	dir := l.Env["OPENCODE_CONFIG_DIR"]
	if dir == "" {
		t.Fatalf("OPENCODE_CONFIG_DIR 未注入: %v", l.Env)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if _, err := os.Stat(filepath.Join(dir, "plugin", "kshell-notify.js")); err != nil {
		t.Fatalf("插件文件缺失: %v", err)
	}
}

// 用户已有 OPENCODE_CONFIG_DIR（显式 env 或进程环境）时跳过注入，避免覆盖用户自定义配置目录。
func TestOpenCodePluginSkippedWhenEnvExists(t *testing.T) {
	spec := &launcher.Spec{Env: []string{"OPENCODE_CONFIG_DIR=C:\\my\\cfg"}}
	applyNotifyInject(spec, "opencode", "k", `D:\ws`)
	if got := envValue(spec.Env, "OPENCODE_CONFIG_DIR"); got != `C:\my\cfg` {
		t.Fatalf("用户配置目录应保留: %v", spec.Env)
	}
	// 进程环境里已有该变量也应跳过
	t.Setenv("OPENCODE_CONFIG_DIR", `C:\my\cfg`)
	spec2 := &launcher.Spec{}
	applyNotifyInject(spec2, "opencode", "k", `D:\ws`)
	if envValue(spec2.Env, "OPENCODE_CONFIG_DIR") != "" {
		t.Fatalf("进程环境已有该变量时不应注入: %v", spec2.Env)
	}
}

// cleanupOpenCodeEnv 删除测试生成的插件临时目录。
func cleanupOpenCodeEnv(t *testing.T, env []string) {
	t.Helper()
	if dir := envValue(env, "OPENCODE_CONFIG_DIR"); dir != "" {
		t.Cleanup(func() { os.RemoveAll(dir) })
	}
}

// cleanupOpenCodeTempDirsIn 只删超龄的 kshell-opencode-* 目录：
// 新鲜目录、非匹配条目（目录名不符 / 同名文件）都保留。
func TestCleanupOpenCodeTempDirs(t *testing.T) {
	root := t.TempDir()
	aged := filepath.Join(root, "kshell-opencode-old")
	fresh := filepath.Join(root, "kshell-opencode-new")
	other := filepath.Join(root, "other-dir")
	notDir := filepath.Join(root, "kshell-opencode-file")
	for _, d := range []string{aged, fresh, other} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(notDir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(aged, stale, stale); err != nil {
		t.Fatal(err)
	}

	cleanupOpenCodeTempDirsIn(root)

	if _, err := os.Stat(aged); !os.IsNotExist(err) {
		t.Fatalf("超龄目录应被删除")
	}
	for _, keep := range []string{fresh, other, notDir} {
		if _, err := os.Stat(keep); err != nil {
			t.Fatalf("%s 应保留: %v", keep, err)
		}
	}
}

// 扫描根不存在时静默返回：清理是锦上添花，绝不能影响启动。
func TestCleanupOpenCodeTempDirsRootMissing(t *testing.T) {
	cleanupOpenCodeTempDirsIn(filepath.Join(t.TempDir(), "not-exist")) // 不 panic 即通过
}

func endsWithAgentHook(cmd, tool string) bool {
	suffix := " agent-hook " + tool
	return len(cmd) > len(suffix) && cmd[len(cmd)-len(suffix):] == suffix
}
