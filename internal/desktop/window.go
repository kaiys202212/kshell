// Package desktop 提供 kshell 桌面版（Wails）的窗口管理：
// 弹出新终端窗口、按标题聚焦、跟踪窗口存活并回收死亡项。
package desktop

import (
	"sync"
)

// TerminalLauncher 抽象「弹新终端窗口」与「按标题聚焦已开窗口」，
// Win32 实现之外可打桩测试。
type TerminalLauncher interface {
	// Launch 在 dir 中弹出新终端窗口，窗口标题恒为 title，args 为附加命令行参数
	Launch(dir, title string, args []string) error
	// Focus 按标题查找已开窗口并聚焦（最小化也拉回），不存在返回 false
	Focus(title string) bool
}

// procChecker 判断指定标题的窗口/进程是否存活。
// 存活判断必须基于「按标题找窗口」而非子进程 PID：
// wt 启动器进程可能秒退，真窗口属于 WindowsTerminal 进程。
type procChecker func(title string) bool

// defaultProcAlive 由平台实现文件注入（window_win.go 的 init）；
// 非 windows 构建为 nil，此时登记的窗口默认视为存活。
var defaultProcAlive procChecker

// WindowManager 维护 标题→存活检查 的活跃终端窗口表。
// 前端绑定会被多 goroutine 调用，alive 表由 mu 保护。
type WindowManager struct {
	launcher TerminalLauncher
	onClosed func(title string)
	mu       sync.Mutex
	alive    map[string]procChecker

	// newChecker 为新登记窗口生成存活检查函数，字段注入便于打桩
	newChecker func(title string) procChecker
}

// NewWindowManager 创建窗口管理器；onClosed 在窗口死亡被回收时回调（参数为标题）。
func NewWindowManager(l TerminalLauncher, onClosed func(title string)) *WindowManager {
	return &WindowManager{
		launcher: l,
		onClosed: onClosed,
		alive:    make(map[string]procChecker),
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
// 窗口已存活则转 Focus 复用，否则 launcher.Launch 并登记存活检查。
func (m *WindowManager) LaunchSession(dir, text string) error {
	title := m.TerminalTitle(text)

	m.mu.Lock()
	_, exists := m.alive[title]
	m.mu.Unlock()

	if exists {
		m.Focus(title)
		return nil
	}
	if err := m.launcher.Launch(dir, title, nil); err != nil {
		return err
	}

	m.mu.Lock()
	m.alive[title] = m.newChecker(title)
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

// Reap 逐项执行存活检查，移除死亡窗口并触发 onClosed 回调，
// 返回本次关闭的标题列表。回调在锁外执行，允许其重入本管理器。
func (m *WindowManager) Reap() []string {
	m.mu.Lock()
	dead := make([]string, 0, len(m.alive))
	for title, check := range m.alive {
		if !check(title) {
			dead = append(dead, title)
		}
	}
	for _, title := range dead {
		delete(m.alive, title)
	}
	m.mu.Unlock()

	if m.onClosed != nil {
		for _, title := range dead {
			m.onClosed(title)
		}
	}
	return dead
}
