package discovery

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/yangk/kshell/internal/providers"
)

// snapshotVersion 是快照文件结构版本，结构变更时递增，旧文件自然失效。
const snapshotVersion = 1

// Snapshot 是上一次扫描结果的落盘快照。桌面端启动时先端出它，让工作区/会话列表
// 立即有内容（跳过目录遍历、文件解析与 git 扫描），随后后台扫描完成再覆盖。
type Snapshot struct {
	Version    int                 `json:"version"`
	SavedAt    time.Time           `json:"savedAt"`
	Sessions   []providers.Session `json:"sessions"`
	Workspaces []Workspace         `json:"workspaces"`
	Tools      []Tool              `json:"tools"`
}

// SaveSnapshot 原子写快照：先写同目录唯一临时文件再改名，避免写一半崩溃留下半截 JSON。
// path 为空或 res 为 nil 时什么都不做，调用方无需判断。
func SaveSnapshot(path string, res *Result, tools []Tool) error {
	if path == "" || res == nil {
		return nil
	}
	snap := Snapshot{
		Version:    snapshotVersion,
		SavedAt:    time.Now(),
		Sessions:   res.Sessions,
		Workspaces: res.Workspaces,
		Tools:      tools,
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // rename 成功后残留清理是空操作
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// LoadSnapshot 读取快照。文件不存在、损坏或版本不符都返回 error，调用方退回实时扫描
// 即可——缓存问题不该变成启动问题。返回的切片保证非 nil（避免前端拿到 null）。
func LoadSnapshot(path string) (*Result, []Tool, error) {
	if path == "" {
		return nil, nil, errors.New("快照路径为空")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, nil, err
	}
	if snap.Version != snapshotVersion {
		return nil, nil, errors.New("快照版本不匹配，忽略旧缓存")
	}

	res := &Result{Sessions: snap.Sessions, Workspaces: snap.Workspaces}
	if res.Sessions == nil {
		res.Sessions = []providers.Session{}
	}
	if res.Workspaces == nil {
		res.Workspaces = []Workspace{}
	}
	for i := range res.Workspaces {
		if res.Workspaces[i].ToolCounts == nil {
			res.Workspaces[i].ToolCounts = map[string]int{}
		}
	}
	tools := snap.Tools
	if tools == nil {
		tools = []Tool{}
	}
	return res, tools, nil
}
