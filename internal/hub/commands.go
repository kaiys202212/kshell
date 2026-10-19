package hub

import (
	"fmt"
	"strings"
)

// SessionRef 是命令层可见的一个可切换会话（编号映射用）。
type SessionRef struct {
	Num       int    // 编号（ListSessions 返回时由 Console 统一编号 1..N）
	ChatID    string // 活跃 chat 的 ID；未激活的磁盘会话为空
	SessionID string // 磁盘会话 ID（可空）
	Workspace string
	Tool      string
	// ToolName 是工具展示名（欢迎词归因用）；查不到为空，消费方回落 Tool。
	ToolName string
	Title    string
	Current   bool // 是否当前上下文
}

// MenuOption 是菜单里的一项（工具/项目/模型）。
type MenuOption struct {
	ID    string
	Label string
}

// ConsoleStatus 是 #st 的状态快照。
type ConsoleStatus struct {
	CurrentTitle string
	Running      int // 运行中任务数
	Undelivered  int // 微信未送达消息数
}

// Executor 是命令层的执行后端（desktop App 实现；测试用 fake）。
type Executor interface {
	ListSessions() []SessionRef
	CurrentSession() (SessionRef, bool)
	SwitchTo(chatID string) error
	// OpenSession 打开一个尚未激活的磁盘会话（SessionID 非空时有效），返回可投递的引用。
	// 已激活的会话直接返回现状。实现方负责激活（desktop 走 OpenSessionACP）。
	OpenSession(sessionID string) (SessionRef, error)
	EasyNew() (SessionRef, error)
	NewMenuOptions() (tools, workspaces, models []MenuOption)
	CreateNew(toolID, wsID, model string) (SessionRef, error)
	Stop(chatID string) error
	ListModels(chatID string) ([]MenuOption, string, error) // 列表+当前 ID
	SetModel(chatID, model string) error
	SearchSessions(kw string) []SessionRef
	Send(chatID, text string) error
	GrantPermission(chatID, reqID string, allow bool) error
	Status() ConsoleStatus
	// RecentMessages 取 AgentHub 最近 n 条消息（时间正序）；无历史返回空。
	RecentMessages(n int) []Message
}

// —— 用户可见文案统一放 lang.go 的 cmdTexts（双语）——

// lastBase 取路径的末段：兼容 Windows 反斜杠与 Unix 斜杠（跨渠道文本里的
// 路径分隔不保证与运行平台一致，不能只依赖 filepath.Base）。
func lastBase(p string) string {
	if p == "" {
		return ""
	}
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// formatRef 把会话格式化为一行列表项（含编号与当前标记）。▶ 标记放编号后
// 而非行首：行首 ▶ 会打断 Markdown 有序列表解析（回复按 Markdown 渲染）。
func formatRef(r SessionRef) string {
	mark := ""
	if r.Current {
		mark = "▶ "
	}
	return itoa(r.Num) + ". " + mark + "[" + lastBase(r.Workspace) + "] " + r.Tool + " " + r.Title
}

// sessionTag 把会话标题格式化为【会话名】归因前缀；标题为空返回空串。
func sessionTag(title string) string {
	if title == "" {
		return ""
	}
	return "【" + title + "】"
}

// formatSwitched 是切换成功后的单行回显（verb 为含 %s 的文案，传【会话名】）。
func formatSwitched(verb string, r SessionRef) string {
	return fmt.Sprintf(verb, sessionTag(r.Title))
}

// welcomeReply 是创建成功后的 agent 第一人称欢迎词：【会话名】+ session_ready
//（工具展示名缺失回落 ToolID）。
func (c *Console) welcomeReply(r SessionRef) string {
	name := r.ToolName
	if name == "" {
		name = r.Tool
	}
	return sessionTag(r.Title) + fmt.Sprintf(c.text("session_ready"), name)
}

// hubMsgTitle 取消息的归因标题：会话标题快照优先，缺省回落工具 ID。
func hubMsgTitle(m Message) string {
	if m.SessionTitle != "" {
		return m.SessionTitle
	}
	if m.Tool != "" {
		return m.Tool
	}
	return "-"
}

// truncateOneLine 把多行文本压成单行（连续空白折叠为单空格）并按 rune 截断。
func truncateOneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
