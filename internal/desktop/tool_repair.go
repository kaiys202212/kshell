package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// maxAutoRepairAttempts 是残缺安装的自动修复重试上限：连续失败到顶后
// 停止自动尝试，UI 降级为「已损坏」+ 手动修复；工具恢复健康时清零。
const maxAutoRepairAttempts = 3

// repairStatePath 返回状态文件完整路径；Layout.State 未装配时返回空串，
// 调用方据空串静默跳过（测试的零值 Layout 与无状态目录场景）。
func (a *App) repairStatePath(name string) string {
	root := a.snapshot().Layout.State
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
