package hub

import (
	"fmt"
	"strconv"
	"strings"
)

// itoa 供 commands.go / console.go 共用的整数转字符串。
func itoa(n int) string { return strconv.Itoa(n) }

// menuKind 区分活跃菜单类型。
type menuKind int

const (
	menuNew    menuKind = iota // #n 分步新建：选工具→项目→模型
	menuModels                 // #m 切换模型
	menuSearch                 // #f 搜索结果选择
)

// pendingMenu 是进行中的多步选择状态（设计决策：无定时器，菜单保持到用户处理或 #c）。
type pendingMenu struct {
	kind menuKind
	// new 菜单字段
	step         int    // 1=选工具 2=选项目 3=选模型
	toolID       string // step2 起已选
	wsID         string // step3 起已选
	toolLabel    string
	options      []MenuOption // 当前步骤待选列表
	targetChatID string       // models 菜单的目标会话
	// search 菜单字段
	results []SessionRef // 搜索结果（同时刷进编号映射）
}

// Reply 是一条要回显给用户的回复。
type Reply struct {
	Text string
}

// Console 是一个渠道（微信/桌面端 AgentHub）的命令会话状态机。
// 每个渠道持自己的实例，互不影响。
type Console struct {
	ex      Executor
	current SessionRef
	// hasCurrent 区分「确无当前会话」与「还没向后端问过」：零值哨兵无法区分。
	hasCurrent bool
	menu       *pendingMenu
	perm       *pendingPermission
	// lastList 是最近一次列表/搜索的编号映射（纯数字与 #g 依据它路由）。
	lastList []SessionRef
}

// pendingPermission 是权限请求的选择状态（1 允许 / 2 拒绝）。
type pendingPermission struct {
	chatID string
	reqID  string
	desc   string // 权限请求描述；本期不回显，留作后续展示
}

// NewConsole 创建命令状态机。
func NewConsole(ex Executor) *Console {
	return &Console{ex: ex}
}

// SetCurrent 显式设置当前会话上下文（渠道初始化时由装配方调用：
// 微信桥用配置里记住的上次会话，桌面端用 hub 最后活跃会话）。
func (c *Console) SetCurrent(ref SessionRef) {
	c.current = ref
	c.hasCurrent = true
}

// OpenPermissionMenu 由渠道层在收到权限请求事件时调用（微信桥/桌面端均可），
// 之后的「1」/「2」输入被解释为允许/拒绝；回显由调用方处理。权限菜单
// 只拦截数字回复，其余输入照常走命令与转发流程。
func (c *Console) OpenPermissionMenu(chatID, reqID, desc string) {
	c.perm = &pendingPermission{chatID: chatID, reqID: reqID, desc: desc}
}

// Handle 处理一条用户输入，返回要回显的回复（可能多条，按序展示）。
func (c *Console) Handle(input string) []Reply {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}

	// permission 菜单只拦截数字回复；其余输入照常走命令/转发流程。
	if c.perm != nil {
		if _, err := strconv.Atoi(input); err == nil {
			return c.handlePermission(input)
		}
	}

	if strings.HasPrefix(input, "#") {
		return c.handleCommand(input)
	}

	// 纯数字：菜单选择或编号切换。
	if n, err := strconv.Atoi(input); err == nil && n > 0 {
		if c.menu != nil {
			return c.handleMenuNumber(n)
		}
		return c.switchByNumber(n)
	}

	// 其他文本：有活跃菜单则提示，否则发给当前会话。
	if c.menu != nil {
		return single(msgPickNumber)
	}
	return c.sendToCurrent(input)
}

// handlePermission 处理权限请求等待期的数字回复。
func (c *Console) handlePermission(input string) []Reply {
	perm := c.perm
	var allow bool
	switch input {
	case "1":
		allow = true
	case "2":
		allow = false
	default:
		// 其余数字不消费菜单，让用户重新选。
		return single("请回复 1（允许）或 2（拒绝）")
	}
	c.perm = nil
	if err := c.ex.GrantPermission(perm.chatID, perm.reqID, allow); err != nil {
		// 请求可能已被桌面端处理，失败一律按失效提示。
		return single(msgPermStale)
	}
	return single(msgPermDone)
}

// handleCommand 分发 # 开头的命令。
func (c *Console) handleCommand(input string) []Reply {
	// #g / #f 是带参数命令，其余先按单词切。
	cmd, arg := splitCmd(input)
	switch cmd {
	case "#?":
		return c.handleHelp()
	case "#l":
		return c.handleList(false)
	case "#la":
		return c.handleList(true)
	case "#en":
		return c.handleEasyNew()
	case "#n":
		return c.handleNewMenu()
	case "#s":
		return c.handleStop()
	case "#m":
		return c.handleModelsMenu()
	case "#c":
		return c.handleCancel()
	case "#st":
		return c.handleStatus()
	case "#g":
		return c.handleG(arg)
	case "#f":
		return c.handleSearch(arg)
	default:
		return single(msgUnknownCmd)
	}
}

// splitCmd 把 "#g 1 hi" 切成 cmd="#g"、arg="1 hi"；无参数时 arg 为空。
// 已知命令都是无空格的短词，取第一个空白前的段。
func splitCmd(input string) (cmd, arg string) {
	if i := strings.IndexAny(input, " \t"); i >= 0 {
		return input[:i], strings.TrimSpace(input[i+1:])
	}
	return input, ""
}

// handleHelp 回复帮助全文与当前会话行。
func (c *Console) handleHelp() []Reply {
	var sb strings.Builder
	sb.WriteString(helpText)
	sb.WriteString("\n\n当前会话: ")
	if !c.hasActiveSession() {
		sb.WriteString("无")
	} else {
		sb.WriteString(fmt.Sprintf("%s (%s)", c.current.Title, c.current.Tool))
	}
	return single(sb.String())
}

// refreshCurrent 从执行后端同步当前会话（后端是唯一事实来源，切换可能
// 发生在别的渠道）。
func (c *Console) refreshCurrent() {
	if cur, ok := c.ex.CurrentSession(); ok {
		c.current = cur
		c.hasCurrent = true
	}
}

// hasActiveSession 报告当前是否有活跃会话（惰性问一次后端）。
func (c *Console) hasActiveSession() bool {
	if !c.hasCurrent {
		c.refreshCurrent()
	}
	return c.hasCurrent
}

// handleList 列出会话；all=false 只取前 10。同时刷新编号映射与 current。
func (c *Console) handleList(all bool) []Reply {
	sessions := c.ex.ListSessions()
	if len(sessions) == 0 {
		return single(msgNoSessions)
	}
	limit := len(sessions)
	if !all && limit > 10 {
		limit = 10
	}
	shown := sessions[:limit]
	c.lastList = shown
	c.refreshCurrent()

	lines := make([]string, 0, limit)
	for i := range shown {
		shown[i].Num = i + 1
		shown[i].Current = shown[i].ChatID != "" && shown[i].ChatID == c.current.ChatID
		lines = append(lines, formatRef(shown[i]))
	}
	return single(strings.Join(lines, "\n"))
}

// activateRef 把编号映射里的会话切为当前上下文：未激活的磁盘会话
//（ChatID 空、SessionID 非空）走 OpenSession 激活，已激活的走 SwitchTo。
// 成功后 current 更新为实际可投递的 ref（OpenSession 返回新 ChatID）。
func (c *Console) activateRef(ref SessionRef) error {
	if ref.ChatID == "" && ref.SessionID != "" {
		newRef, err := c.ex.OpenSession(ref.SessionID)
		if err != nil {
			return err
		}
		if newRef.ChatID != "" {
			ref = newRef // 用激活后的可投递引用覆盖（含新 ChatID）
		}
	} else if err := c.ex.SwitchTo(ref.ChatID); err != nil {
		return err
	}
	c.current = ref
	c.hasCurrent = true
	return nil
}

// switchByNumber 处理无菜单时的纯数字输入：按上次列表映射切换。
func (c *Console) switchByNumber(n int) []Reply {
	ref, ok := c.refByNum(n)
	if !ok {
		return single(msgNoMapping)
	}
	if err := c.activateRef(ref); err != nil {
		return single(fmt.Sprintf(msgSwitchFailed, err))
	}
	return single(formatCreated("已切换", ref))
}

// refByNum 按编号查上次列表映射。
func (c *Console) refByNum(n int) (SessionRef, bool) {
	for _, r := range c.lastList {
		if r.Num == n {
			return r, true
		}
	}
	return SessionRef{}, false
}

// handleEasyNew 快速新建。
func (c *Console) handleEasyNew() []Reply {
	ref, err := c.ex.EasyNew()
	if err != nil {
		return single(fmt.Sprintf(msgCreateFailed, err))
	}
	c.current = ref
	c.hasCurrent = true
	return single(formatCreated("已创建", ref))
}

// handleNewMenu 进入 #n 分步新建菜单 step1。
func (c *Console) handleNewMenu() []Reply {
	tools, _, _ := c.ex.NewMenuOptions()
	if len(tools) == 0 {
		return single(fmt.Sprintf(msgCreateFailed, "无可用工具"))
	}
	c.menu = &pendingMenu{kind: menuNew, step: 1, options: tools}
	return c.renderOptions("选择工具:", tools)
}

// handleMenuNumber 处理活跃菜单里的数字选择。
func (c *Console) handleMenuNumber(n int) []Reply {
	menu := c.menu
	if n < 1 || n > len(menu.options) {
		return single(msgNoMapping)
	}
	opt := menu.options[n-1]

	switch menu.kind {
	case menuNew:
		switch menu.step {
		case 1: // 已选工具 → 选项目
			menu.toolID = opt.ID
			menu.toolLabel = opt.Label
			_, workspaces, _ := c.ex.NewMenuOptions()
			if len(workspaces) == 0 {
				c.menu = nil
				return single(fmt.Sprintf(msgCreateFailed, "无可用工作区"))
			}
			menu.step = 2
			menu.options = workspaces
			return c.renderOptions("选择项目:", workspaces)
		case 2: // 已选项目 → 有模型列表则选模型，否则直接创建
			menu.wsID = opt.ID
			_, _, models := c.ex.NewMenuOptions()
			if len(models) == 0 {
				return c.finishCreate(menu, "")
			}
			menu.step = 3
			menu.options = models
			return c.renderOptions("选择模型:", models)
		default: // step3：已选模型 → 创建
			return c.finishCreate(menu, opt.ID)
		}
	case menuModels:
		c.menu = nil
		if err := c.ex.SetModel(menu.targetChatID, opt.ID); err != nil {
			return single(fmt.Sprintf(msgSwitchFailed, err))
		}
		return single("已切换模型: " + opt.Label)
	case menuSearch:
		c.menu = nil
		ref := menu.results[n-1]
		if err := c.activateRef(ref); err != nil {
			return single(fmt.Sprintf(msgSwitchFailed, err))
		}
		return single(formatCreated("已切换", ref))
	}
	c.menu = nil
	return single(msgNoMapping)
}

// finishCreate 用菜单里累积的参数创建会话；无论成败都清菜单。
func (c *Console) finishCreate(menu *pendingMenu, model string) []Reply {
	c.menu = nil
	ref, err := c.ex.CreateNew(menu.toolID, menu.wsID, model)
	if err != nil {
		return single(fmt.Sprintf(msgCreateFailed, err))
	}
	c.current = ref
	c.hasCurrent = true
	return single(formatCreated("已创建", ref))
}

// handleCancel 取消进行中的选择。
func (c *Console) handleCancel() []Reply {
	if c.menu == nil {
		return single(msgNothingToCancel)
	}
	c.menu = nil
	return single(msgCancelled)
}

// handleStop 中断当前会话。
func (c *Console) handleStop() []Reply {
	if !c.hasActiveSession() {
		return single(msgNoCurrent)
	}
	target := c.current.ChatID
	if target == "" {
		target = c.current.SessionID
	}
	if err := c.ex.Stop(target); err != nil {
		return single(fmt.Sprintf(msgSendFailed, err))
	}
	return single(msgStopped)
}

// handleModelsMenu 进入 #m 模型切换菜单。
func (c *Console) handleModelsMenu() []Reply {
	if !c.hasActiveSession() {
		return single(msgNoCurrent)
	}
	chatID := c.current.ChatID
	if chatID == "" {
		chatID = c.current.SessionID
	}
	models, curID, err := c.ex.ListModels(chatID)
	if err != nil {
		return single(fmt.Sprintf(msgSwitchFailed, err))
	}
	if len(models) == 0 {
		return single("该会话无可选模型")
	}
	c.menu = &pendingMenu{kind: menuModels, options: models, targetChatID: chatID}
	return c.renderOptionsMarked("选择模型:", models, curID)
}

// handleG 处理 "#g <n> <text>"：向编号 n 的会话发送文本（不切 current）。
func (c *Console) handleG(arg string) []Reply {
	nStr, text := splitCmd(arg)
	if nStr == "" || text == "" {
		return single(msgUsageG)
	}
	n, err := strconv.Atoi(nStr)
	if err != nil || n < 1 {
		return single(msgUsageG)
	}
	ref, ok := c.refByNum(n)
	if !ok {
		return single(msgNoMapping)
	}
	if err := c.ex.Send(ref.ChatID, text); err != nil {
		return single(fmt.Sprintf(msgSendFailed, err))
	}
	return single("已发送到: " + ref.Title)
}

// handleSearch 处理 "#f <kw>"：搜索并进入结果选择菜单。
func (c *Console) handleSearch(arg string) []Reply {
	if arg == "" {
		return single(msgUsageF)
	}
	results := c.ex.SearchSessions(arg)
	if len(results) == 0 {
		return single(msgSearchNoMatch)
	}
	c.refreshCurrent() // 与 #l 一致：先同步 current 再比对 ▶ 标记
	for i := range results {
		results[i].Num = i + 1
		results[i].Current = results[i].ChatID != "" && results[i].ChatID == c.current.ChatID
	}
	c.lastList = results // 搜索结果同样编号，供菜单数字与后续切换共用
	c.menu = &pendingMenu{kind: menuSearch, results: results}
	// options 与 results 同序同长，供 handleMenuNumber 的编号边界检查共用。
	menu := c.menu
	menu.options = make([]MenuOption, len(results))
	lines := make([]string, 0, len(results))
	for _, r := range results {
		lines = append(lines, formatRef(r))
	}
	return single(strings.Join(lines, "\n"))
}

// handleStatus 文本化状态快照。
func (c *Console) handleStatus() []Reply {
	st := c.ex.Status()
	return single(fmt.Sprintf("当前会话: %s\n运行中任务: %d\n未送达消息: %d", st.CurrentTitle, st.Running, st.Undelivered))
}

// renderOptions 渲染编号选项列表。
func (c *Console) renderOptions(title string, opts []MenuOption) []Reply {
	lines := make([]string, 0, len(opts)+1)
	lines = append(lines, title)
	for i, o := range opts {
		lines = append(lines, itoa(i+1)+". "+o.Label)
	}
	return single(strings.Join(lines, "\n"))
}

// renderOptionsMarked 同 renderOptions，但当前项（curID）标 ▶。
func (c *Console) renderOptionsMarked(title string, opts []MenuOption, curID string) []Reply {
	lines := make([]string, 0, len(opts)+1)
	lines = append(lines, title)
	for i, o := range opts {
		mark := ""
		if o.ID == curID {
			mark = "▶"
		}
		lines = append(lines, mark+itoa(i+1)+". "+o.Label)
	}
	return single(strings.Join(lines, "\n"))
}

// sendToCurrent 把普通文本发给当前会话；成功不回显（agent 流程会回复）。
func (c *Console) sendToCurrent(text string) []Reply {
	if !c.hasActiveSession() {
		return single(msgNoCurrentList)
	}
	if err := c.ex.Send(c.current.ChatID, text); err != nil {
		return single(fmt.Sprintf(msgSendFailed, err))
	}
	return nil
}

// single 构造单条回复。
func single(text string) []Reply {
	return []Reply{{Text: text}}
}
