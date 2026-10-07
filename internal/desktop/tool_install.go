package desktop

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/executil"
	"github.com/yangk/kshell/internal/providers"
)

var (
	errUnknownTool  = errors.New("err.tool.unknown")
	errNotInstaller = errors.New("err.tool.no_install_recipe")
	errInstallBusy  = errors.New("err.tool.install_in_progress")
	errCursorPurge  = errors.New("err.tool.cursor_no_clear")
	errCursorNoBin  = errors.New("err.tool.cursor_agent_not_found")
)

// InstallRecipeView 是回传前端的安装配方预览：命令只读，CanPurge 由 PurgeDirs 是否为空决定。
type InstallRecipeView struct {
	ToolID       string
	Name         string
	InstallCmd   string
	UninstallCmd string
	PurgeDirs    []string
	CanPurge     bool
}

// ToolInstallJobView 是当前安装/卸载任务快照，供前端轮询日志与忙状态。
type ToolInstallJobView struct {
	ToolID  string
	Action  string
	Log     string
	Error   string
	Running bool
	// Trigger 为 "auto" 表示这是残缺自动修复任务，前端显示「检测到损坏，正在自动修复」。
	Trigger string
}

// installCmdRunner 执行一条完整 shell 命令并把输出按行回调；测试注入，生产走 runShellCommand。
type installCmdRunner func(ctx context.Context, shell, cmdline string, onLog func(string)) error

type installJob struct {
	toolID  string
	action  string // install | uninstall
	running bool
	trigger string // manual | auto：auto 表示残缺自动修复
	log     strings.Builder
	errText string
}

// GetToolInstallRecipe 按工具 ID 查内置安装配方；未知 ID 或自定义 Generic 分别报错。
func (a *App) GetToolInstallRecipe(id string) (InstallRecipeView, error) {
	p, r, err := a.lookupRecipe(id)
	if err != nil {
		return InstallRecipeView{}, err
	}
	return InstallRecipeView{
		ToolID:       p.ID(),
		Name:         p.DisplayName(),
		InstallCmd:   r.InstallCmd,
		UninstallCmd: r.UninstallCmd,
		PurgeDirs:    r.PurgeDirs,
		CanPurge:     len(r.PurgeDirs) > 0,
	}, nil
}

// InstallBuiltinTool 启动后台安装；启动失败（未知 ID 等）同步返回，执行失败走 done 事件。
func (a *App) InstallBuiltinTool(id string) error {
	_, r, err := a.lookupRecipe(id)
	if err != nil {
		return err
	}
	return a.startInstallJob(id, "install", r, false, "manual")
}

// UninstallBuiltinTool 启动后台卸载。purgeConfig 且配方未声明 PurgeDirs（含 Cursor）直接拒绝。
func (a *App) UninstallBuiltinTool(id string, purgeConfig bool) error {
	_, r, err := a.lookupRecipe(id)
	if err != nil {
		return err
	}
	if purgeConfig && len(r.PurgeDirs) == 0 {
		return errCursorPurge
	}
	return a.startInstallJob(id, "uninstall", r, purgeConfig, "manual")
}

// GetToolInstallJob 拷贝当前任务视图（Log 为累积文本）。
func (a *App) GetToolInstallJob() ToolInstallJobView {
	a.mu.Lock()
	defer a.mu.Unlock()
	return ToolInstallJobView{
		ToolID:  a.installJob.toolID,
		Action:  a.installJob.action,
		Log:     a.installJob.log.String(),
		Error:   a.installJob.errText,
		Running: a.installJob.running,
		Trigger: a.installJob.trigger,
	}
}

func (a *App) lookupRecipe(id string) (providers.Provider, providers.InstallRecipe, error) {
	for _, p := range a.snapshot().Providers {
		if p.ID() != id {
			continue
		}
		r, ok := providers.RecipeOf(p)
		if !ok {
			return p, providers.InstallRecipe{}, errNotInstaller
		}
		return p, r, nil
	}
	return nil, providers.InstallRecipe{}, errUnknownTool
}

func (a *App) startInstallJob(id, action string, recipe providers.InstallRecipe, purge bool, trigger string) error {
	a.mu.Lock()
	if a.installJob.running {
		a.mu.Unlock()
		return errInstallBusy
	}
	a.installJob = installJob{toolID: id, action: action, running: true, trigger: trigger}
	a.mu.Unlock()
	go a.execInstallJob(id, action, recipe, purge)
	return nil
}

func (a *App) execInstallJob(id, action string, recipe providers.InstallRecipe, purge bool) {
	var runErr error
	defer func() {
		// 先清 running 再发 done：前端收到事件后立刻 GetToolInstallJob，应看到已结束。
		a.mu.Lock()
		a.installJob.running = false
		errText := ""
		if runErr != nil {
			errText = runErr.Error()
			a.installJob.errText = errText
		}
		// 成功后的修复态维护：卸载写标记（阻止自动修复把刚卸的装回来），
		// 安装清标记。重试计数**不在这里清**——安装退出 0 不代表工具真恢复
		// （假成功会把计数清回 0 形成连环重装），计数只由扫描发现工具不再
		// Broken 时在 maybeAutoRepair 里清零（设计 §3 的另一半）。
		if runErr == nil {
			if action == "uninstall" {
				a.setUninstalledMarker(id)
			} else {
				a.clearUninstalledMarker(id)
			}
		}
		a.mu.Unlock()
		a.Emit("tool:install:done", map[string]any{
			"toolID": id,
			"action": action,
			"ok":     runErr == nil,
			"error":  errText,
		})
	}()

	onLog := func(line string) {
		a.mu.Lock()
		a.installJob.log.WriteString(line)
		a.installJob.log.WriteByte('\n')
		a.mu.Unlock()
		a.Emit("tool:install:log", map[string]any{"toolID": id, "text": line})
	}

	runErr = a.execInstallCommand(id, action, recipe, onLog)
	if runErr != nil {
		return
	}

	if action == "uninstall" && purge {
		if err := a.purgeConfigDirs(recipe.PurgeDirs, a.snapshot().Home); err != nil {
			onLog("tool.log.clear_config_failed|" + err.Error())
			runErr = err
			return
		}
	}

	// 刚装上的 CLI 常不在当前进程 PATH：npm 全局目录，以及 Cursor 的安装目录。
	ensureToolBinsOnPATH()

	o := a.snapshot()
	tools := discovery.DetectAll(o.Home, o.Providers)
	a.mu.Lock()
	a.tools = tools
	a.keepInstallTools = true
	a.toolsReady = true
	a.mu.Unlock()

	_, _ = a.ScanSessions()
}

func (a *App) execInstallCommand(id, action string, recipe providers.InstallRecipe, onLog func(string)) error {
	if action == "install" {
		return a.runInstallCmd(a.runCtx(), recipe.Shell, recipe.InstallCmd, onLog)
	}
	var cmdErr error
	// 先补常见安装目录进 PATH，否则 FindBins/Detect 看不到 %APPDATA%\npm 里的残留。
	ensureToolBinsOnPATH()
	if recipe.UninstallCmd != "" {
		cmdErr = a.runInstallCmd(a.runCtx(), recipe.Shell, recipe.UninstallCmd, onLog)
	} else if tool := a.toolByID(id); tool.BinPath != "" {
		// node 入口形态（BinArgs 非空）：BinPath 是版本目录里的 node.exe，
		// 单删它目录与 index.js 还在，DetectAll 重扫后工具依然「已安装」，
		// 必须整个版本目录一起删。
		if len(tool.BinArgs) > 0 {
			cmdErr = os.RemoveAll(filepath.Dir(tool.BinPath))
		} else if err := os.Remove(tool.BinPath); err != nil && !os.IsNotExist(err) {
			cmdErr = err
		}
	} else {
		cmdErr = errCursorNoBin
	}

	removed := a.removeLeftoverBins(id, onLog)
	if left := a.leftoverBins(id); len(left) > 0 {
		return fmt.Errorf("err.tool.still_detected|%s", left[0])
	}
	if cmdErr != nil && removed == 0 {
		if recipe.UninstallCmd == "" && a.toolByID(id).BinPath == "" {
			onLog(errCursorNoBin.Error())
		}
		return cmdErr
	}
	if cmdErr != nil {
		onLog("tool.log.uninstall_cmd_failed_removed|" + cmdErr.Error())
	}
	return nil
}

func (a *App) leftoverBins(id string) []string {
	o := a.snapshot()
	for _, p := range o.Providers {
		if p.ID() == id {
			spec := p.DetectSpec(o.Home)
			out := providers.FindBins(spec, o.Home)
			// node 入口形态的残留：FindBins 只认 shim 名，扫不到
			// versions\<ver>\node.exe + index.js，这里用 Detect 补上，
			// 否则其他版本目录仍完整可用时卸载会「假成功」。
			if det := providers.Detect(spec, o.Home); det.Source == "node-entry" && det.BinPath != "" {
				out = append(out, det.BinPath)
			}
			return out
		}
	}
	return nil
}

func (a *App) removeLeftoverBins(id string, onLog func(string)) int {
	extra := a.toolByID(id).BinPath
	bins := a.leftoverBins(id)
	if extra != "" {
		seen := false
		for _, b := range bins {
			if filepath.Clean(b) == filepath.Clean(extra) {
				seen = true
				break
			}
		}
		if !seen {
			bins = append(bins, extra)
		}
	}
	n := 0
	for _, bin := range bins {
		if err := os.Remove(bin); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			if onLog != nil {
				onLog("tool.log.remove_residual_failed|" + bin + "|" + err.Error())
			}
			continue
		}
		if onLog != nil {
			onLog("tool.log.residual_removed|" + bin)
		}
		n++
	}
	return n
}

func (a *App) toolByID(id string) discovery.Tool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.tools {
		if t.ID == id {
			return t
		}
	}
	return discovery.Tool{}
}

// runInstallCmd 走注入的 InstallRunner；未注入则用真实 shell runner。
func (a *App) runInstallCmd(ctx context.Context, shell, cmdline string, onLog func(string)) error {
	r := a.snapshot().InstallRunner
	if r == nil {
		r = runShellCommand
	}
	return r(ctx, shell, cmdline, onLog)
}

func expandPurgeDir(dir, home string) string {
	if strings.HasPrefix(dir, "~/") || strings.HasPrefix(dir, `~\`) {
		return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(dir, "~/"), `~\`))
	}
	return dir
}

// purgeConfigDirs 删除配方声明的配置目录。路径不存在视为成功；真正的删除失败向上返回。
func (a *App) purgeConfigDirs(dirs []string, home string) error {
	expanded := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		expanded = append(expanded, expandPurgeDir(dir, home))
	}
	fn := a.snapshot().PurgeDirs
	if fn == nil {
		fn = removeAllExisting
	}
	return fn(expanded)
}

func removeAllExisting(dirs []string) error {
	var first error
	for _, dir := range dirs {
		err := os.RemoveAll(dir)
		if err == nil || os.IsNotExist(err) {
			continue
		}
		if first == nil {
			first = err
		}
	}
	return first
}

// ensureToolBinsOnPATH 把安装脚本常用、但当前进程 PATH 里没有的目录补上，
// 否则紧接着的 DetectAll / LookPath 仍会把刚装好的 CLI 当成未安装。
func ensureToolBinsOnPATH() {
	for _, dir := range toolBinDirs() {
		appendPATHIfMissing(dir)
	}
}

func toolBinDirs() []string {
	if runtime.GOOS == "windows" {
		var dirs []string
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			dirs = append(dirs, filepath.Join(appdata, "npm"))
		}
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			dirs = append(dirs, filepath.Join(local, "cursor-agent"))
		}
		return dirs
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	return []string{filepath.Join(home, ".local", "bin")}
}

func appendPATHIfMissing(dir string) {
	if dir == "" {
		return
	}
	pathEnv := os.Getenv("PATH")
	for _, p := range filepath.SplitList(pathEnv) {
		if samePath(filepath.Clean(p), filepath.Clean(dir)) {
			return
		}
	}
	if pathEnv == "" {
		_ = os.Setenv("PATH", dir)
		return
	}
	_ = os.Setenv("PATH", pathEnv+string(os.PathListSeparator)+dir)
}

func runShellCommand(ctx context.Context, shell, cmdline string, onLog func(string)) error {
	// npm 全局命令依赖本机 Node；缺 npm 时给出明确提示，避免甩一条含糊的 LookPath 错误。
	if strings.Contains(cmdline, "npm ") {
		if _, err := exec.LookPath("npm"); err != nil {
			if onLog != nil {
				onLog("tool.log.npm_missing")
			}
			return err
		}
	}

	var cmd *exec.Cmd
	switch shell {
	case "cmd":
		cmd = exec.CommandContext(ctx, "cmd.exe", "/c", cmdline)
	case "powershell":
		cmd = exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", cmdline)
	case "sh":
		cmd = exec.CommandContext(ctx, "sh", "-c", cmdline)
	default:
		return fmt.Errorf("err.tool.unknown_shell|%s", shell)
	}

	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	executil.HideWindow(cmd)

	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		return err
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			if onLog != nil {
				onLog(sc.Text())
			}
		}
	}()

	err := cmd.Wait()
	_ = pw.Close()
	<-done
	return err
}
