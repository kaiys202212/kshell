package agentapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// memoryCap 是内存与文件保留的条目上限：超过时整体重写文件截断最旧条目。
const memoryCap = 500

// searchLimit 是 Search 返回的条数上限。
const searchLimit = 20

// MemoryEntry 是管理者记忆 journal 的一条记录（jsonl 逐行存储）。
type MemoryEntry struct {
	Ts      int64  `json:"ts"`                // UnixNano，零值时写入时补当前时刻
	Channel string `json:"channel"`           // hub | wechat
	Input   string `json:"input"`             // 用户输入
	Outcome string `json:"outcome"`           // 最终结论
	Actions string `json:"actions,omitempty"` // 关键工具动作，单行摘要
}

// ManagerMemory 是管理者循环的长期记忆 journal：append-only jsonl 落盘，
// 目录由调用方注入（desktop 传 ~/.kshell/hub/agent，测试传临时目录）。
// 文件缺失/损坏按空处理（对齐 hub.Store.Load 口径），不让历史文件问题
// 变成管理者循环的启动问题。
type ManagerMemory struct {
	mu      sync.Mutex
	path    string
	entries []MemoryEntry // 内存副本，旧→新
}

// NewManagerMemory 打开指定目录下的记忆 journal 并加载已有条目。
// 加载逐行解析，坏行跳过（单行损坏不应丢掉其余记忆）；坏行会在下次
// cap 触发的整体重写时自然清除。
func NewManagerMemory(dir string) *ManagerMemory {
	m := &ManagerMemory{path: filepath.Join(dir, "memory.jsonl")}
	m.load()
	return m
}

// load 读入 memory.jsonl，缺失/损坏按空处理，坏行跳过。
func (m *ManagerMemory) load() {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e MemoryEntry
		if json.Unmarshal([]byte(line), &e) != nil {
			continue // 坏行跳过
		}
		m.entries = append(m.entries, e)
	}
}

// Append 追加一条记忆并落盘（jsonl 逐行追加）；超过 memoryCap 时截断
// 最旧溢出部分并整体重写文件（同目录唯一 tmp + rename 原子替换，对齐
// hub.Store.writeFile 范式）。写失败返回错误，调用方决定只记日志不 panic。
func (m *ManagerMemory) Append(e MemoryEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if e.Ts == 0 {
		e.Ts = time.Now().UnixNano()
	}
	m.entries = append(m.entries, e)

	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	// 未超 cap 走追加路径，避免每次写入都全量重写
	if len(m.entries) <= memoryCap {
		return m.appendLine(e)
	}

	// 截断最旧溢出部分，整体重写（同时自然清掉历史坏行）
	overflow := len(m.entries) - memoryCap
	m.entries = append([]MemoryEntry(nil), m.entries[overflow:]...)
	return m.rewriteFile(dir)
}

// appendLine 以追加模式写一行 JSON。
func (m *ManagerMemory) appendLine(e MemoryEntry) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.OpenFile(m.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// rewriteFile 原子重写整个 journal：先写同目录唯一临时文件再改名，
// 避免写一半崩溃留下半截文件。
func (m *ManagerMemory) rewriteFile(dir string) error {
	var buf strings.Builder
	for _, e := range m.entries {
		data, err := json.Marshal(e)
		if err != nil {
			return err
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(m.path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // rename 成功后残留清理是空操作
	if _, err := tmp.WriteString(buf.String()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, m.path)
}

// Recent 返回最近 n 条记忆（新在前）；n<=0 返回空。返回副本，调用方可安全持有。
func (m *ManagerMemory) Recent(n int) []MemoryEntry {
	m.mu.Lock()
	defer m.mu.Unlock()

	if n <= 0 || len(m.entries) == 0 {
		return nil
	}
	if n > len(m.entries) {
		n = len(m.entries)
	}
	out := make([]MemoryEntry, n)
	for i := 0; i < n; i++ {
		out[i] = m.entries[len(m.entries)-1-i]
	}
	return out
}

// Search 对 Input/Outcome/Actions 三字段做大小写不敏感子串搜索，任一命中
// 即算；结果新在前，上限 searchLimit 条；keyword trim 后为空返回空。
func (m *ManagerMemory) Search(keyword string) []MemoryEntry {
	keyword = strings.TrimSpace(strings.ToLower(keyword))
	if keyword == "" {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var out []MemoryEntry
	for i := len(m.entries) - 1; i >= 0 && len(out) < searchLimit; i-- {
		e := m.entries[i]
		if strings.Contains(strings.ToLower(e.Input), keyword) ||
			strings.Contains(strings.ToLower(e.Outcome), keyword) ||
			strings.Contains(strings.ToLower(e.Actions), keyword) {
			out = append(out, e)
		}
	}
	return out
}
