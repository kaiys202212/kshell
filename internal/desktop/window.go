// Package desktop 提供 kshell 桌面版（Wails）的窗口管理：
// 弹出新终端窗口、按标题聚焦、跟踪窗口存活并回收死亡项。
package desktop

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// psQuote 将文本包成 PowerShell 单引号字符串字面量（内嵌单引号双写转义）。
// 平台无关：绑定层组装窗口内命令语句也要用，放这里保证非 Windows 构建可用。
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// TerminalLauncher 抽象「弹新终端窗口」与「按标题聚焦已开窗口」，
// Win32 实现之外可打桩测试。
type TerminalLauncher interface {
	// Launch 在 dir 中弹出新终端窗口，窗口标题恒为 title。
	// args 为附加 PowerShell 语句（如启动 agent CLI 的命令），由内部可信代码传入，
	// 实现方负责把它们安全地并入目标脚本。
	Launch(dir, title string, args []string) error
	// Focus 按标题查找已开窗口并聚焦（最小化也拉回），不存在返回 false。
	// 返回 false 不代表窗口已关闭（可能只是聚焦失败），关闭与否以 Reap 为准。
	Focus(title string) bool
}

// procChecker 判断指定标题的窗口/进程是否存活。
// 存活判断必须基于「按标题找窗口」而非子进程 PID：
// wt 启动器进程可能秒退，真窗口属于 WindowsTerminal 进程。
// 契约：check 不得重入 WindowManager（会在锁外执行，重入虽不死锁但可能自观察）。
type procChecker func(title string) bool

// defaultProcAlive 由平台实现文件注入（window_win.go 的 init）；
// 非 windows 构建为 nil，此时登记的窗口默认视为存活。
var defaultProcAlive procChecker

// defaultLauncherFactory 由平台实现文件注入（window_win.go / launcher_other.go 的 init），
// 供绑定层创建默认弹窗实现；非 windows 构建为占位实现（报不支持）。
var defaultLauncherFactory func() TerminalLauncher

// windowEntry 是活跃表中的一项：存活检查 + 登记时间（宽限期判定用）。
// pending 表示 Launch 已发起但真实检查尚未就位（占位，防并发重复弹窗）。
type windowEntry struct {
	check   procChecker
	since   time.Time
	pending bool
}

// WindowManager 维护 标题→窗口条目 的活跃终端窗口表。
// 前端绑定会被多 goroutine 调用，alive 表由 mu 保护。
type WindowManager struct {
	launcher TerminalLauncher
	onClosed func(title string)
	mu       sync.Mutex
	alive    map[string]*windowEntry

	// reapGrace 是登记后的宽限期：窗口创建可能有数百 ms 到数秒延迟
	//（wt 只是转发器），期间存活探测必然失败，不得据此判死。
	reapGrace time.Duration

	// newChecker 为新登记窗口生成存活检查函数，字段注入便于打桩
	newChecker func(title string) procChecker
}

// NewWindowManager 创建窗口管理器；onClosed 在窗口死亡被回收时回调（参数为标题）。
// 回调在锁外执行，允许重入本管理器。
func NewWindowManager(l TerminalLauncher, onClosed func(title string)) *WindowManager {
	return &WindowManager{
		launcher:  l,
		onClosed:  onClosed,
		alive:     make(map[string]*windowEntry),
		reapGrace: 3 * time.Second,
		newChecker: func(title string) procChecker {
			if defaultProcAlive != nil {
				return defaultProcAlive
			}
			return func(string) bool { return true }
		},
	}
}

// TerminalTitle 生成终端窗口标题："kshell · " + text。
// 超长按 80 rune 截断并以省略号结尾。
func (m *WindowManager) TerminalTitle(text string) string {
	const (
		prefix   = "kshell · "
		maxRunes = 80
	)
	full := prefix + text
	runes := []rune(full)
	if len(runes) <= maxRunes {
		return full
	}
	return string(runes[:maxRunes-1]) + "…"
}

// LaunchSession 在 dir 中为会话 text 弹出终端窗口：
// 已登记则转 Focus 复用；否则先占位登记（挡住并发同标题重复弹窗）再 Launch，
// Launch 失败回滚占位，成功后换上真实存活检查。
// args 为窗口内要执行的附加 PowerShell 语句（如启动 agent CLI），由调用方传入。
func (m *WindowManager) LaunchSession(dir, text string, args ...string) error {
	title := m.TerminalTitle(text)

	m.mu.Lock()
	if e, ok := m.alive[title]; ok {
		pending := e.pending
		m.mu.Unlock()
		if !pending {
			m.launcher.Focus(title)
		}
		return nil // pending 期间（窗口创建中）直接忽略重复请求
	}
	e := &windowEntry{
		check:   func(string) bool { return true },
		since:   time.Now(),
		pending: true,
	}
	m.alive[title] = e
	m.mu.Unlock()

	if err := m.launcher.Launch(dir, title, args); err != nil {
		m.mu.Lock()
		if m.alive[title] == e { // 未被并发替换才回滚
			delete(m.alive, title)
		}
		m.mu.Unlock()
		return err
	}

	m.mu.Lock()
	if m.alive[title] == e {
		e.check = m.newChecker(title)
		e.pending = false
	}
	m.mu.Unlock()
	return nil
}

// Focus 聚焦已登记的窗口；未登记的标题直接返回 false，不打 launcher。
func (m *WindowManager) Focus(title string) bool {
	m.mu.Lock()
	_, exists := m.alive[title]
	m.mu.Unlock()
	if !exists {
		return false
	}
	return m.launcher.Focus(title)
}

// Alive 报告指定标题的窗口是否仍在存活表中（不主动探测）。
func (m *WindowManager) Alive(title string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, exists := m.alive[title]
	return exists
}

// Reap 对超出宽限期的条目执行存活检查，移除死亡窗口并触发 onClosed 回调，
// 返回本次关闭的标题列表（按标题排序，保证确定性）。
// 探测在锁外逐项执行（Win32 枚举较慢，不阻塞 Focus/Launch）；
// 回调也在锁外执行，允许重入本管理器。
func (m *WindowManager) Reap() []string {
	m.mu.Lock()
	now := time.Now()
	snap := make(map[string]*windowEntry, len(m.alive))
	for title, e := range m.alive {
		if now.Sub(e.since) < m.reapGrace {
			continue // 宽限期内不判定，避免窗口创建延迟导致误杀
		}
		snap[title] = e
	}
	m.mu.Unlock()

	var dead []string
	for title, e := range snap {
		if e.check(title) {
			continue
		}
		m.mu.Lock()
		// 仅当条目未被并发替换（重新 Launch/回滚）时才移除
		if m.alive[title] == e {
			delete(m.alive, title)
			dead = append(dead, title)
		}
		m.mu.Unlock()
	}
	sort.Strings(dead)

	if m.onClosed != nil {
		for _, title := range dead {
			m.onClosed(title)
		}
	}
	return dead
}
