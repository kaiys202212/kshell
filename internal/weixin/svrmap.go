package weixin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// svrAttr 是一条出站消息 ID 对应的会话归因（内存 + 落盘共用）。
type svrAttr struct {
	ChatID    string `json:"chat_id"`
	SessionID string `json:"session_id,omitempty"`
	Title     string `json:"title,omitempty"`
	Ts        int64  `json:"ts"`
}

// svrMapFile 是 ~/.kshell/weixin-svr-map.json 的根结构：按 wxid 分片，避免换绑串号。
type svrMapFile struct {
	Version  int                          `json:"version"`
	Accounts map[string][]svrMapFileEntry `json:"accounts"`
}

// svrMapFileEntry 是落盘的一条映射（含 svr_id）。
type svrMapFileEntry struct {
	SvrID     string `json:"svr_id"`
	ChatID    string `json:"chat_id"`
	SessionID string `json:"session_id,omitempty"`
	Title     string `json:"title,omitempty"`
	Ts        int64  `json:"ts"`
}

const (
	svrMapFileVersion = 1
	// svrMapPersistCap 是单账号落盘/内存上限（热数据；超限丢最旧）。
	svrMapPersistCap = 1024
	// svrMapTTL 是落盘条目保留时长；加载时剔除过期。
	svrMapTTL = 30 * 24 * time.Hour
)

// defaultSvrMapPath 返回默认落盘路径 ~/.kshell/weixin-svr-map.json。
func defaultSvrMapPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "kshell-weixin-svr-map.json")
	}
	return filepath.Join(home, ".kshell", "weixin-svr-map.json")
}

// loadSvrMapAccount 从 path 加载指定 wxid 的映射。文件缺失返回空表；
// 损坏则改名为 .bak 后返回空表（不阻断启动）。wxid 空时返回空表不读盘。
func loadSvrMapAccount(path, wxid string) (map[string]svrAttr, []string) {
	out := map[string]svrAttr{}
	var order []string
	if path == "" || wxid == "" {
		return out, order
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return out, order
	}
	var f svrMapFile
	if err := json.Unmarshal(data, &f); err != nil {
		backupCorruptSvrMap(path)
		return out, order
	}
	if f.Accounts == nil {
		return out, order
	}
	cutoff := time.Now().Add(-svrMapTTL).Unix()
	for _, e := range f.Accounts[wxid] {
		if e.SvrID == "" || e.ChatID == "" {
			continue
		}
		if e.Ts > 0 && e.Ts < cutoff {
			continue
		}
		if _, dup := out[e.SvrID]; dup {
			continue
		}
		out[e.SvrID] = svrAttr{ChatID: e.ChatID, SessionID: e.SessionID, Title: e.Title, Ts: e.Ts}
		order = append(order, e.SvrID)
	}
	// 超限只保留最新尾部（文件内顺序为旧→新）。
	if len(order) > svrMapPersistCap {
		drop := len(order) - svrMapPersistCap
		for _, id := range order[:drop] {
			delete(out, id)
		}
		order = order[drop:]
	}
	return out, order
}

// saveSvrMapAccount 把某 wxid 的内存映射写回 path（读写-改-写回整文件）。
// 失败返回 error，调用方只打日志、不挡出站。wxid 空则跳过。
func saveSvrMapAccount(path, wxid string, m map[string]svrAttr, order []string) error {
	if path == "" || wxid == "" {
		return nil
	}
	f := svrMapFile{Version: svrMapFileVersion, Accounts: map[string][]svrMapFileEntry{}}
	if data, err := os.ReadFile(path); err == nil {
		var existing svrMapFile
		if json.Unmarshal(data, &existing) == nil && existing.Accounts != nil {
			f.Accounts = existing.Accounts
		}
	}
	entries := make([]svrMapFileEntry, 0, len(order))
	for _, id := range order {
		a, ok := m[id]
		if !ok || a.ChatID == "" {
			continue
		}
		entries = append(entries, svrMapFileEntry{
			SvrID: id, ChatID: a.ChatID, SessionID: a.SessionID, Title: a.Title, Ts: a.Ts,
		})
	}
	f.Accounts[wxid] = entries
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// backupCorruptSvrMap 把损坏文件改名为 .bak（已存在则带时间戳），失败静默。
func backupCorruptSvrMap(path string) {
	bak := path + ".bak"
	if _, err := os.Stat(bak); err == nil {
		bak = path + "." + time.Now().Format("20060102150405") + ".bak"
	}
	_ = os.Rename(path, bak)
}
