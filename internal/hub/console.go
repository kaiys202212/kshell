package hub

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// itoa 供 commands.go / console.go 共用的整数转字符串。
func itoa(n int) string { return strconv.Itoa(n) }

// menuKind 区分活跃菜单类型。
type menuKind int

const (
	menuNew    menuKind = iota // #n 分步新建：选工具→项目→模型
	menuModels                 // #m 切换模型
	menuSearch                 // #f 搜索结果选择
	menuPick                   // 无精确目标：列会话等用户选，正文暂存
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
	// pick 菜单字段：正文在用户回编号后立即发送
	pendingText string
}

// Reply 是一条要回显给用户的回复。
type Reply struct {
	Text string
	// Sent 标记该回复对应一次成功投递（当前仅待选编号发送）。微信桥据此
	// 消费待转交附件队列：附件说明已随暂存正文发出，不得在下一条消息重复拼入。
	Sent bool `json:",omitempty"`
}

// Console 是一个渠道（微信/桌面端 AgentHub）的命令会话状态机。
// 每个渠道持自己的实例，互不影响。
type Console struct {
	// mu 保护以下状态字段：微信轮询 goroutine（HandleInbound → Handle）与
	// chat 回调/装配 goroutine（OpenPermissionMenu/SetCurrent）并发访问。
	// Executor 调用期间持锁可接受：回调需再进入 Console 的路径（如桌面端
	// 发送成功后 clearHubPendingPick → TryCancelPick）必须用 TryLock 放弃，
	// 不得同步阻塞等锁（Handle 全程持锁，阻塞即死锁）。
	mu sync.Mutex
	ex Executor
	// lang 是文案语言（"zh-CN" 中文，其余英文），创建后只读。
	lang    string
	current SessionRef
	// hasCurrent 区分「确无当前会话」与「还没向后端问过」：零值哨兵无法区分。
	hasCurrent bool
	// currentMark 是当前上下文采纳时的 hub 归因消息 ID（水位）：只有更新的
	// 归因才覆盖 current，显式切换（#l/#f 选号、新建）把水位抬到当下。
	currentMark int64
	menu        *pendingMenu
	perm       *pendingPermission
	// lastList 是最近一次列表/搜索的编号映射（纯数字与 #g 依据它路由）。
	lastList []SessionRef
	// lastRecent 是最近一次 #h 列表的消息快照（#h <序号> 查全文依据它索引）。
	lastRecent []Message
}

// pendingPermission 是权限请求的选择状态（1 允许 / 2 拒绝）。
type pendingPermission struct {
	chatID string
	reqID  string
	desc   string // 权限请求描述；本期不回显，留作后续展示
}

// NewConsole 创建命令状态机（默认中文文案）。
func NewConsole(ex Executor) *Console {
	return NewConsoleLang(ex, "zh-CN")
}

// NewConsoleLang 创建指定文案语言的命令状态机（"zh-CN" 中文，其余英文）。
func NewConsoleLang(ex Executor, lang string) *Console {
	return &Console{ex: ex, lang: lang}
}

// SetCurrent 显式设置当前会话上下文（渠道初始化时由装配方调用：
// 微信桥用配置里记住的上次会话，桌面端用 hub 最后活跃会话）。
func (c *Console) SetCurrent(ref SessionRef) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = ref
	c.hasCurrent = true
}

// CurrentTitle 返回当前会话标题快照（微信桥 ack 回退归因用；无活跃会话为
// 空串）。加锁读：与 mu 保护声明一致，防未来装配路径并发调用成真竞争。
func (c *Console) CurrentTitle() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current.Title
}

// OpenPermissionMenu 由渠道层在收到权限请求事件时调用（微信桥/桌面端均可），
// 之后的「1」/「2」输入被解释为允许/拒绝；回显由调用方处理。权限菜单
// 只拦截数字回复，其余输入照常走命令与转发流程。
func (c *Console) OpenPermissionMenu(chatID, reqID, desc string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.perm = &pendingPermission{chatID: chatID, reqID: reqID, desc: desc}
}

// Handle 处理一条用户输入，返回要回显的回复（可能多条，按序展示）。
// 全程持锁：入站轮询与其他渠道的装配/权限事件并发时保证状态一致。
func (c *Console) Handle(input string) []Reply {
	c.mu.Lock()
	defer c.mu.Unlock()
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}
	input = normalizeInput(input)

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
		return single(c.text("pick_number"))
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
		return single(c.text("perm_hint"))
	}
	c.perm = nil
	if err := c.ex.GrantPermission(perm.chatID, perm.reqID, allow); err != nil {
		// 请求可能已被桌面端处理，失败一律按失效提示。
		return single(c.text("perm_stale"))
	}
	return single(c.text("perm_done"))
}

// handleCommand 分发 # 开头的命令。
func (c *Console) handleCommand(input string) []Reply {
	// #g / #f 是带参数命令，其余先按单词切。
	cmd, arg := splitCmd(input)
	// 除 #c（显式取消并回文案）外，任何命令都让待选正文失效：用户已转向
	// 其他操作，残留菜单会把后续纯数字误解成选会话并发送旧正文。
	if cmd != "#c" && c.menu != nil && c.menu.kind == menuPick {
		c.menu = nil
	}
	switch cmd {
	case "#?":
		return c.handleHelp()
	case "#？": // 全角问号：手机输入法常把 ? 打成 ？（2026-10-18 用户反馈）
		return c.handleHelp()
	case "#l":
		return c.handleList(false)
	case "#la":
		return c.handleList(true)
	case "#en":
		return c.handleEasyNew(arg)
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
	case "#h":
		return c.handleRecent(arg)
	default:
		return single(c.text("unknown_cmd"))
	}
}

// splitCmd 把 "#g 1 hi" 切成 cmd="#g"、arg="1 hi"；无参数时 arg 为空。
// 已知命令都是无空格的短词，取第一个空白前的段；全角冒号（如 "#g：1 hi"）
// 同样视作命令词与参数的分隔（多字节，按完整 rune 长度跳过）。
func splitCmd(input string) (cmd, arg string) {
	si := strings.IndexAny(input, " \t")
	ci := strings.Index(input, "：")
	i, sep := si, 1
	if ci >= 0 && (si < 0 || ci < si) {
		i, sep = ci, len("：")
	}
	if i < 0 {
		return input, ""
	}
	return input[:i], strings.TrimSpace(input[i+sep:])
}

// knownCommands 是全角＃提升为命令前缀的白名单：只有 ＃ 后首词命中已知
// 命令才视为命令意图，避免劫持以全角＃开头的普通文本（发往当前会话）。
var knownCommands = map[string]bool{
	"?": true, "？": true, "l": true, "la": true, "en": true, "n": true, "s": true,
	"m": true, "g": true, "f": true, "st": true, "c": true, "h": true,
}

// normalizeInput 归一化全角输入（手机输入法常把符号打成全角）：
//   - 全角空格 → 半角空格（全局替换，普通文本里无歧义）
//   - 纯全角数字串 → 半角（编号切换 / 菜单与权限回复）
//   - 全角＃ → 半角 #（仅当 ＃ 后首词是已知命令时；带参数命令的编号后的
//     全角冒号由 handleG 消化，命令词后的冒号由 splitCmd 消化）
func normalizeInput(input string) string {
	input = strings.ReplaceAll(input, "　", " ")
	if input != "" {
		allDigits := true
		for _, r := range input {
			if r < '０' || r > '９' {
				allDigits = false
				break
			}
		}
		if allDigits {
			return normalizeDigits(input)
		}
	}
	if rest, ok := strings.CutPrefix(input, "＃"); ok {
		token := rest
		if i := strings.IndexAny(rest, " \t："); i >= 0 {
			token = rest[:i]
		}
		if knownCommands[token] {
			return "#" + rest
		}
	}
	return input
}

// normalizeDigits 把字符串里的全角数字 ０-９ 转成半角 0-9，其余字符原样保留。
func normalizeDigits(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '０' && r <= '９' {
			r = '0' + (r - '０')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// handleHelp 回复帮助全文与当前会话行（help 本身是 Markdown，段落间空行）。
func (c *Console) handleHelp() []Reply {
	var sb strings.Builder
	sb.WriteString(c.text("help"))
	sb.WriteString("\n\n" + fmt.Sprintf(c.text("current_session"), ""))
	if !c.hasActiveSession() {
		sb.WriteString(c.text("none"))
	} else {
		sb.WriteString(fmt.Sprintf("%s (%s)", c.current.Title, c.current.Tool))
	}
	return single(sb.String())
}

// refreshCurrent 从执行后端同步最新归因。只有比水位更新的 hub 归因才覆盖
// current：显式切换（选号/新建）把水位抬到当下，避免旧消息把目标抢回去。
func (c *Console) refreshCurrent() {
	id, ref, ok := c.ex.LastActivity()
	if !ok {
		return
	}
	if c.hasCurrent && id <= c.currentMark {
		return
	}
	c.current = ref
	c.hasCurrent = true
	c.currentMark = id
}

// markCurrent 把归因水位抬到后端当前进度：显式选择发生在此刻之前的历史
// 归因不再有资格覆盖它。
func (c *Console) markCurrent() {
	if id, _, ok := c.ex.LastActivity(); ok && id > c.currentMark {
		c.currentMark = id
	}
}

// hasActiveSession 报告当前是否有活跃会话（每次都向后端同步归因）。
func (c *Console) hasActiveSession() bool {
	c.refreshCurrent()
	return c.hasCurrent
}

// handleList 列出会话；all=false 只取前 10。同时刷新编号映射与 current。
func (c *Console) handleList(all bool) []Reply {
	sessions := c.ex.ListSessions()
	if len(sessions) == 0 {
		return single(c.text("no_sessions"))
	}
	limit := len(sessions)
	if !all && limit > 10 {
		limit = 10
	}
	shown := sessions[:limit]
	c.lastList = shown
	c.refreshCurrent()

	lines := make([]string, 0, limit+1)
	for i := range shown {
		shown[i].Num = i + 1
		shown[i].Current = shown[i].ChatID != "" && shown[i].ChatID == c.current.ChatID
		lines = append(lines, formatRef(shown[i]))
	}
	// 引导提示：列表本身不说明数字的用途，新用户不知道可以直接回数字切换。
	// 提示与列表间用空行分隔，防 Markdown 懒续行把提示吞进最后一个列表项。
	return single(strings.Join([]string{strings.Join(lines, "\n"), c.text("list_hint")}, "\n\n"))
}

// activateRef 把编号映射里的会话切为当前上下文：未激活的磁盘会话
// （ChatID 空、SessionID 非空）走 OpenSession 激活，已激活的走 SwitchTo。
// 成功后 current 更新为实际可投递的 ref（OpenSession 返回新 ChatID），
// 并把归因水位抬到当下（显式选择优先于旧归因）。
func (c *Console) activateRef(ref SessionRef) (SessionRef, error) {
	if ref.ChatID == "" && ref.SessionID != "" {
		newRef, err := c.ex.OpenSession(ref.SessionID)
		if err != nil {
			return SessionRef{}, err
		}
		if newRef.ChatID != "" {
			ref = newRef // 用激活后的可投递引用覆盖（含新 ChatID）
		}
	} else if err := c.ex.SwitchTo(ref.ChatID); err != nil {
		return SessionRef{}, err
	}
	c.current = ref
	c.hasCurrent = true
	c.markCurrent()
	return ref, nil
}

// switchByNumber 处理无菜单时的纯数字输入：按上次列表映射切换。
func (c *Console) switchByNumber(n int) []Reply {
	ref, ok := c.refByNum(n)
	if !ok {
		return single(c.text("no_mapping"))
	}
	ref, err := c.activateRef(ref)
	if err != nil {
		return single(fmt.Sprintf(c.text("switch_failed"), err))
	}
	return single(formatSwitched(c.text("switched"), ref))
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

// handleEasyNew 快速新建。arg 非空时（"#en <消息>"）创建成功后把消息自动
// 发给新会话：CreateNew 返回即已完成握手，SendChatPrompt 对仍在启动的会话
// 也有入队兜底，agent 稍后回复并经 hub 镜像回微信。发送失败在欢迎词后追加
// 发送失败提示。
func (c *Console) handleEasyNew(arg string) []Reply {
	ref, err := c.ex.EasyNew()
	if err != nil {
		return single(fmt.Sprintf(c.text("create_failed"), err))
	}
	c.current = ref
	c.hasCurrent = true
	c.markCurrent()
	replies := []Reply{{Text: c.welcomeReply(ref)}}
	if arg != "" && ref.ChatID != "" {
		if err := c.ex.Send(ref.ChatID, arg); err != nil {
			replies = append(replies, Reply{Text: fmt.Sprintf(c.text("send_failed"), err)})
		}
	}
	return replies
}

// handleNewMenu 进入 #n 分步新建菜单 step1。
func (c *Console) handleNewMenu() []Reply {
	tools, _, _ := c.ex.NewMenuOptions()
	if len(tools) == 0 {
		return single(fmt.Sprintf(c.text("create_failed"), c.text("no_tools")))
	}
	c.menu = &pendingMenu{kind: menuNew, step: 1, options: tools}
	return c.renderOptions(c.text("pick_tool"), tools)
}

// handleMenuNumber 处理活跃菜单里的数字选择。
func (c *Console) handleMenuNumber(n int) []Reply {
	menu := c.menu
	if n < 1 || n > len(menu.options) {
		return single(c.text("no_mapping"))
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
				return single(fmt.Sprintf(c.text("create_failed"), c.text("no_workspaces")))
			}
			menu.step = 2
			menu.options = workspaces
			return c.renderOptions(c.text("pick_workspace"), workspaces)
		case 2: // 已选项目 → 有模型列表则选模型，否则直接创建
			menu.wsID = opt.ID
			_, _, models := c.ex.NewMenuOptions()
			if len(models) == 0 {
				return c.finishCreate(menu, "")
			}
			menu.step = 3
			menu.options = models
			return c.renderOptions(c.text("pick_model"), models)
		default: // step3：已选模型 → 创建
			return c.finishCreate(menu, opt.ID)
		}
	case menuModels:
		c.menu = nil
		if err := c.ex.SetModel(menu.targetChatID, opt.ID); err != nil {
			return single(fmt.Sprintf(c.text("switch_failed"), err))
		}
		return single(fmt.Sprintf(c.text("model_switched"), opt.Label))
	case menuSearch:
		c.menu = nil
		ref, err := c.activateRef(menu.results[n-1])
		if err != nil {
			return single(fmt.Sprintf(c.text("switch_failed"), err))
		}
		return single(formatSwitched(c.text("switched"), ref))
	case menuPick:
		// 先清菜单：激活/发送失败也不保留待发正文，避免静默改投。
		c.menu = nil
		ref, err := c.activateRef(menu.results[n-1])
		if err != nil {
			return single(fmt.Sprintf(c.text("switch_failed"), err))
		}
		if err := c.ex.Send(ref.ChatID, menu.pendingText); err != nil {
			return single(fmt.Sprintf(c.text("send_failed"), err))
		}
		return []Reply{{Text: fmt.Sprintf(c.text("sent_to"), sessionTag(ref.Title)), Sent: true}}
	}
	c.menu = nil
	return single(c.text("no_mapping"))
}

// finishCreate 用菜单里累积的参数创建会话；无论成败都清菜单。
func (c *Console) finishCreate(menu *pendingMenu, model string) []Reply {
	c.menu = nil
	ref, err := c.ex.CreateNew(menu.toolID, menu.wsID, model)
	if err != nil {
		return single(fmt.Sprintf(c.text("create_failed"), err))
	}
	c.current = ref
	c.hasCurrent = true
	c.markCurrent()
	return single(c.welcomeReply(ref))
}

// handleCancel 取消进行中的选择。
func (c *Console) handleCancel() []Reply {
	if c.menu == nil {
		return single(c.text("nothing_to_cancel"))
	}
	c.menu = nil
	return single(c.text("cancelled"))
}

// handleStop 中断当前会话。ChatID 空（未激活的磁盘会话）没有运行中的
// 任务，直接按无活跃会话提示；SessionID 不能传给 ex.Stop（CancelChat 按
// chat ID 解析，必报错）。
func (c *Console) handleStop() []Reply {
	if !c.hasActiveSession() || c.current.ChatID == "" {
		return single(c.text("no_current"))
	}
	if err := c.ex.Stop(c.current.ChatID); err != nil {
		return single(fmt.Sprintf(c.text("send_failed"), err))
	}
	return single(c.text("stopped"))
}

// handleModelsMenu 进入 #m 模型切换菜单。
func (c *Console) handleModelsMenu() []Reply {
	if !c.hasActiveSession() {
		return single(c.text("no_current"))
	}
	chatID := c.current.ChatID
	if chatID == "" {
		chatID = c.current.SessionID
	}
	models, curID, err := c.ex.ListModels(chatID)
	if err != nil {
		return single(fmt.Sprintf(c.text("switch_failed"), err))
	}
	if len(models) == 0 {
		return single(c.text("no_models"))
	}
	c.menu = &pendingMenu{kind: menuModels, options: models, targetChatID: chatID}
	return c.renderOptionsMarked(c.text("pick_model"), models, curID)
}

// handleG 处理 "#g <n> <text>"：向编号 n 的会话发送文本（不切 current）。
// 编号兼容全角数字；编号后允许紧跟一个全角/半角冒号作分隔（"#g 1：hi"），
// 正文里的冒号不受影响（只在编号后消化一个）。
func (c *Console) handleG(arg string) []Reply {
	i := 0
	for i < len(arg) {
		r, size := utf8.DecodeRuneInString(arg[i:])
		if (r >= '0' && r <= '9') || (r >= '０' && r <= '９') {
			i += size
			continue
		}
		break
	}
	nStr := normalizeDigits(arg[:i])
	text := strings.TrimSpace(arg[i:])
	text = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(text, "："), ":"))
	if nStr == "" || text == "" {
		return single(c.text("usage_g"))
	}
	n, err := strconv.Atoi(nStr)
	if err != nil || n < 1 {
		return single(c.text("usage_g"))
	}
	ref, ok := c.refByNum(n)
	if !ok {
		return single(c.text("no_mapping"))
	}
	if err := c.ex.Send(ref.ChatID, text); err != nil {
		return single(fmt.Sprintf(c.text("send_failed"), err))
	}
	return single(fmt.Sprintf(c.text("sent_to"), sessionTag(ref.Title)))
}

// handleSearch 处理 "#f <kw>"：搜索并进入结果选择菜单。
func (c *Console) handleSearch(arg string) []Reply {
	if arg == "" {
		return single(c.text("usage_f"))
	}
	results := c.ex.SearchSessions(arg)
	if len(results) == 0 {
		return single(c.text("search_no_match"))
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
	lines := make([]string, 0, len(results)+1)
	for _, r := range results {
		lines = append(lines, formatRef(r))
	}
	// 搜索结果同样靠数字选择，补一条引导（与菜单选择共用 pick_number 文案），
	// 空行分隔防 Markdown 懒续行。
	return single(strings.Join([]string{strings.Join(lines, "\n"), c.text("pick_number")}, "\n\n"))
}

// handleStatus 文本化状态快照（无序列表，按 Markdown 渲染不挤行）。
func (c *Console) handleStatus() []Reply {
	st := c.ex.Status()
	return single(fmt.Sprintf("- "+c.text("current_session")+"\n- "+c.text("status_running")+"\n- "+c.text("status_undelivered"),
		st.CurrentTitle, st.Running, st.Undelivered))
}

// #h 的条数默认值（列表固定取最近 N 条）与单条正文截断上限（rune 数）。
const (
	recentDefault = 10
	// recentTextCap 是列表里单条消息正文的最大回显长度，超长截断加省略号；
	// #h <序号> 查全文不受此限（微信端另有 truncateForWeixin 兜底）。
	recentTextCap = 80
)

// handleRecent 处理 "#h" 与 "#h <序号>"：无参回显 AgentHub 最近 10 条消息
//（存快照供序号索引）；带数字序号回显上次列表第 N 条的完整正文，无快照时
// 先自动取最近一批。条数参数已砍（2026-10-19 用户决策：#h <n> 语义让位序号）。
func (c *Console) handleRecent(arg string) []Reply {
	if arg == "" {
		msgs := c.ex.RecentMessages(recentDefault)
		if len(msgs) == 0 {
			return single(c.text("no_messages"))
		}
		c.lastRecent = msgs
		return single(c.renderRecentList(msgs))
	}
	n, err := strconv.Atoi(normalizeDigits(arg))
	if err != nil || n < 1 {
		return single(c.text("usage_h"))
	}
	if len(c.lastRecent) == 0 {
		if msgs := c.ex.RecentMessages(recentDefault); len(msgs) > 0 {
			c.lastRecent = msgs
		}
	}
	if len(c.lastRecent) == 0 {
		return single(c.text("no_messages"))
	}
	if n > len(c.lastRecent) {
		return single(c.text("recent_no_mapping"))
	}
	// 详情正文原样拼接：消息本身即 Markdown，行首 #/** 等按 Markdown 渲染是
	// 预期行为（与 AgentHub 气泡一致），不做转义（已知例外，评审 M-2）。
	m := c.lastRecent[n-1]
	role := c.text("msg_agent")
	if m.Kind == "user" {
		role = c.text("msg_user")
	}
	return single(fmt.Sprintf(c.text("msg_detail"), n, role, hubMsgTitle(m)) + "\n\n" + m.Text)
}

// renderRecentList 渲染 #h 列表：加粗表头 + 空行 + 有序列表（1=最旧），
// 单条正文按 rune 截断防刷屏。
func (c *Console) renderRecentList(msgs []Message) string {
	roleUser := c.text("msg_user")
	lines := make([]string, 0, len(msgs))
	for i, m := range msgs {
		role := c.text("msg_agent")
		if m.Kind == "user" {
			role = roleUser
		}
		lines = append(lines, itoa(i+1)+". ["+role+"]["+hubMsgTitle(m)+"] "+truncateOneLine(m.Text, recentTextCap))
	}
	return fmt.Sprintf(c.text("recent_header"), len(msgs)) + "\n\n" + strings.Join(lines, "\n")
}

// renderOptions 渲染编号选项列表（标题与列表间空行，Markdown 段落分隔）。
func (c *Console) renderOptions(title string, opts []MenuOption) []Reply {
	lines := make([]string, 0, len(opts))
	for i, o := range opts {
		lines = append(lines, itoa(i+1)+". "+o.Label)
	}
	return single(title + "\n\n" + strings.Join(lines, "\n"))
}

// renderOptionsMarked 同 renderOptions，但当前项（curID）在编号后标 ▶
//（行首 ▶ 会打断 Markdown 有序列表解析）。
func (c *Console) renderOptionsMarked(title string, opts []MenuOption, curID string) []Reply {
	lines := make([]string, 0, len(opts))
	for i, o := range opts {
		mark := ""
		if o.ID == curID {
			mark = "▶ "
		}
		lines = append(lines, itoa(i+1)+". "+mark+o.Label)
	}
	return single(title + "\n\n" + strings.Join(lines, "\n"))
}

// sendToCurrent 把普通文本发给精确命中的当前会话；未激活的磁盘会话先
// OpenSession 激活。无精确目标时进入待选列表，由用户回编号后发送。
func (c *Console) sendToCurrent(text string) []Reply {
	c.refreshCurrent()
	ref := c.current
	if !c.hasCurrent || (ref.ChatID == "" && ref.SessionID == "") {
		replies, _ := c.listForPending(text)
		return replies
	}
	if ref.ChatID == "" {
		activated, err := c.activateRef(ref)
		if err != nil {
			return single(fmt.Sprintf(c.text("switch_failed"), err))
		}
		ref = activated
	}
	if ref.ChatID == "" {
		replies, _ := c.listForPending(text)
		return replies
	}
	if err := c.ex.Send(ref.ChatID, text); err != nil {
		return single(fmt.Sprintf(c.text("send_failed"), err))
	}
	return nil
}

// listForPending 列出前 10 个会话并暂存正文，等用户回编号后立即发送。
// 与 #l 共用编号映射（lastList），#c 取消时随菜单丢弃正文。
// 第二个返回值报告是否建立了待选菜单（无会话时为 false）。
func (c *Console) listForPending(text string) ([]Reply, bool) {
	sessions := c.ex.ListSessions()
	if len(sessions) == 0 {
		return single(c.text("no_sessions")), false
	}
	limit := len(sessions)
	if limit > 10 {
		limit = 10
	}
	shown := sessions[:limit]
	for i := range shown {
		shown[i].Num = i + 1
		shown[i].Current = false // 无精确目标：不标当前
	}
	c.lastList = shown
	c.menu = &pendingMenu{
		kind:        menuPick,
		options:     make([]MenuOption, len(shown)),
		results:     shown,
		pendingText: text,
	}
	lines := make([]string, 0, limit)
	for _, r := range shown {
		lines = append(lines, formatRef(r))
	}
	return single(strings.Join([]string{
		c.text("pick_target"),
		strings.Join(lines, "\n"),
		c.text("pick_number"),
	}, "\n\n")), true
}

// PickTarget 供 AgentHub 在无精确目标时直接进入待选列表（暂存正文）。
// 只 trim，不做命令归一化：正文必须原样投递。第二个返回值报告是否建立
// 待选菜单（无会话/当前有其他选择时为 false，调用方应保留用户草稿）。
func (c *Console) PickTarget(text string) ([]Reply, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, false
	}
	// 其他菜单（#n/#m/#f）进行中：不覆盖，避免静默丢失用户已做的选择。
	if c.menu != nil && c.menu.kind != menuPick {
		return single(c.text("pick_busy")), false
	}
	return c.listForPending(text)
}

// TryCancelPick 让待选菜单失效（AgentHub 已经另行投递）。仅清理 menuPick，
// 不碰其他选择菜单；lastList 保留，后续数字仍可按最近列表切换。锁被占用
//（Console.Handle 全程持锁回调 Executor 发送）时返回 false 并放弃：该链路
// 任一 ex.Send 调用点进入前 menu 必非 menuPick，放弃取消无损语义。
func (c *Console) TryCancelPick() bool {
	if !c.mu.TryLock() {
		return false
	}
	defer c.mu.Unlock()
	if c.menu != nil && c.menu.kind == menuPick {
		c.menu = nil
	}
	return true
}

// single 构造单条回复。
func single(text string) []Reply {
	return []Reply{{Text: text}}
}
