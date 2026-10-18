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

// fileVersion 是落盘文件结构版本，结构变更时递增。
// v2：Item 新增 project 字段。Load 兼容读取更低版本（见 Load），只拒绝更高版本。
// v3：Item 新增 sessionId 字段（磁盘会话 ID，跨进程稳定）；chat ID 含 bootID
// 仅进程内有效，桌面端重启后需求卡片靠 sessionId 重新找到活跃会话。
const fileVersion = 3

// ErrProjectRequired 是 project 缺失的稳定 wire key（前端 translateBackend 翻译），
// 文案本身不面向用户，勿改成自然语言。
var ErrProjectRequired = errors.New("err.requirements.project_required")

// ErrTextRequired 是需求文本缺失的稳定 wire key（前端 translateBackend 翻译），
// 文案本身不面向用户，勿改成自然语言。
var ErrTextRequired = errors.New("err.requirements.text_required")

// 需求状态机取值：未开始 → 执行中 → 已完成 → 已归档。
// 迁移约束见 SetStatus / Archive / Update / Delete 各方法的守卫。
const (
	StatusPending   = "pending"
	StatusActive    = "active"
	StatusDone      = "done"
	StatusArchived  = "archived"
)

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
	// Project 是需求所属项目（工作区路径）。v2 起新增必填；存量数据由
	// 迁移（SetWorkspaceResolver + List 惰性触发）从 DispatchedTo 回填。
	// 已知边界：迁移时 DispatchedTo 匹配不到工作区列表（如目标目录已删除）
	// 的条目将永久留空本字段，需用户编辑需求时重选项目。
	Project string `json:"project,omitempty"`
	// DispatchedTo 是已派发目标工作区路径；空表示未派发。
	DispatchedTo string `json:"dispatchedTo,omitempty"`
	// ChatID 是派发时创建的会话 ID；空表示未派发或无关联会话。
	// 注意 chat ID 含 bootID（进程内唯一），桌面端重启后不再有效；
	// 跨进程恢复靠下面的 SessionID。
	ChatID string `json:"chatId,omitempty"`
	// SessionID 是关联的磁盘会话 ID（agent 工具自身的会话记录 ID），跨进程
	// 稳定。派发时随 ChatID 一并落盘；桌面端重启后 ChatID 失效，desktop 层
	// 按 SessionID 反查活跃会话恢复绑定。旧数据无此字段（零值），由
	// ListRequirements 惰性回填。
	SessionID string `json:"sessionId,omitempty"`
	// SessionTitle 是关联会话标题的快照：会话关闭/重启后无法反查标题，
	// 需求列表仍能展示会话名（实时标题由前端活跃会话镜像覆盖）。
	SessionTitle string `json:"sessionTitle,omitempty"`
	// Status 是需求状态（pending/active/done/archived）。
	// 旧数据零值 "" 在 Load 时归一化为 pending，落盘后不再出现 ""。
	Status string `json:"status"`
	// ArchivedAt 是归档时刻的 UnixNano；未归档为 0。
	ArchivedAt int64 `json:"archivedAt,omitempty"`
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
	// workspaceResolver 返回当前已知工作区路径列表，供存量迁移把 DispatchedTo
	// 回填为 Project；返回空列表表示扫描未就绪（迁移推迟到下次触发）。
	workspaceResolver func() []string
	// migrated 标记迁移已执行过（resolver 就绪并跑完一次后置位）。
	migrated bool
}

// New 创建 store；dir 由调用方给出（桌面端传 ~/.kshell，测试传 temp 目录）。
func New(dir string) *Store {
	return &Store{itemPath: filepath.Join(dir, "requirements.json")}
}

// Load 读 requirements.json。文件缺失/损坏按空处理，返回 nil。
// 版本校验只拒绝**更高**版本（未来结构不认识），更低版本兼容读取——
// v1 → v2 仅新增 project 字段，旧文件直接可解析，迁移负责回填。
// 不让历史文件问题变成启动问题（对齐 hub.Store.Load 的口径）。
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.items = nil
	if data, err := os.ReadFile(s.itemPath); err == nil {
		var f file
		if json.Unmarshal(data, &f) == nil && f.Version <= fileVersion {
			// 旧数据没有 status 字段（零值 ""），归一化为 pending，
			// 避免后续状态机判断遇到未知空值。
			for i := range f.Items {
				f.Items[i].Status = normalizeStatus(f.Items[i].Status)
				// 存量数据 tags 可能为 null（旧版 copyTags 空入参返回 nil），归一化为空切片
				if f.Items[i].Tags == nil {
					f.Items[i].Tags = []string{}
				}
			}
			s.items = f.Items
		}
	}
	return nil
}

// SetWorkspaceResolver 注入工作区路径解析函数，供存量迁移把 DispatchedTo
// 回填为 Project；注入后由 List 惰性触发迁移。
//
// 锁序契约：ensureMigrated 在持有 Store 内部锁（s.mu）时调用回调，因此——
//   - 回调内不得再调用本 Store 的任何方法（会重入死锁）；
//   - 调用方不得在持有会锁到本 Store 的锁时触发 List（同样死锁）。
//
// 注入时机：仅迁移完成标记置位前有效（见 ensureMigrated），migrated 置位后
// 替换 resolver 不再有任何效果。
func (s *Store) SetWorkspaceResolver(fn func() []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workspaceResolver = fn
}

// List 返回条目副本（复制切片，调用方可安全持有）。
// 返回前先尝试存量迁移：desktop 层的 ListRequirements 走这里，保证前端
// 拿到的列表已完成回填；workspace 扫描未就绪时自动推迟到下次调用。
func (s *Store) List() []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureMigrated()
	out := make([]Item, len(s.items))
	copy(out, s.items)
	return out
}

// ensureMigrated 执行存量迁移（调用方持锁，最多成功执行一次）：
// 把 Project 为空且 DispatchedTo 命中 resolver 工作区列表的条目回填
// Project = DispatchedTo 原值。resolver 未注入或返回空列表（扫描未完成）
// 时不置完成标记，留给下次触发；落盘失败回滚内存，同样允许重试。
func (s *Store) ensureMigrated() {
	if s.migrated || s.workspaceResolver == nil {
		return
	}
	ws := s.workspaceResolver()
	if len(ws) == 0 {
		return
	}
	old := append([]Item(nil), s.items...)
	changed := false
	for i := range s.items {
		if s.items[i].Project != "" || s.items[i].DispatchedTo == "" {
			continue
		}
		for _, w := range ws {
			if sameWorkspacePath(w, s.items[i].DispatchedTo) {
				s.items[i].Project = s.items[i].DispatchedTo
				changed = true
				break
			}
		}
	}
	if !changed {
		s.migrated = true
		return
	}
	if err := s.write(); err != nil {
		s.items = old // 落盘失败回滚，下次触发重试
		return
	}
	s.migrated = true
}

// Add 新增一条需求：生成 ID 与时间戳，插入列表头（最新在前）。
// text 与 project 去空白后非空才允许保存；priority 非法值归一化为 medium。
func (s *Store) Add(project, text, priority string, tags []string) (Item, error) {
	project = strings.TrimSpace(project)
	if project == "" {
		return Item{}, ErrProjectRequired
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return Item{}, ErrTextRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	it := Item{
		ID:       newID(),
		Text:     text,
		Priority: normalizePriority(priority),
		Tags:     copyTags(tags),
		Project:  project,
		Status:   StatusPending,
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

// Update 修改既有需求（项目/文本/优先级/标签整体替换）；id 不存在返回错误。
// 仅 pending 状态允许修改，防止执行中/已完成/已归档的需求被误改历史记录。
func (s *Store) Update(id, project, text, priority string, tags []string) (Item, error) {
	project = strings.TrimSpace(project)
	if project == "" {
		return Item{}, ErrProjectRequired
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return Item{}, ErrTextRequired
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.items {
		if s.items[i].ID != id {
			continue
		}
		if s.items[i].Status != StatusPending {
			return Item{}, errors.New("仅未开始的需求允许修改")
		}
		old := s.items[i]
		s.items[i].Project = project
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
// 仅 pending 状态可删，避免误删已派发/已完成的历史记录。
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.items {
		if s.items[i].ID != id {
			continue
		}
		switch s.items[i].Status {
		case StatusActive, StatusDone, StatusArchived:
			return errors.New("仅未开始的需求允许删除")
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

// MarkDispatched 标记需求已派发到某工作区并关联会话 ID 与会话标题快照；
// sessionID 是磁盘会话 ID（可空：降级新起的会话尚未绑定磁盘记录）；
// id 不存在返回错误。
// 派发意味着需求进入执行阶段，状态置为 active。仅 pending/active 条目允许
// 派发，done/archived 为终态，重新派发会把状态拉回 active，击穿归档不变量。
func (s *Store) MarkDispatched(id, workspace, chatID, title, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.items {
		if s.items[i].ID != id {
			continue
		}
		if st := s.items[i].Status; st != StatusPending && st != StatusActive {
			return errors.New("已完成或已归档的需求不可重新派发")
		}
		old := s.items[i]
		s.items[i].DispatchedTo = workspace
		s.items[i].ChatID = chatID
		s.items[i].SessionID = sessionID
		s.items[i].SessionTitle = title
		s.items[i].Status = StatusActive
		if err := s.write(); err != nil {
			s.items[i] = old // 落盘失败回滚
			return err
		}
		return nil
	}
	return errors.New("需求不存在: " + id)
}

// SetSessionID 按 ChatID 补录关联需求的磁盘会话 ID（存量条目派发时尚无该字段，
// ListRequirements 反查活跃会话时惰性回填）。返回是否发生更新：空 sessionID、
// 已有同值、无匹配条目都返回 false。落盘失败返回错误。
func (s *Store) SetSessionID(chatID, sessionID string) (bool, error) {
	if chatID == "" || sessionID == "" {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.items {
		if s.items[i].ChatID != chatID {
			continue
		}
		if s.items[i].SessionID == sessionID {
			return false, nil
		}
		old := s.items[i].SessionID
		s.items[i].SessionID = sessionID
		if err := s.write(); err != nil {
			s.items[i].SessionID = old // 落盘失败回滚
			return false, err
		}
		return true, nil
	}
	return false, nil
}

// SetSessionTitle 按 ChatID 更新关联需求的会话标题快照（会话首条消息改标题、
// 扫描回填真实标题时调用）。返回是否发生更新：
// 空标题、标题相同、无匹配条目、条目已归档（终态快照不改写）都返回 false。
// 落盘失败返回错误。
func (s *Store) SetSessionTitle(chatID, title string) (bool, error) {
	if chatID == "" || title == "" {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.items {
		if s.items[i].ChatID != chatID || s.items[i].Status == StatusArchived {
			continue
		}
		if s.items[i].SessionTitle == title {
			return false, nil
		}
		old := s.items[i].SessionTitle
		s.items[i].SessionTitle = title
		if err := s.write(); err != nil {
			s.items[i].SessionTitle = old // 落盘失败回滚
			return false, err
		}
		return true, nil
	}
	return false, nil
}

// SetStatus 限制性地推进状态机：只允许 pending→active、active→done 两种
// 前向迁移，其余（非法值、回退、归档后再改）一律拒绝，避免调用方绕过
// Archive 等带额外字段的专用入口破坏不变量。
func (s *Store) SetStatus(id, status string) error {
	if status != StatusActive && status != StatusDone {
		return errors.New("非法的需求状态: " + status)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.items {
		if s.items[i].ID != id {
			continue
		}
		st := s.items[i].Status
		allowed := (st == StatusPending && status == StatusActive) ||
			(st == StatusActive && status == StatusDone)
		if !allowed {
			return errors.New("需求状态不允许从 " + st + " 变更为 " + status)
		}
		old := s.items[i].Status
		s.items[i].Status = status
		if err := s.write(); err != nil {
			s.items[i].Status = old // 落盘失败回滚
			return err
		}
		return nil
	}
	return errors.New("需求不存在: " + id)
}

// Archive 把 done 状态的需求归档（终态），并记录归档时刻。仅 done 可归档，
// 未完成的需求应先走正常完成流程，防止半途归档丢失跟踪。
func (s *Store) Archive(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.items {
		if s.items[i].ID != id {
			continue
		}
		if s.items[i].Status != StatusDone {
			return errors.New("仅已完成的需求允许归档")
		}
		old := s.items[i]
		s.items[i].Status = StatusArchived
		s.items[i].ArchivedAt = time.Now().UnixNano()
		if err := s.write(); err != nil {
			s.items[i] = old // 落盘失败回滚
			return err
		}
		return nil
	}
	return errors.New("需求不存在: " + id)
}

// ListArchived 返回全部归档条目，按归档时间倒序（最新归档在前），便于
// 界面直接按时间浏览历史。
func (s *Store) ListArchived() []Item {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []Item
	for _, it := range s.items {
		if it.Status == StatusArchived {
			out = append(out, it)
		}
	}
	// 插入排序按 ArchivedAt 降序：归档条目通常很少，无需引入 sort
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].ArchivedAt > out[j-1].ArchivedAt; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// write 原子写：先写同目录唯一临时文件再改名，避免写一半崩溃留下半截 JSON
// （对齐 hub.writeFile）。写回时保持当前 fileVersion。调用方持锁。
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

// normalizeStatus 把空值/非法状态归一化为 pending：旧落盘数据没有 status
// 字段，解析后是零值 ""，统一补默认值避免状态机遇到未知分支。
func normalizeStatus(st string) string {
	switch st {
	case StatusActive, StatusDone, StatusArchived:
		return st
	default:
		return StatusPending
	}
}

// copyTags 拷贝标签切片，隔离调用方后续修改；空入参返回空切片而非 nil——
// Go nil slice 经 JSON 序列化是 null，前端对条目 tags 直接 .map 会崩溃（真实事故）。
func copyTags(tags []string) []string {
	out := make([]string, len(tags))
	copy(out, tags)
	return out
}

// normalizeWorkspacePath 归一化工作区路径：分隔符统一 '/'、去掉尾部多余
// 分隔符、统一小写。语义对齐前端 lib/workspacePath.ts 的同名函数——
// 同一目录在不同数据源里写法不一致（大小写漂移、斜杠方向不同），只按
// 原样比较会把同一工作区当成两个。
func normalizeWorkspacePath(p string) string {
	if p == "" {
		return ""
	}
	p = strings.ReplaceAll(p, `\`, "/")
	p = strings.TrimRight(p, "/")
	return strings.ToLower(p)
}

// sameWorkspacePath 判断两个路径是否指向同一工作区（归一化后比较）。
func sameWorkspacePath(a, b string) bool {
	return normalizeWorkspacePath(a) == normalizeWorkspacePath(b)
}
