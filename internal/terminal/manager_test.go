package terminal

import (
	"bytes"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubHandle 是内存桩句柄：输出由测试 feed 注入，退出码由 finish 指定。
// Read 只由 Manager 的读协程单 goroutine 调用，pending 无需加锁；
// 行为对齐真实 ConPTY：进程退出后先吐出最后一段未 flush 的输出，然后才是流结束（EOF）。
type stubHandle struct {
	data   chan []byte
	closed chan struct{}
	exited chan struct{}

	mu      sync.Mutex
	code    int
	late    []byte // 进程退出后才 flush 出来的尾巴
	pending []byte
	written []byte
	resizes [][2]int

	closeOnce sync.Once
	waitOnce  sync.Once
	finOnce   sync.Once
}

func newStubHandle() *stubHandle {
	return &stubHandle{
		data:   make(chan []byte, 64),
		closed: make(chan struct{}),
		exited: make(chan struct{}),
	}
}

// feed 注入一段终端输出。
func (h *stubHandle) feed(s string) { h.data <- []byte(s) }

// finish 模拟进程自行退出（退出码 code）；tail 是退出后才 flush 出来的最后一段输出。
func (h *stubHandle) finish(code int, tail ...string) {
	h.finOnce.Do(func() {
		h.mu.Lock()
		h.code = code
		if len(tail) > 0 {
			h.late = []byte(strings.Join(tail, ""))
		}
		h.mu.Unlock()
		close(h.exited)
	})
}

func (h *stubHandle) Read(b []byte) (int, error) {
	if len(h.pending) == 0 {
		select {
		case chunk := <-h.data: // 先取已喂入的数据，避免与「已退出」抢
			h.pending = chunk
		default:
			select {
			case chunk := <-h.data:
				h.pending = chunk
			case <-h.closed:
				return 0, io.ErrClosedPipe // 与真实句柄一致：关闭后 Read 立即报错
			case <-h.exited:
				h.mu.Lock()
				late := h.late
				h.late = nil
				h.mu.Unlock()
				if len(late) == 0 {
					return 0, io.EOF
				}
				h.pending = late
			}
		}
	}
	n := copy(b, h.pending)
	h.pending = h.pending[n:]
	return n, nil
}

func (h *stubHandle) Write(b []byte) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if isClosed(h.closed) {
		return 0, io.ErrClosedPipe
	}
	h.written = append(h.written, b...)
	return len(b), nil
}

func (h *stubHandle) Resize(cols, rows int) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if isClosed(h.closed) {
		return io.ErrClosedPipe
	}
	h.resizes = append(h.resizes, [2]int{cols, rows})
	return nil
}

// Wait 等进程结束（finish 或 Close 都会唤醒），返回退出码。
func (h *stubHandle) Wait() (int, error) {
	h.waitOnce.Do(func() {
		select {
		case <-h.exited:
		case <-h.closed:
		}
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.code, nil
}

func (h *stubHandle) Close() error {
	h.closeOnce.Do(func() { close(h.closed) })
	return nil
}

func (h *stubHandle) writes() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return string(h.written)
}

func isClosed(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// stubBackend 记录每次 Start 并产出桩句柄。
type stubBackend struct {
	mu       sync.Mutex
	specs    []Spec
	sizes    [][2]int
	handles  []*stubHandle
	startErr error
}

func (b *stubBackend) Start(spec Spec, cols, rows int) (Handle, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.startErr != nil {
		return nil, b.startErr
	}
	h := newStubHandle()
	b.specs = append(b.specs, spec)
	b.sizes = append(b.sizes, [2]int{cols, rows})
	b.handles = append(b.handles, h)
	return h, nil
}

func (b *stubBackend) starts() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.handles)
}

func (b *stubBackend) lastSpec() Spec {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.specs) == 0 {
		return Spec{}
	}
	return b.specs[len(b.specs)-1]
}

func (b *stubBackend) lastHandle() *stubHandle {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.handles) == 0 {
		return nil
	}
	return b.handles[len(b.handles)-1]
}

// recorder 收集回调，便于断言事件内容与顺序。
type recorder struct {
	mu     sync.Mutex
	data   []byte
	exits  []int
	seq    []string // "data:<文本>" / "exit:<码>"，按回调发生顺序
	dataCh chan []byte
	exitCh chan int
}

func newRecorder() *recorder {
	return &recorder{dataCh: make(chan []byte, 64), exitCh: make(chan int, 8)}
}

// 事件通道只做「通知」，满了就丢：回调在读协程里同步执行，不能让测试的消费速度反过来卡住读协程。
func (r *recorder) onData(_ string, chunk []byte) {
	r.mu.Lock()
	r.data = append(r.data, chunk...)
	r.seq = append(r.seq, "data:"+string(chunk))
	r.mu.Unlock()
	select {
	case r.dataCh <- chunk:
	default:
	}
}

func (r *recorder) onExit(_ string, code int) {
	r.mu.Lock()
	r.exits = append(r.exits, code)
	r.seq = append(r.seq, "exit:"+strconv.Itoa(code))
	r.mu.Unlock()
	select {
	case r.exitCh <- code:
	default:
	}
}

func (r *recorder) output() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.data)
}

func (r *recorder) exitCodes() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int(nil), r.exits...)
}

func (r *recorder) events() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seq...)
}

// waitFor 阻塞等待条件成立，超时即失败。
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("等待 %s 超时", what)
}

func newTestManager(t *testing.T) (*Manager, *stubBackend, *recorder) {
	t.Helper()
	b := &stubBackend{}
	r := newRecorder()
	m := NewManager(b, r.onData, r.onExit)
	return m, b, r
}

func sampleSpec() Spec {
	return Spec{Path: "claude", Args: []string{"--resume", "s1"}, Dir: "D:/ws"}
}

func openInfo(id string) Info {
	return Info{Kind: KindSession, SessionID: id, Workspace: "D:/ws", Title: "修复登录", ToolID: "claude"}
}

func TestOpenStartsAndReusesRunningSession(t *testing.T) {
	m, b, _ := newTestManager(t)

	first, err := m.Open("session:s1", openInfo("s1"), sampleSpec(), 100, 30)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if first.ID == "" {
		t.Fatal("Open 应返回非空 ID")
	}
	if first.Status != StatusRunning || first.Kind != KindSession || first.SessionID != "s1" {
		t.Fatalf("Info 字段未正确落位: %+v", first)
	}
	if first.Cols != 100 || first.Rows != 30 {
		t.Fatalf("Info 尺寸未落位: %+v", first)
	}
	if got := b.lastSpec(); got.Path != "claude" || len(got.Args) != 2 || got.Dir != "D:/ws" {
		t.Fatalf("Spec 未透传给后端: %+v", got)
	}

	second, err := m.Open("session:s1", openInfo("s1"), sampleSpec(), 80, 24)
	if err != nil {
		t.Fatalf("重复 Open error: %v", err)
	}
	if second != first {
		t.Fatalf("运行中的同 key Open 应复用既有 Info: %+v vs %+v", second, first)
	}
	if b.starts() != 1 {
		t.Fatalf("运行中的同 key Open 不应重启进程, starts = %d", b.starts())
	}
}

func TestOpenRestartsExitedSessionKeepingID(t *testing.T) {
	m, b, r := newTestManager(t)

	first, err := m.Open("session:s1", openInfo("s1"), sampleSpec(), 80, 24)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	b.lastHandle().feed("hello")
	waitFor(t, "首次输出投递", func() bool { return r.output() == "hello" })

	b.lastHandle().finish(3)
	waitFor(t, "退出回调", func() bool { return len(r.exitCodes()) == 1 })
	if r.exitCodes()[0] != 3 {
		t.Fatalf("退出码 = %v, 期望 3", r.exitCodes())
	}
	list := m.List()
	if len(list) != 1 || list[0].Status != StatusExited || list[0].ExitCode != 3 {
		t.Fatalf("退出后 List = %+v", list)
	}
	if _, err := m.Scrollback(first.ID); err != nil {
		t.Fatalf("已退出会话 Scrollback 应仍可读: %v", err)
	}

	second, err := m.Open("session:s1", openInfo("s1"), sampleSpec(), 80, 24)
	if err != nil {
		t.Fatalf("重启 Open error: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("重启应沿用同一 ID: %q vs %q", second.ID, first.ID)
	}
	if b.starts() != 2 {
		t.Fatalf("已退出的同 key Open 应重启进程, starts = %d", b.starts())
	}
	if second.Status != StatusRunning || second.ExitCode != 0 {
		t.Fatalf("重启后状态未复位: %+v", second)
	}
	// 重开重置缓冲：只应看到重启之后的数据
	b.lastHandle().feed("again")
	waitFor(t, "重启后输出投递", func() bool { return r.output() == "helloagain" })
	got, err := m.Scrollback(second.ID)
	if err != nil {
		t.Fatalf("Scrollback error: %v", err)
	}
	if string(got) != "again" {
		t.Fatalf("重开应重置回放缓冲, got %q", got)
	}
	if len(m.List()) != 1 {
		t.Fatalf("重开不应新增会话: %+v", m.List())
	}
}

func TestScrollbackKeepsRecentBytesOnly(t *testing.T) {
	m, b, r := newTestManager(t)

	info, err := m.Open("session:s1", openInfo("s1"), sampleSpec(), 80, 24)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}

	chunk := bytes.Repeat([]byte("x"), 4096)
	const total = defaultScrollback + 64*1024
	for sent := 0; sent < total; sent += len(chunk) {
		b.lastHandle().feed(string(chunk))
	}
	waitFor(t, "全部输出投递", func() bool { return len(r.output()) == total })

	waitFor(t, "缓冲到达上限", func() bool {
		got, err := m.Scrollback(info.ID)
		return err == nil && len(got) == defaultScrollback
	})
	got, err := m.Scrollback(info.ID)
	if err != nil {
		t.Fatalf("Scrollback error: %v", err)
	}
	if len(got) != defaultScrollback {
		t.Fatalf("缓冲上限 = %d 字节, 期望 %d", len(got), defaultScrollback)
	}
	if !bytes.Equal(got, bytes.Repeat([]byte("x"), defaultScrollback)) {
		t.Fatal("缓冲内容应保留最近的输出")
	}
}

func TestOpenNormalizesNonPositiveSize(t *testing.T) {
	m, _, _ := newTestManager(t)

	info, err := m.Open("session:s1", openInfo("s1"), sampleSpec(), 0, 0)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if info.Cols != defaultCols || info.Rows != defaultRows {
		t.Fatalf("非正尺寸应回退到默认值, got %dx%d", info.Cols, info.Rows)
	}
}

func TestWriteAndResize(t *testing.T) {
	m, b, _ := newTestManager(t)

	info, err := m.Open("session:s1", openInfo("s1"), sampleSpec(), 80, 24)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}

	if err := m.Write(info.ID, []byte("ls\r")); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if got := b.lastHandle().writes(); got != "ls\r" {
		t.Fatalf("句柄收到 %q, 期望 %q", got, "ls\r")
	}

	if err := m.Resize(info.ID, 120, 40); err != nil {
		t.Fatalf("Resize error: %v", err)
	}
	if got := m.List()[0]; got.Cols != 120 || got.Rows != 40 {
		t.Fatalf("Resize 后 Info 尺寸未同步: %+v", got)
	}

	if err := m.Resize(info.ID, 0, 0); err == nil {
		t.Fatal("非正尺寸的 Resize 应报错")
	}
}

func TestWriteResizeOnUnknownOrExited(t *testing.T) {
	m, b, r := newTestManager(t)

	if err := m.Write("nope", []byte("x")); err == nil || !strings.Contains(err.Error(), "终端不存在") {
		t.Fatalf("未知会话 Write 应报错: %v", err)
	}
	if err := m.Resize("nope", 80, 24); !errors.Is(err, errNotFound) {
		t.Fatalf("未知会话 Resize 应报 errNotFound: %v", err)
	}
	if _, err := m.Scrollback("nope"); !errors.Is(err, errNotFound) {
		t.Fatalf("未知会话 Scrollback 应报 errNotFound: %v", err)
	}

	info, err := m.Open("session:s1", openInfo("s1"), sampleSpec(), 80, 24)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	b.lastHandle().finish(0)
	waitFor(t, "退出回调", func() bool { return len(r.exitCodes()) == 1 })

	if err := m.Write(info.ID, []byte("x")); !errors.Is(err, errExited) {
		t.Fatalf("已退出会话 Write 应报 errExited: %v", err)
	}
	if err := m.Resize(info.ID, 80, 24); !errors.Is(err, errExited) {
		t.Fatalf("已退出会话 Resize 应报 errExited: %v", err)
	}
}

func TestCloseIsIdempotentAndSilencesExit(t *testing.T) {
	m, b, r := newTestManager(t)

	info, err := m.Open("session:s1", openInfo("s1"), sampleSpec(), 80, 24)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if err := m.Close(info.ID); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	if err := m.Close(info.ID); err != nil {
		t.Fatalf("重复 Close 应返回 nil: %v", err)
	}
	if err := m.Close("nope"); err != nil {
		t.Fatalf("未知 ID Close 应返回 nil: %v", err)
	}

	// 关闭后进程收尾不得再报退出（前端已主动关闭）
	waitFor(t, "句柄关闭", func() bool { return isClosed(b.lastHandle().closed) })
	time.Sleep(50 * time.Millisecond)
	if got := r.exitCodes(); len(got) != 0 {
		t.Fatalf("关闭后不应触发 onExit, got %v", got)
	}
	// 主动关闭应从 List 摘除：否则前端 scan:done 整表刷新会把已关页签「复活」
	if got := m.List(); len(got) != 0 {
		t.Fatalf("Close 后 List 应为空, got %+v", got)
	}
	if err := m.Write(info.ID, []byte("x")); !errors.Is(err, errNotFound) {
		t.Fatalf("关闭后 Write 应报 errNotFound: %v", err)
	}
}

// TestExitAfterPendingOutputStillDelivered 覆盖真实 ConPTY 的行为：
// 进程退出时最后一段输出可能还在伪终端里（要在关句柄前收干净），
// 且「退出事件」必须排在所有输出回调之后，否则前端会丢掉最后一段回放。
func TestExitAfterPendingOutputStillDelivered(t *testing.T) {
	m, b, r := newTestManager(t)

	info, err := m.Open("session:s1", openInfo("s1"), sampleSpec(), 80, 24)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	b.lastHandle().feed("first")
	waitFor(t, "首段输出投递", func() bool { return r.output() == "first" })

	b.lastHandle().finish(7, "tail") // 退出后还有一段未 flush 的尾巴
	waitFor(t, "退出回调", func() bool { return len(r.exitCodes()) == 1 })

	if got := r.output(); got != "firsttail" {
		t.Fatalf("累计输出 = %q, 期望 firsttail", got)
	}
	events := r.events()
	if len(events) == 0 || events[len(events)-1] != "exit:7" {
		t.Fatalf("退出回调必须是最后一个事件: %v", events)
	}
	if got, err := m.Scrollback(info.ID); err != nil || string(got) != "firsttail" {
		t.Fatalf("Scrollback = %q, err = %v", got, err)
	}
}

func TestListFollowsCreationOrder(t *testing.T) {
	m, _, _ := newTestManager(t)

	a, err := m.Open("session:a", openInfo("a"), sampleSpec(), 80, 24)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	bInfo, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "D:/ws2", Title: "ws2 · Claude"}, sampleSpec(), 80, 24)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}

	list := m.List()
	if len(list) != 2 || list[0].ID != a.ID || list[1].ID != bInfo.ID {
		t.Fatalf("List 应按创建顺序: %+v", list)
	}
	if list[1].Kind != KindNew || list[1].Title != "ws2 · Claude" {
		t.Fatalf("Info 字段未落位: %+v", list[1])
	}

	// 快照与内部状态解耦：改动返回值不应影响管理器
	list[0].Title = "被改了"
	if m.List()[0].Title == "被改了" {
		t.Fatal("List 应返回快照副本")
	}
}

func TestConcurrentOpenSameKeyStartsOnce(t *testing.T) {
	m, b, _ := newTestManager(t)

	const n = 16
	var wg sync.WaitGroup
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			info, err := m.Open("session:s1", openInfo("s1"), sampleSpec(), 80, 24)
			if err != nil {
				t.Errorf("并发 Open error: %v", err)
				return
			}
			ids[i] = info.ID
		}(i)
	}
	wg.Wait()

	if b.starts() != 1 {
		t.Fatalf("同 key 并发 Open 只应起一个进程, starts = %d", b.starts())
	}
	for i, id := range ids {
		if id != ids[0] {
			t.Fatalf("并发 Open 应返回同一 ID: [%d]=%q vs [0]=%q", i, id, ids[0])
		}
	}
	if len(m.List()) != 1 {
		t.Fatalf("并发 Open 不应新增会话: %+v", m.List())
	}
}

func TestOpenStartFailureLeavesNoSession(t *testing.T) {
	m, b, _ := newTestManager(t)
	b.startErr = errors.New("启动炸了")

	if _, err := m.Open("session:s1", openInfo("s1"), sampleSpec(), 80, 24); err == nil {
		t.Fatal("启动失败应返回错误")
	}
	if got := m.List(); len(got) != 0 {
		t.Fatalf("启动失败不应留下会话: %+v", got)
	}

	// 后端恢复后同 key 仍可正常打开
	b.mu.Lock()
	b.startErr = nil
	b.mu.Unlock()
	info, err := m.Open("session:s1", openInfo("s1"), sampleSpec(), 80, 24)
	if err != nil {
		t.Fatalf("重试 Open error: %v", err)
	}
	if info.Status != StatusRunning {
		t.Fatalf("重试后状态应为 running: %+v", info)
	}
}

func TestCloseAllClosesEverySession(t *testing.T) {
	m, b, r := newTestManager(t)

	if _, err := m.Open("session:a", openInfo("a"), sampleSpec(), 80, 24); err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if _, err := m.Open("session:b", openInfo("b"), sampleSpec(), 80, 24); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	m.CloseAll()

	if b.starts() != 2 {
		t.Fatalf("starts = %d", b.starts())
	}
	b.mu.Lock()
	handles := append([]*stubHandle(nil), b.handles...)
	b.mu.Unlock()
	for _, h := range handles {
		if !isClosed(h.closed) {
			t.Fatal("CloseAll 应关闭全部句柄")
		}
	}
	time.Sleep(50 * time.Millisecond)
	if got := r.exitCodes(); len(got) != 0 {
		t.Fatalf("CloseAll 不应触发 onExit, got %v", got)
	}
	for _, info := range m.List() {
		if info.Status != StatusExited {
			t.Fatalf("CloseAll 后应为 exited: %+v", info)
		}
	}
}

func TestOpenRejectsEmptyKeyAndBackend(t *testing.T) {
	m, _, _ := newTestManager(t)
	if _, err := m.Open("", openInfo("s1"), sampleSpec(), 80, 24); !errors.Is(err, errEmptyKey) {
		t.Fatalf("空 key 应报 errEmptyKey: %v", err)
	}

	empty := NewManager(nil, nil, nil)
	if _, err := empty.Open("session:s1", openInfo("s1"), sampleSpec(), 80, 24); !errors.Is(err, errNoBackend) {
		t.Fatalf("未装配后端应报 errNoBackend: %v", err)
	}
}

func TestAppendCappedDropsOldest(t *testing.T) {
	got := appendCapped([]byte("12345678"), []byte("abc"), 8)
	if string(got) != "45678abc" {
		t.Fatalf("appendCapped = %q", got)
	}
	// 单块就超过上限：只保留最后 max 字节
	got = appendCapped(nil, []byte("abcdefghij"), 4)
	if string(got) != "ghij" {
		t.Fatalf("超长单块 appendCapped = %q", got)
	}
	if got := appendCapped(nil, []byte("x"), 0); got != nil {
		t.Fatalf("max<=0 应返回 nil, got %q", got)
	}
}

// newKindNew 终端 Info：新建会话，尚未绑定磁盘会话（SessionID 空）。
func newKindNewInfo(ws, toolID string) Info {
	return Info{Kind: KindNew, Workspace: ws, Title: ws + " · 工具", ToolID: toolID}
}

func TestAttachSessionBindsRunningNewTerminal(t *testing.T) {
	m, _, _ := newTestManager(t)

	info, err := m.Open("new:1", newKindNewInfo(`D:\ws`, "codebuddy"), sampleSpec(), 80, 24)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}

	if !m.AttachSession("disk-1", `d:/WS/`, "codebuddy", "修复页签标题") {
		t.Fatal("匹配的新建终端应绑定成功")
	}
	list := m.List()
	if len(list) != 1 {
		t.Fatalf("List = %+v", list)
	}
	got := list[0]
	if got.ID != info.ID || got.SessionID != "disk-1" || got.Title != "修复页签标题" {
		t.Fatalf("绑定后 Info 未回填: %+v", got)
	}
}

func TestAttachSessionSkipsNonCandidates(t *testing.T) {
	m, _, _ := newTestManager(t)
	if _, err := m.Open("new:1", newKindNewInfo(`D:\ws`, "codebuddy"), sampleSpec(), 80, 24); err != nil {
		t.Fatalf("Open error: %v", err)
	}

	cases := []struct {
		name      string
		ws, tool  string
	}{
		{"工作区不匹配", `D:\other`, "codebuddy"},
		{"工具不匹配", `D:\ws`, "claude"},
	}
	for _, c := range cases {
		if m.AttachSession("disk-1", c.ws, c.tool, "标题") {
			t.Fatalf("%s 不应绑定", c.name)
		}
	}
	list := m.List()
	if list[0].SessionID != "" || list[0].Title != `D:\ws · 工具` {
		t.Fatalf("不匹配时 Info 不应被改写: %+v", list[0])
	}
}

func TestAttachSessionSkipsSessionKindAndExited(t *testing.T) {
	m, b, _ := newTestManager(t)

	// 已有磁盘会话的恢复型终端不改写
	if _, err := m.Open("session:s1", openInfo("s1"), sampleSpec(), 80, 24); err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if m.AttachSession("disk-1", "D:/ws", "claude", "标题") {
		t.Fatal("KindSession 终端不应被 AttachSession 改写")
	}
	if got := m.List()[0]; got.SessionID != "s1" || got.Title != "修复登录" {
		t.Fatalf("KindSession Info 被意外改写: %+v", got)
	}

	// 已退出的新建终端不绑定
	if _, err := m.Open("new:1", newKindNewInfo(`D:\ws`, "codebuddy"), sampleSpec(), 80, 24); err != nil {
		t.Fatalf("Open error: %v", err)
	}
	m.CloseAll()
	waitFor(t, "全部退出", func() bool {
		for _, it := range m.List() {
			if it.Status == StatusRunning {
				return false
			}
		}
		return true
	})
	_ = b
	if m.AttachSession("disk-1", `D:\ws`, "codebuddy", "标题") {
		t.Fatal("已退出的终端不应绑定")
	}
}

func TestAttachSessionFillsEmptyToolID(t *testing.T) {
	m, _, _ := newTestManager(t)

	// Info.ToolID 为空表示由 launch 选首选工具：该工作区任意工具的新会话都可绑定
	if _, err := m.Open("new:1", newKindNewInfo(`D:\ws`, ""), sampleSpec(), 80, 24); err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if !m.AttachSession("disk-1", `D:\ws`, "codebuddy", "标题") {
		t.Fatal("Info.ToolID 为空时应允许绑定该工作区会话")
	}
	if got := m.List()[0]; got.SessionID != "disk-1" {
		t.Fatalf("绑定失败: %+v", got)
	}
}
