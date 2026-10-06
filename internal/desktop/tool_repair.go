package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

// maxAutoRepairAttempts 是残缺安装的自动修复重试上限：连续失败到顶后
// 停止自动尝试，UI 降级为「已损坏」+ 手动修复；工具恢复健康时清零。
const maxAutoRepairAttempts = 3

// repairStatePath 返回状态文件完整路径；Layout.State 未装配时返回空串，
// 调用方据空串静默跳过（测试的零值 Layout 与无状态目录场景）。
// 直接读 a.opts（装配后只读，app.go 注释约定），不走 snapshot：本函数会被
// execInstallJob 持锁路径调用，再拿 mu 会死锁。
func (a *App) repairStatePath(name string) string {
	root := a.opts.Layout.State
	if root == "" {
		return ""
	}
	return filepath.Join(root, name)
}

// setUninstalledMarker 记录「kshell 自己卸载过该工具」：残缺自动修复据此排除，
// 避免把用户刚卸的又装回来。kshell 之外的卸载无法感知，宁可误修复也不放过。
func (a *App) setUninstalledMarker(id string) {
	if p := a.repairStatePath(id + ".uninstalled"); p != "" {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, nil, 0o644) // 内容无语义，存在即标记
	}
}

func (a *App) clearUninstalledMarker(id string) {
	if p := a.repairStatePath(id + ".uninstalled"); p != "" {
		_ = os.Remove(p)
	}
}

func (a *App) uninstalledByUser(id string) bool {
	p := a.repairStatePath(id + ".uninstalled")
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// bumpRepairAttempts 自动修复启动前记账。损坏 JSON 视为 0 重新累计，
// 状态文件问题绝不能阻塞修复本身。
func (a *App) bumpRepairAttempts(id string) {
	p := a.repairStatePath(id + "-repair.json")
	if p == "" {
		return
	}
	n := decodeAttempts(p) + 1
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	data, _ := json.Marshal(map[string]int{"attempts": n})
	_ = os.WriteFile(p, data, 0o644)
}

func (a *App) readRepairAttempts(id string) int {
	p := a.repairStatePath(id + "-repair.json")
	if p == "" {
		return 0
	}
	return decodeAttempts(p)
}

// clearRepairState 清重试计数：安装成功或工具恢复健康时调用。
func (a *App) clearRepairState(id string) {
	if p := a.repairStatePath(id + "-repair.json"); p != "" {
		_ = os.Remove(p)
	}
}

func decodeAttempts(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var rec struct {
		Attempts int `json:"attempts"`
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		return 0 // 损坏文件视为 0
	}
	if rec.Attempts < 0 {
		return 0
	}
	return rec.Attempts
}

// procScanUnderDir 解析进程占用检查器：注入优先，否则走平台默认实现。
func (a *App) procScanUnderDir(dir string) bool {
	if fn := a.snapshot().ProcScan; fn != nil {
		return fn(dir)
	}
	return runningProcessUnderDir(dir)
}

// maybeAutoRepair 对残缺安装执行自动修复门控（详见设计文档）：
// 残缺 + 无 kshell 卸载标记 + 有使用残留 + 无进程占用 + 无进行中任务 + 重试未到上限，
// 全部满足才复用 startInstallJob 跑官方安装配方。进程占用只跳过本轮（扫描周期即退避），
// 不计重试；其余不满足直接跳过该工具。布局零值（测试）下全部静默跳过。
func (a *App) maybeAutoRepair(tools []discovery.Tool) {
	for _, t := range tools {
		if !t.Installed || !t.Broken {
			continue
		}
		if a.uninstalledByUser(t.ID) {
			continue
		}
		o := a.snapshot()
		var spec providers.DetectSpec
		for _, p := range o.Providers {
			if p.ID() == t.ID {
				spec = p.DetectSpec(o.Home)
				break
			}
		}
		if spec.BinName == "" || !providers.HasResidue(spec, o.Home) {
			continue
		}
		a.mu.Lock()
		busy := a.installJob.running
		a.mu.Unlock()
		if busy {
			return // 已有任务在跑，本轮对所有工具都先不动
		}
		if a.readRepairAttempts(t.ID) >= maxAutoRepairAttempts {
			continue
		}
		if a.installDirBusy(spec, o.Home) {
			continue
		}
		_, r, err := a.lookupRecipe(t.ID)
		if err != nil {
			continue // 无安装配方（自定义 provider）无法自动修
		}
		if err := a.startInstallJob(t.ID, "install", r, false, "auto"); err != nil {
			continue
		}
		a.bumpRepairAttempts(t.ID)
		a.Emit("tool:install:log", map[string]any{
			"toolID": t.ID,
			"text":   "检测到安装损坏，正在自动修复…",
		})
		return // 一个任务独占，启动成功后本轮结束
	}
}

// installDirBusy 检查工具安装目录下是否有运行中进程：取 InstallDirs 中首个存在的
// 目录（cursor 即安装根，覆盖 versions\ 子树）。全部不存在时视为不忙。
func (a *App) installDirBusy(spec providers.DetectSpec, home string) bool {
	for _, dir := range spec.InstallDirs {
		expanded := expandHomeForRepair(dir, home)
		if isDirExists(expanded) {
			return a.procScanUnderDir(expanded)
		}
	}
	return false
}

// expandHomeForRepair 展开 ~ 前缀；%VAR% 走 os.ExpandEnv。
func expandHomeForRepair(path, home string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		return filepath.Join(home, path[2:])
	}
	return os.ExpandEnv(path)
}

func isDirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
