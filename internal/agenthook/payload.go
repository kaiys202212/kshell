package agenthook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Payload 是落到通知收件箱的事件结构，桌面端（后续任务）按同构 JSON 读取。
type Payload struct {
	Tool      string `json:"tool"`
	Event     string `json:"event"`
	TermKey   string `json:"termKey"`
	Workspace string `json:"workspace"`
	Summary   string `json:"summary"`
	Raw       string `json:"raw"`
	Ts        int64  `json:"ts"`
}

// inboxDir 返回通知收件箱目录：~/.kshell/notify/inbox。
func inboxDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".kshell", "notify", "inbox"), nil
}

// writeInbox 把事件原子落盘到收件箱：先写同目录临时文件再 rename，
// 避免桌面端读到半截 JSON。ts 用写入时刻的 UnixNano，同时充当文件名前缀
// （<unixnano>-<pid>.json）保证唯一与天然按时间排序。
func writeInbox(p Payload) error {
	dir, err := inboxDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	p.Ts = time.Now().UnixNano()
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	final := filepath.Join(dir, fmt.Sprintf("%d-%d.json", p.Ts, os.Getpid()))
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

// CleanupInbox 删除收件箱中修改时间早于 maxAge 的 *.json；目录不存在时静默返回。
// 桌面端启动时调用，防止长期使用下收件箱无限堆积。
func CleanupInbox(maxAge time.Duration) {
	dir, err := inboxDir()
	if err != nil {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			// 单个删除失败不值得处理：下一轮启动还会再试
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
