package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// opencode-session-models.json：OpenCode 会话级模型记忆。
// OpenCode 的 SQLite 会话不记录上次模型（resume 后 Current 恒为 agent 默认，
// 与 Claude 的 JSONL 尾部可解析不同），kshell 侧在 SetChatModel 成功时记下
// 「磁盘会话 ID → 下发的原生模型 ID」，恢复历史会话时查回并 SetModel。
const opencodeSessionModelStateFile = "opencode-session-models.json"

// openCodeSessionModelStatePath 返回 state 文件完整路径；Layout.State 未装配
// 时返回空串，调用方据空串静默跳过（测试的零值 Layout 与无状态目录场景）。
// 只读 a.opts（装配后只读），与 tool_repair.go 同口径。
func (a *App) openCodeSessionModelStatePath() string {
	root := a.opts.Layout.State
	if root == "" {
		return ""
	}
	return filepath.Join(root, opencodeSessionModelStateFile)
}

// recordOpenCodeSessionModel 记录一条会话→模型；覆盖旧值（同会话切两次用最后一次）。
// 辅助功能：写失败静默，绝不影响 SetChatModel 本身。
func (a *App) recordOpenCodeSessionModel(sessionID, modelID string) {
	path := a.openCodeSessionModelStatePath()
	if path == "" || sessionID == "" || modelID == "" {
		return
	}
	state := readOpenCodeModelState(path)
	state[sessionID] = modelID
	writeOpencodeModelState(path, state)
}

// lookupOpenCodeSessionModel 查该会话记忆的模型；未记过或文件不可读返回空串。
func (a *App) lookupOpenCodeSessionModel(sessionID string) string {
	path := a.openCodeSessionModelStatePath()
	if path == "" || sessionID == "" {
		return ""
	}
	return readOpenCodeModelState(path)[sessionID]
}

func readOpenCodeModelState(path string) map[string]string {
	b, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	var m map[string]string
	if json.Unmarshal(b, &m) != nil {
		return map[string]string{}
	}
	return m
}

// writeOpencodeModelState 原子写 state JSON（temp+rename，与 SaveProvidersYAML 同口径）。
func writeOpencodeModelState(path string, state map[string]string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
	}
}
