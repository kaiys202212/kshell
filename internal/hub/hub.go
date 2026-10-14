// Package hub 持久化 AgentHub（首页跨项目 agent 汇总窗）的消息历史。
//
// 消息分两类（Kind）：用户发出的（user）与 agent 回复（agent）。历史落盘到
// <dir>/live.json（最近 liveCap 条），溢出的最旧部分按自然月滚动到
// <dir>/archive/YYYY-MM.json（只增不删，用户可手动清理）。
//
// 独立成包的原因：读写语义（原子写、版本、按自然月归档、ID 水位恢复）与桌面端
// 绑定层无关，测试可在 temp 目录内完成，无需拉起 Wails。
package hub

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// fileVersion 是落盘文件结构版本，结构变更时递增，旧文件自然失效。
const fileVersion = 1

// defaultLiveCap 是 live 文件保留的消息条数上限（超出按月归档）。
const defaultLiveCap = 500

// Message 是一条 AgentHub 消息，两类共用。
type Message struct {
	// ID 是全局自增序号：归档后仍单调，供前端稳定排序与去重。
	ID int64 `json:"id"`
	// Kind 取值 "user"（用户发出）或 "agent"（agent 回复）。
	Kind string `json:"kind"`
	// Tool 是 agent 回复的工具 ID；用户消息为投递目标工具 ID（可空）。
	Tool string `json:"tool,omitempty"`
	// ChatID 是关联的 chat.ID（用户消息的目标 / agent 回复的来源），归因用。
	ChatID string `json:"chatId,omitempty"`
	// TermKey 是 agent 回复的终端/聊天 key；用户消息为空。
	TermKey string `json:"termKey,omitempty"`
	// Workspace 是消息所属工作区路径。
	Workspace string `json:"workspace,omitempty"`
	// Text 是正文：用户输入，或 agent 回复的 summary。
	Text string `json:"text"`
	// Event 是 agent 回复的事件语义（Stop/Notification/error...）；用户消息为空。
	Event string `json:"event,omitempty"`
	// Ts 是消息时刻的 UnixNano。
	Ts int64 `json:"ts"`
}

// file 是 live.json / archive/YYYY-MM.json 的落盘结构。
type file struct {
	Version  int       `json:"version"`
	Month    string    `json:"month,omitempty"` // 仅归档文件使用
	Messages []Message `json:"messages"`
}

// ArchiveMonth 是一个归档月份及其消息条数（供 UI 的归档入口列表）。
type ArchiveMonth struct {
	Month string `json:"month"` // "YYYY-MM"
	Count int    `json:"count"`
}

// Store 串行化对 hub 目录的读写。
type Store struct {
	mu       sync.Mutex
	dir      string
	livePath string
	archDir  string
	liveCap  int
	live     []Message
	nextID   int64
	loaded   bool
}

// New 创建 store；cap<=0 时用默认上限。dir 由调用方给出（桌面端传 ~/.kshell/hub，
// 测试传 temp 目录）。
func New(dir string, liveCap int) *Store {
	if liveCap <= 0 {
		liveCap = defaultLiveCap
	}
	return &Store{
		dir:      dir,
		livePath: filepath.Join(dir, "live.json"),
		archDir:  filepath.Join(dir, "archive"),
		liveCap:  liveCap,
	}
}

// Load 读 live.json 与归档最大 ID，恢复 ID 水位。文件缺失/损坏都按空处理，
// 不让历史文件问题变成启动问题（对齐 discovery.LoadSnapshot 的口径）。
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.live = nil
	s.nextID = 0
	s.loaded = true

	if data, err := os.ReadFile(s.livePath); err == nil {
		var f file
		if json.Unmarshal(data, &f) == nil && f.Version == fileVersion {
			s.live = f.Messages
		}
	}
	for _, m := range s.live {
		if m.ID > s.nextID {
			s.nextID = m.ID
		}
	}
	// 归档里的 ID 可能大于 live（live 只有最近一批），需一并抬高水位，
	// 否则重启后新消息 ID 会与归档里的旧 ID 撞车。
	if maxArch := s.maxArchiveID(); maxArch > s.nextID {
		s.nextID = maxArch
	}
	return nil
}

// List 返回 live 消息副本（复制切片，调用方可安全持有）。
func (s *Store) List() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Message, len(s.live))
	copy(out, s.live)
	return out
}

// Append 追加一条消息：分配 ID 与时间戳，超过 liveCap 时把最旧的溢出部分按月归档。
// 归档先落盘成功再写 live，中途崩溃最坏是重复一条（可接受），不会丢。
func (s *Store) Append(m Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	m.ID = s.nextID + 1
	if m.Ts == 0 {
		m.Ts = time.Now().UnixNano()
	}
	s.nextID = m.ID
	s.live = append(s.live, m)

	if len(s.live) > s.liveCap {
		overflow := len(s.live) - s.liveCap
		old := s.live[:overflow]
		if err := s.archive(old); err != nil {
			// 归档失败：放弃本次溢出（留在 live 里继续超限），保消息不丢；
			// 下次 Append 再试。不把错误吞掉，交给调用方决定是否记日志。
			return err
		}
		s.live = s.live[overflow:]
	}
	return s.writeLive()
}

// Clear 清空 live 消息（归档保留，符合「只增不删」）。
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.live) == 0 {
		return nil
	}
	s.live = nil
	return s.writeLive()
}

// ListArchive 列出全部归档月份与条数，按月份倒序（新在前）。
func (s *Store) ListArchive() []ArchiveMonth {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []ArchiveMonth{}
	entries, err := os.ReadDir(s.archDir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		month := strings.TrimSuffix(e.Name(), ".json")
		if !validMonth(month) {
			continue
		}
		f, err := s.readArchiveFile(month)
		if err != nil {
			continue
		}
		out = append(out, ArchiveMonth{Month: month, Count: len(f.Messages)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Month > out[j].Month })
	return out
}

// LoadArchive 读某月归档全量；月份缺失返回空切片（非错误），月份格式非法返回错误。
func (s *Store) LoadArchive(month string) ([]Message, error) {
	if !validMonth(month) {
		return nil, fmt.Errorf("非法归档月份: %q", month)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.readArchiveFile(month)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Message{}, nil
		}
		// 损坏文件同样按空处理，避免单月损坏拖垮入口列表
		return []Message{}, nil
	}
	out := make([]Message, len(f.Messages))
	copy(out, f.Messages)
	return out, nil
}

// archive 把 overflow 按自然月追加到对应归档文件（调用方持锁）。
func (s *Store) archive(msgs []Message) error {
	if len(msgs) == 0 {
		return nil
	}
	byMonth := map[string][]Message{}
	order := []string{}
	for _, m := range msgs {
		month := monthOf(m.Ts)
		if _, ok := byMonth[month]; !ok {
			order = append(order, month)
		}
		byMonth[month] = append(byMonth[month], m)
	}
	if err := os.MkdirAll(s.archDir, 0o700); err != nil {
		return err
	}
	for _, month := range order {
		existing, err := s.readArchiveFile(month)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			existing = &file{Version: fileVersion, Month: month}
		}
		if existing == nil {
			existing = &file{Version: fileVersion, Month: month}
		}
		existing.Version = fileVersion
		existing.Month = month
		existing.Messages = append(existing.Messages, byMonth[month]...)
		if err := s.writeFile(s.archivePath(month), existing); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) maxArchiveID() int64 {
	entries, err := os.ReadDir(s.archDir)
	if err != nil {
		return 0
	}
	var maxID int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		f, err := s.readArchiveFile(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil {
			continue
		}
		for _, m := range f.Messages {
			if m.ID > maxID {
				maxID = m.ID
			}
		}
	}
	return maxID
}

func (s *Store) archivePath(month string) string {
	return filepath.Join(s.archDir, month+".json")
}

// readArchiveFile 读归档文件；不存在返回 os.ErrNotExist，损坏返回解析错误。
func (s *Store) readArchiveFile(month string) (*file, error) {
	data, err := os.ReadFile(s.archivePath(month))
	if err != nil {
		return nil, err
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

func (s *Store) writeLive() error {
	return s.writeFile(s.livePath, &file{Version: fileVersion, Messages: s.live})
}

// writeFile 原子写：先写同目录唯一临时文件再改名，避免写一半崩溃留下半截 JSON
// （对齐 discovery.SaveSnapshot）。
func (s *Store) writeFile(path string, f *file) error {
	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
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

// monthOf 把 UnixNano 归到自然月 "YYYY-MM"（UTC：消息时间只做归档分桶，
// 不用于展示本地时间，避免时区把同一批消息拆到两个月）。
func monthOf(ts int64) string {
	return time.Unix(0, ts).UTC().Format("2006-01")
}

// validMonth 校验 "YYYY-MM"。
func validMonth(m string) bool {
	if len(m) != 7 || m[4] != '-' {
		return false
	}
	for i, c := range m {
		if i == 4 {
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	mo := (m[5]-'0')*10 + (m[6] - '0')
	return mo >= 1 && mo <= 12
}
