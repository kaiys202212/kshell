package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/yangk/kshell/internal/providers"
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
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	writeStateJSONAtomic(path, append(raw, '\n'))
}

// writeStateJSONAtomic 原子写任意 JSON 载荷；失败静默（记忆类状态是辅助功能）。
func writeStateJSONAtomic(path string, raw []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
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

// opencodeLastModelStateFile：全局「上次 opencode 模型」记忆。
// 与按会话记忆分开的原因：新建会话在扫描绑定前拿不到磁盘会话 ID，
// 按会话记录会静默丢失；全局键保证同一工具的下一次新建总能沿用。
const opencodeLastModelStateFile = "opencode-last-model.json"

// openCodeLastModelState 是全局记忆的落盘形态（原生 OpenCode 模型 ID）。
type openCodeLastModelState struct {
	ModelID string `json:"modelId"`
}

func (a *App) openCodeLastModelStatePath() string {
	root := a.opts.Layout.State
	if root == "" {
		return ""
	}
	return filepath.Join(root, opencodeLastModelStateFile)
}

// recordOpenCodeLastModel 记录上一次在 opencode 会话里成功使用的模型；
// 空值忽略，覆盖旧值。写失败静默。
func (a *App) recordOpenCodeLastModel(modelID string) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return
	}
	path := a.openCodeLastModelStatePath()
	if path == "" {
		return
	}
	raw, err := json.MarshalIndent(openCodeLastModelState{ModelID: modelID}, "", "  ")
	if err != nil {
		return
	}
	writeStateJSONAtomic(path, append(raw, '\n'))
}

// lastOpenCodeModel 返回新建 opencode 会话应沿用的模型（原生 ID）：
// 全局「上次模型」优先；老版本没有该文件时回落到扫描结果里最近一条
// opencode 会话的会话级记忆；都没有返回空串（保持 agent 默认）。
func (a *App) lastOpenCodeModel() string {
	if m := a.lookupOpenCodeLastModel(); m != "" {
		return m
	}
	a.mu.Lock()
	res := a.result // Result 整体替换、替换后只读；取指针快照即可
	a.mu.Unlock()
	if res == nil {
		return ""
	}
	var latest providers.Session
	found := false
	for _, s := range res.Sessions {
		if s.ToolID != "opencode" || s.ID == "" {
			continue
		}
		if !found || s.UpdatedAt.After(latest.UpdatedAt) {
			latest = s
			found = true
		}
	}
	if !found {
		return ""
	}
	return a.lookupOpenCodeSessionModel(latest.ID)
}

// lookupOpenCodeLastModel 读取全局上次模型；缺失/损坏返回空串。
func (a *App) lookupOpenCodeLastModel() string {
	path := a.openCodeLastModelStatePath()
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var st openCodeLastModelState
	if json.Unmarshal(b, &st) != nil {
		return ""
	}
	return strings.TrimSpace(st.ModelID)
}
