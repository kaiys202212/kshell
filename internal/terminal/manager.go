// Package terminal 管理桌面版中心区里的内嵌终端会话：
// 进程启动交给可替换的 Backend（真实实现见 backend_pty.go，Windows 走 ConPTY），
// 本包负责会话表、读协程、环形缓冲与状态/事件回调。
//
// 绑定方法（desktop.App）会被多个 goroutine 调用，Manager 的全部导出方法并发安全。
package terminal

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/yangk/kshell/internal/discovery"
)

// 会话类型与状态取值：与前端约定，不要在绑定层另写字面量。
const (
	KindSession = "session" // 恢复某个历史会话
	KindNew     = "new"     // 在工作区新建会话

	StatusRunning = "running"
	StatusExited  = "exited"
)

var (
	errNoBackend  = errors.New("终端后端未装配")
	errNotFound   = errors.New("终端不存在或已关闭")
	errExited     = errors.New("终端已退出")
	errBadSize    = errors.New("终端尺寸必须为正数")
	errEmptyKey   = errors.New("终端 key 不能为空")
	errEmptyStart = errors.New("终端启动失败：未指定可执行文件")
)

// defaultScrollback 是每个会话保留的回放缓冲上限（256 KiB），超出丢最旧字节。
const defaultScrollback = 256 * 1024

// readChunk 是单次 Read 的缓冲区大小，同时也是单次 onData 回调的最大字节数。
const readChunk = 32 * 1024

// 打开时的兜底尺寸：前端在终端组件尚未布局（隐藏页签）时可能传 0，
// 此时仍要能把终端开起来，后续 ResizeTerminal 会纠正。
const (
	defaultCols = 80
	defaultRows = 25
)

// 进程退出后的收尾宽限（真机实测的必要处理）：
// ConPTY 在子进程退出时既不会给读端 EOF，退出瞬间 ConPTY 内部还有没来得及 flush 的输出，
// 立刻关句柄会丢掉最后一段（实测约 4 成概率丢，如退出前最后一行）。因此以
// 「连续 drainIdle 没有新数据」为界收尾，并用 drainMax 兜底防止一直读不停。
const (
	drainIdle = 80 * time.Millisecond
	drainMax  = 500 * time.Millisecond
)

// Spec 描述一次终端进程启动（已完成平台的 shim 解析）。
type Spec struct {
	Path string
	Args []string
	Dir  string
	Env  []string // 为空表示继承当前进程环境
}

// Info 是暴露给前端的终端快照。
type Info struct {
	ID        string
	Kind      string // KindSession | KindNew
	SessionID string // Kind == KindSession 时关联的历史会话 ID
	Workspace string
	Title     string
	ToolID    string
	Status    string // StatusRunning | StatusExited
	ExitCode  int
	Cols      int
	Rows      int
}

// Backend 负责真正把进程挂到伪终端上启动；可打桩替换，便于测试。
type Backend interface {
	Start(spec Spec, cols, rows int) (Handle, error)
}

// Handle 是一个已启动终端进程的句柄。
// 契约：Read 在读协程单 goroutine 调用；Close 与 Wait 可能来自不同 goroutine，
// 实现方必须保证 Close 幂等、且 Close 后 Read 能立即返回错误（否则读协程会一直挂着）。
type Handle interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Resize(cols, rows int) error
	Wait() (int, error)
	Close() error
}

// session 是一个终端会话的内部状态。
// 字段除 gen/handle 外都在 Manager.mu 保护下读写；
// gen 是「第几次启动」的代号：旧读协程用捕获的 gen 判断自己是否已被重开，避免越权改动状态。
type session struct {
	id   string
	info Info
	gen  int

	handle Handle
	exited bool
	buf    []byte // 最近 defaultScrollback 字节的输出（超出丢最旧）
	read   int    // 累计读到的字节数，收尾宽限用它判断「还有没有新输出」
}

// Manager 维护 key→会话 与 id→会话 两张表，并为每个会话跑一个读协程。
type Manager struct {
	backend Backend
	onData  func(id string, chunk []byte) // 锁外调用，chunk 是独立副本
	onExit  func(id string, exitCode int) // 锁外调用，仅在运行→退出的状态跃迁时触发一次
	maxBuf  int
	nextSeq int

	mu    sync.Mutex
	byKey map[string]*session
	byID  map[string]*session
	order []*session // 按创建顺序，List 据此排序
}

// NewManager 创建管理器；onData/onExit 可为 nil（回调在锁外执行，允许重入本管理器）。
func NewManager(b Backend, onData func(id string, chunk []byte), onExit func(id string, exitCode int)) *Manager {
	return &Manager{
		backend: b,
		onData:  onData,
		onExit:  onExit,
		maxBuf:  defaultScrollback,
		byKey:   make(map[string]*session),
		byID:    make(map[string]*session),
	}
}

// Open 打开（或复用）一个终端会话。
// key 是幂等键：同 key 且运行中直接返回既有 Info（不重启进程）；
// 同 key 但已退出（含被 Close）则关闭旧句柄后原地重启，沿用同一个 ID 与 key，并重置回放缓冲。
// 启动失败时返回错误且不留下半启动的会话（已有会话会被归位到「已退出」）。
func (m *Manager) Open(key string, info Info, spec Spec, cols, rows int) (Info, error) {
	if m.backend == nil {
		return Info{}, errNoBackend
	}
	if key == "" {
		return Info{}, errEmptyKey
	}
	cols, rows = normalizeSize(cols, rows)

	// 启动进程期间持锁：同 key 的并发 Open 必须只起一个进程（Start 本身不回调本管理器，不会死锁）。
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.byKey[key]; ok {
		if !s.exited && s.handle != nil {
			return s.info, nil
		}
		if s.handle != nil {
			_ = s.handle.Close() // 旧进程/旧句柄先清掉，避免泄漏
		}
		s.buf = nil
		if err := m.startLocked(s, info, spec, cols, rows); err != nil {
			return Info{}, err
		}
		return s.info, nil
	}

	s := &session{id: m.newIDLocked()}
	if err := m.startLocked(s, info, spec, cols, rows); err != nil {
		return Info{}, err
	}
	m.byKey[key] = s
	m.byID[s.id] = s
	m.order = append(m.order, s)
	return s.info, nil
}

// startLocked 启动进程并落位会话状态（调用方持锁）。
// 失败时把会话归位到「已退出」，让调用方拿到错误、后续 Open 能重试。
func (m *Manager) startLocked(s *session, info Info, spec Spec, cols, rows int) error {
	h, err := m.backend.Start(spec, cols, rows)
	if err != nil {
		s.handle = nil
		s.exited = true
		s.info.Status = StatusExited
		return err
	}

	s.handle = h
	s.info = info
	s.info.ID = s.id
	if s.info.Kind == "" {
		s.info.Kind = KindSession
	}
	s.info.Status = StatusRunning
	s.info.ExitCode = 0
	s.info.Cols, s.info.Rows = cols, rows
	s.exited = false
	s.gen++
	go m.run(s, h, s.gen)
	return nil
}

// newIDLocked 生成会话 ID（调用方持锁）。ID 全进程唯一、可读，前端只当作不透明标识。
func (m *Manager) newIDLocked() string {
	m.nextSeq++
	return fmt.Sprintf("t%d", m.nextSeq)
}

// run 是每个会话唯一的收尾协程：
//  1. 并发起读协程，把输出投递到回放缓冲与 onData；
//  2. 等进程退出（Wait）后先收尾宽限（见 drainAfterExit），再关句柄唤醒读协程——
//     ConPTY 不会在子进程退出时给读端 EOF，必须关句柄；
//  3. 等读协程彻底结束（剩下的数据已投递完）再落退出状态并触发 onExit，
//     避免前端「先收到退出、后收到输出」而丢掉最后一段回放。
func (m *Manager) run(s *session, h Handle, gen int) {
	id := s.id
	readDone := make(chan struct{})

	go func() {
		defer close(readDone)
		buf := make([]byte, readChunk)
		for {
			n, err := h.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n]) // 副本：回调方可以安全留存
				m.record(s, gen, chunk)
				if m.onData != nil {
					m.onData(id, chunk)
				}
			}
			if err != nil {
				return
			}
		}
	}()

	code, _ := h.Wait() // 退出码是唯一在意的信息；等不到进程时才有的 err 由退出码兜底
	m.drainAfterExit(s, gen, readDone)
	_ = h.Close() // 关句柄唤醒可能仍阻塞在 Read 的读协程（ConPTY 不自行 EOF）
	<-readDone

	if m.markExited(id, gen, code) && m.onExit != nil {
		m.onExit(id, code)
	}
}

// drainAfterExit 在进程退出后、关句柄前等读协程把最后一段输出收干净（见 drainIdle/drainMax 说明）。
// 读协程自然结束时（Unix 上进程退出即 EIO）立刻返回，不引入额外延迟。
func (m *Manager) drainAfterExit(s *session, gen int, readDone <-chan struct{}) {
	deadline := time.Now().Add(drainMax)
	last := m.readBytes(s, gen)
	for time.Now().Before(deadline) {
		select {
		case <-readDone:
			return
		case <-time.After(drainIdle):
		}
		if cur := m.readBytes(s, gen); cur != last {
			last = cur
			continue // 还有新数据在 flush，再等一轮
		}
		return
	}
}

// record 把一段输出追加到会话的回放缓冲（锁内），旧读协程（gen 不匹配）直接丢弃。
func (m *Manager) record(s *session, gen int, chunk []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.gen != gen {
		return
	}
	s.buf = appendCapped(s.buf, chunk, m.maxBuf)
	s.read += len(chunk)
}

// readBytes 返回会话累计读到的字节数（收尾宽限判断用）。
func (m *Manager) readBytes(s *session, gen int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.gen != gen {
		return -1 // 已被重开：旧读协程的收尾不必再等
	}
	return s.read
}

// markExited 完成「运行中 → 已退出」的状态跃迁，返回 true 表示本次调用赢得了跃迁。
// 已被 Close 或已被重开的会话由 gen/exited 拦住，因此 onExit 只会触发一次、且关闭后不再触发。
func (m *Manager) markExited(id string, gen int, code int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.byID[id]
	if s == nil || s.gen != gen || s.exited {
		return false
	}
	s.exited = true
	s.handle = nil
	s.info.Status = StatusExited
	s.info.ExitCode = code
	return true
}

// Write 把输入写入终端。
func (m *Manager) Write(id string, data []byte) error {
	m.mu.Lock()
	s := m.byID[id]
	if s == nil {
		m.mu.Unlock()
		return errNotFound
	}
	if s.exited || s.handle == nil {
		m.mu.Unlock()
		return errExited
	}
	h := s.handle
	m.mu.Unlock()

	_, err := h.Write(data) // 锁外写：写管道可能阻塞，不能占着管理器锁
	return err
}

// Resize 调整终端尺寸，成功后同步到 Info。
func (m *Manager) Resize(id string, cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return errBadSize
	}
	m.mu.Lock()
	s := m.byID[id]
	if s == nil {
		m.mu.Unlock()
		return errNotFound
	}
	if s.exited || s.handle == nil {
		m.mu.Unlock()
		return errExited
	}
	h := s.handle
	m.mu.Unlock()

	if err := h.Resize(cols, rows); err != nil {
		return err
	}

	m.mu.Lock()
	if cur := m.byID[id]; cur == s {
		s.info.Cols, s.info.Rows = cols, rows
	}
	m.mu.Unlock()
	return nil
}

// Close 结束终端：终止进程并关闭句柄。幂等（重复调用与未知 ID 都返回 nil），
// 且关闭后不会再触发 onExit（前端已主动关闭，不需要「已退出」事件）。
func (m *Manager) Close(id string) error {
	m.mu.Lock()
	s := m.byID[id]
	if s == nil {
		m.mu.Unlock()
		return nil
	}
	h := s.handle
	s.handle = nil
	s.exited = true
	s.info.Status = StatusExited
	m.mu.Unlock()

	if h == nil {
		return nil
	}
	return h.Close()
}

// List 返回全部会话的快照（按创建时间升序，含已退出的会话，供前端还原页签）。
func (m *Manager) List() []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Info, 0, len(m.order))
	for _, s := range m.order {
		out = append(out, s.info)
	}
	return out
}

// AttachSession 把扫描发现的磁盘会话绑定到匹配的新建终端上：
// 新建（KindNew）终端在启动时磁盘上还没有会话记录，SessionID 为空、标题是占位文案；
// 扫描发现新会话后由 desktop 层调用本方法回填，页签标题与前端「恢复/切换」判断都依赖它。
// 只绑运行中、未绑定（SessionID 为空）的新建终端；工作区路径归一化后比较，
// toolID 与终端 ToolID 不一致时不绑（终端 ToolID 为空表示由 launch 选首选，允许绑定）。
// 返回是否发生了绑定，供调用方决定是否通知前端刷新。
func (m *Manager) AttachSession(sessionID, workspace, toolID, title string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	ws := discovery.NormalizePath(workspace)
	for _, s := range m.order {
		info := &s.info
		if s.exited || info.Kind != KindNew || info.SessionID != "" {
			continue
		}
		if discovery.NormalizePath(info.Workspace) != ws {
			continue
		}
		if info.ToolID != "" && toolID != "" && info.ToolID != toolID {
			continue
		}
		info.SessionID = sessionID
		if title != "" {
			info.Title = title
		}
		return true
	}
	return false
}

// Scrollback 返回会话最近输出（最多 defaultScrollback 字节）的副本；已退出的会话仍可读。
func (m *Manager) Scrollback(id string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.byID[id]
	if s == nil {
		return nil, errNotFound
	}
	out := make([]byte, len(s.buf))
	copy(out, s.buf)
	return out, nil
}

// CloseAll 关闭全部会话（应用退出时调用），不触发 onExit。
func (m *Manager) CloseAll() {
	m.mu.Lock()
	handles := make([]Handle, 0, len(m.order))
	for _, s := range m.order {
		if s.handle != nil {
			handles = append(handles, s.handle)
			s.handle = nil
		}
		s.exited = true
		s.info.Status = StatusExited
	}
	m.mu.Unlock()

	for _, h := range handles {
		_ = h.Close() // 锁外逐个关闭：结束进程可能耗时，不阻塞其它操作
	}
}

// normalizeSize 把非正尺寸换成默认值（见 defaultCols/defaultRows 的说明）。
func normalizeSize(cols, rows int) (int, int) {
	if cols <= 0 {
		cols = defaultCols
	}
	if rows <= 0 {
		rows = defaultRows
	}
	return cols, rows
}

// appendCapped 把 chunk 追加到 buf，保持总长不超过 max（超出丢最旧字节）。
// 丢弃时原地前移复用底层数组，避免每次追加都重新分配。
func appendCapped(buf, chunk []byte, max int) []byte {
	if max <= 0 {
		return nil
	}
	if len(chunk) >= max {
		out := make([]byte, max)
		copy(out, chunk[len(chunk)-max:])
		return out
	}
	if drop := len(buf) + len(chunk) - max; drop > 0 {
		n := copy(buf, buf[drop:]) // copy 内部按 memmove 处理重叠，安全
		buf = buf[:n]
	}
	return append(buf, chunk...)
}
