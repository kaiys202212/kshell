// Package requirements 持久化首页「需求列表」：用户录入的需求条目
// （文本 + 优先级 + 标签），可标记已派发到哪个工作区。
//
// 独立成包的原因：与 hub 同理，读写语义（原子写、版本容错）与桌面端绑定层
// 无关，测试在 temp 目录内完成，无需拉起 Wails。
package requirements

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// fileVersion 是落盘文件结构版本，结构变更时递增，旧文件自然失效。
const fileVersion = 1

// Item 是一条需求。
type Item struct {
	// ID 是随机 ID（crypto/rand 16 字节 hex）。
	ID string `json:"id"`
	// Text 是需求描述（去空白后非空才允许保存）。
	Text string `json:"text"`
	// Priority 取值 "high"|"medium"|"low"，非法值归一化为 "medium"。
	Priority string `json:"priority"`
	// Tags 是自由标签，可空。
	Tags []string `json:"tags"`
	// DispatchedTo 是已派发目标工作区路径；空表示未派发。
	DispatchedTo string `json:"dispatchedTo,omitempty"`
	// Ts 是创建时刻的 UnixNano。
	Ts int64 `json:"ts"`
}

// file 是 requirements.json 的落盘结构。
type file struct {
	Version int    `json:"version"`
	Items   []Item `json:"items"`
}

// Store 串行化对需求列表的读写。
type Store struct {
	mu       sync.Mutex
	itemPath string
	items    []Item
}

// New 创建 store；dir 由调用方给出（桌面端传 ~/.kshell，测试传 temp 目录）。
func New(dir string) *Store {
	return &Store{itemPath: filepath.Join(dir, "requirements.json")}
}

// Load 读 requirements.json。文件缺失/损坏/版本不符都按空处理，返回 nil，
// 不让历史文件问题变成启动问题（对齐 hub.Store.Load 的口径）。
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.items = nil
	if data, err := os.ReadFile(s.itemPath); err == nil {
		var f file
		if json.Unmarshal(data, &f) == nil && f.Version == fileVersion {
			s.items = f.Items
		}
	}
	return nil
}

// List 返回条目副本（复制切片，调用方可安全持有）。
func (s *Store) List() []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Item, len(s.items))
	copy(out, s.items)
	return out
}

// Add 新增一条需求：生成 ID 与时间戳，插入列表头（最新在前）。
// text 去空白后非空才允许保存；priority 非法值归一化为 medium。
func (s *Store) Add(text, priority string, tags []string) (Item, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Item{}, errors.New("需求文本不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	it := Item{
		ID:       newID(),
		Text:     text,
		Priority: normalizePriority(priority),
		Tags:     copyTags(tags),
		Ts:       time.Now().UnixNano(),
	}
	s.items = append([]Item{it}, s.items...)
	if err := s.write(); err != nil {
		// 落盘失败时回滚内存，避免内存与磁盘不一致
		s.items = s.items[1:]
		return Item{}, err
	}
	return it, nil
}

// Update 修改既有需求（文本/优先级/标签整体替换）；id 不存在返回错误。
func (s *Store) Update(id, text, priority string, tags []string) (Item, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Item{}, errors.New("需求文本不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.items {
		if s.items[i].ID != id {
			continue
		}
		old := s.items[i]
		s.items[i].Text = text
		s.items[i].Priority = normalizePriority(priority)
		s.items[i].Tags = copyTags(tags)
		if err := s.write(); err != nil {
			s.items[i] = old // 落盘失败回滚
			return Item{}, err
		}
		return s.items[i], nil
	}
	return Item{}, errors.New("需求不存在: " + id)
}

// Delete 删除需求；幂等，id 不存在也返回 nil。
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.items {
		if s.items[i].ID != id {
			continue
		}
		old := append([]Item(nil), s.items...)
		s.items = append(s.items[:i], s.items[i+1:]...)
		if err := s.write(); err != nil {
			s.items = old // 落盘失败回滚
			return err
		}
		return nil
	}
	return nil
}

// MarkDispatched 标记需求已派发到某工作区；id 不存在返回错误。
func (s *Store) MarkDispatched(id, workspace string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.items {
		if s.items[i].ID != id {
			continue
		}
		old := s.items[i].DispatchedTo
		s.items[i].DispatchedTo = workspace
		if err := s.write(); err != nil {
			s.items[i].DispatchedTo = old // 落盘失败回滚
			return err
		}
		return nil
	}
	return errors.New("需求不存在: " + id)
}

// write 原子写：先写同目录唯一临时文件再改名，避免写一半崩溃留下半截 JSON
// （对齐 hub.writeFile）。写回时保持 version=1。调用方持锁。
func (s *Store) write() error {
	data, err := json.Marshal(&file{Version: fileVersion, Items: s.items})
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.itemPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(s.itemPath)+".*.tmp")
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
	return os.Rename(tmpName, s.itemPath)
}

// newID 生成随机 ID：crypto/rand 16 字节 hex。读失败时退化到时间戳
// （正常系统调用不会失败，仅兜底保证 ID 非空）。
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000000")))
	}
	return hex.EncodeToString(b)
}

// normalizePriority 把非法优先级归一化为 medium。
func normalizePriority(p string) string {
	switch p {
	case "high", "low":
		return p
	default:
		return "medium"
	}
}

// copyTags 拷贝标签切片，隔离调用方后续修改；空入参返回 nil 保持 JSON 干净。
func copyTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	out := make([]string, len(tags))
	copy(out, tags)
	return out
}
