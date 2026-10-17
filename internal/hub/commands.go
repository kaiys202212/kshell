package hub

import "strings"

// SessionRef 是命令层可见的一个可切换会话（编号映射用）。
type SessionRef struct {
	Num       int    // 编号（ListSessions 返回时由 Console 统一编号 1..N）
	ChatID    string // 活跃 chat 的 ID；未激活的磁盘会话为空
	SessionID string // 磁盘会话 ID（可空）
	Workspace string
	Tool      string
	Title     string
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
}

// —— 用户可见文案常量（双端共用；i18n 留给后续任务）——

const (
	helpText = `命令列表:
#?  帮助
#l  最近会话(前10)   #la 全部会话
数字 切换到对应会话
#en 快速新建会话     #n  分步新建会话
#s  中断当前任务     #m  切换模型
#g <编号> <内容>  向指定会话发送
#f <关键词>  搜索会话
#st 状态             #c  取消选择
直接输入文字 = 发给当前会话`

	msgNoCurrent       = "当前无活跃会话"
	msgNoCurrentList   = "当前无活跃会话，#l 查看"
	msgNoSessions      = "暂无会话"
	msgNoMapping       = "编号不存在，#l 查看"
	msgSwitchFailed    = "切换失败: %s"
	msgCreateFailed    = "创建失败: %s"
	msgSendFailed      = "发送失败: %s"
	msgCancelled       = "已取消"
	msgNothingToCancel = "无进行中的选择"
	msgPickNumber      = "请回复数字选择，或 #c 取消"
	msgStopped         = "已发送中断指令"
	msgUnknownCmd      = "未知命令，#? 查看帮助"
	msgUsageG          = "用法: #g <编号> <内容>"
	msgUsageF          = "用法: #f <关键词>"
	msgSearchNoMatch   = "未找到匹配会话"
	msgPermDone        = "已处理权限请求"
	msgPermStale       = "该选择已失效，请重新操作"
)

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

// formatRef 把会话格式化为一行列表项（含编号与当前标记）。
func formatRef(r SessionRef) string {
	prefix := ""
	if r.Current {
		prefix = "▶"
	}
	return prefix + itoa(r.Num) + ". [" + lastBase(r.Workspace) + "] " + r.Tool + " " + r.Title
}

// formatCreated 是创建/切换成功后的单行回显。
func formatCreated(verb string, r SessionRef) string {
	return verb + ": [" + lastBase(r.Workspace) + "] " + r.Tool + " " + r.Title
}
