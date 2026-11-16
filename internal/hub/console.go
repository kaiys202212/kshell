package hub

import (
	"fmt"
	"sort"
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
	menu *pendingMenu
	perm *pendingPermission
	// agg 是群发回执聚合策略（mu 保护）：#g 多目标进入聚合，精确投递（单发/
	// 直投/引用/待选移交）退出。默认自建实例；多渠道可经 SetMirrorPolicy
	// 共享同一份。注意：*MirrorPolicy 指向的策略实例自身非线程安全，共享方
	// 须保证串行访问；这里加锁只保护指针本身的读写。
	agg *MirrorPolicy
	// lastList 是最近一次列表/搜索的编号映射（纯数字与 #g 依据它路由）。
	lastList []SessionRef
	// lastRecent 是最近一次 #h 列表的消息快照（#h <序号> 查全文依据它索引）。
	lastRecent []Message
	// digitRecent 为 true 时，无菜单纯数字按 lastRecent 查全文（#h 语境）；
	// false 时按 lastList 切会话（#l/#la/#f/待选列表语境）。默认 false。
	digitRecent bool
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
	return &Console{ex: ex, lang: lang, agg: NewMirrorPolicy()}
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

// MirrorPolicy 取出共享聚合策略（渠道装配时把多渠道 Console 指到同一实例）。
// 加锁读：与 mu 保护的声明一致，防未来并发调用成真竞争。
func (c *Console) MirrorPolicy() *MirrorPolicy {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.agg
}

// Busy 报告 Console 是否有未决交互（待选菜单/权限菜单/新建向导等）：
// 路由层据此让路——菜单流期间输入一律走既有确定性路径，不被智能路由接管。
func (c *Console) Busy() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.menu != nil || c.perm != nil
}

// SetMirrorPolicy 共享替换（nil 拒绝，保持自建兜底）。
func (c *Console) SetMirrorPolicy(p *MirrorPolicy) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if p != nil {
		c.agg = p
	}
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

	// 纯数字：菜单选择；否则按数字语境（#h→查全文 / #l 等→切会话）。
	if n, err := strconv.Atoi(input); err == nil && n > 0 {
		if c.menu != nil {
			return c.handleMenuNumber(n)
		}
		if c.digitRecent {
			return c.handleRecent(input)
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
	case "#gx":
		return c.handleGX(arg)
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
	"m": true, "g": true, "f": true, "st": true, "c": true, "h": true, "gx": true,
}

// normalizeInput 归一化全角输入（手机输入法常把符号打成全角）：
//   - 全角空格 → 半角空格（全局替换，普通文本里无歧义）
//   - 纯全角数字串 → 半角（编号切换 / 菜单与权限回复）
//   - 全角＃ → 半角 #（仅当 ＃ 后首词是已知命令时；带参数命令的编号后的
//     全角冒号由 handleG 消化，命令词后的冒号由 splitCmd 消化）
// IsBareNumber 判定输入是否为裸数字（正整数，全角数字归一化后）——与前端
// isHubNumber / Handle 的 strconv.Atoi+n>0 判定同口径。智能路由层据此分流：
// 反问语境下的数字是「回答编号」，非语境数字是「命令层菜单/编号切换」语义。
// 不代做 TrimSpace：调用方（HubSmartSend 入口）已保证无首尾空白。
func IsBareNumber(input string) bool {
	n, err := strconv.Atoi(normalizeDigits(input))
	return err == nil && n > 0
}

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

// sessionDeliverable 判定会话可否作为默认投递目标：须已激活 ACP（有 ChatID）
// 且非终端占用。未激活磁盘会话 / 终端会话可出现在待选列表，但不作默认 current。
func sessionDeliverable(ref SessionRef) bool {
	return ref.ChatID != "" && !ref.TerminalActive
}

// refreshCurrent 从执行后端同步最新归因。只有比水位更新的 hub 归因才覆盖
// current：显式切换（选号/新建）把水位抬到当下，避免旧消息把目标抢回去。
// 最新归因不可投递（未激活 / 终端）时清除 current，进入「无默认接收者」。
func (c *Console) refreshCurrent() {
	id, ref, ok := c.ex.LastActivity()
	if !ok {
		return
	}
	if c.hasCurrent && id <= c.currentMark {
		return
	}
	c.currentMark = id
	if !sessionDeliverable(ref) {
		c.hasCurrent = false
		c.current = SessionRef{}
		return
	}
	c.current = ref
	c.hasCurrent = true
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
	c.digitRecent = false // #l/#la 后纯数字切会话
	c.refreshCurrent()

	lines := make([]string, 0, limit+1)
	for i := range shown {
		shown[i].Num = i + 1
		shown[i].Current = shown[i].ChatID != "" && shown[i].ChatID == c.current.ChatID
		line := formatRef(shown[i])
		if shown[i].TerminalActive {
			// 终端运行中的会话：明确标记（规则2），保留在列表供认知。
			line += " 〔" + c.text("tag_terminal") + "〕"
		}
		lines = append(lines, line)
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
		return single(fmt.Sprintf(c.text("switch_failed"), c.errText(err)))
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
			return single(fmt.Sprintf(c.text("switch_failed"), c.errText(err)))
		}
		return single(fmt.Sprintf(c.text("model_switched"), opt.Label))
	case menuSearch:
		c.menu = nil
		ref, err := c.activateRef(menu.results[n-1])
		if err != nil {
			return single(fmt.Sprintf(c.text("switch_failed"), c.errText(err)))
		}
		return single(formatSwitched(c.text("switched"), ref))
	case menuPick:
		// 首项哨兵「新建」：转 #n 分步流程，暂存正文随身携带，
		// 创建成功后自动移交（finishCreateHandoff）。
		if n == 1 {
			tools, _, _ := c.ex.NewMenuOptions()
			if len(tools) == 0 {
				c.menu = nil
				return single(fmt.Sprintf(c.text("create_failed"), c.text("no_tools")))
			}
			menu.kind = menuNew
			menu.step = 1
			menu.options = tools
			return c.renderOptions(c.text("pick_tool"), tools)
		}
		// 先清菜单：激活/发送失败也不保留待发正文，避免静默改投。
		pending := menu.pendingText
		results := menu.results
		c.menu = nil
		if n < 2 || n > len(results)+1 {
			return single(c.text("no_mapping"))
		}
		ref, err := c.activateRef(results[n-2])
		if err != nil {
			return single(fmt.Sprintf(c.text("switch_failed"), c.errText(err)))
		}
		if err := c.ex.Send(ref.ChatID, pending); err != nil {
			return single(fmt.Sprintf(c.text("send_failed"), err))
		}
		// 待选编号移交成功属精确投递：退出群发聚合。
		c.agg.Discard()
		return []Reply{{Text: fmt.Sprintf(c.text("sent_to"), sessionTag(ref.Title)), Sent: true}}
	}
	c.menu = nil
	return single(c.text("no_mapping"))
}

// finishCreate 用菜单里累积的参数创建会话；无论成败都清菜单。
// 菜单带暂存正文（待选「新建」入口转入）时：创建成功后把正文发给新会话，
// 回复欢迎词 + 移交确认；失败丢弃正文（不误发到错误目标）。
func (c *Console) finishCreate(menu *pendingMenu, model string) []Reply {
	c.menu = nil
	ref, err := c.ex.CreateNew(menu.toolID, menu.wsID, model)
	if err != nil {
		return single(fmt.Sprintf(c.text("create_failed"), err))
	}
	c.current = ref
	c.hasCurrent = true
	c.markCurrent()
	replies := []Reply{{Text: c.welcomeReply(ref)}}
	pending := menu.pendingText
	if pending == "" {
		return replies
	}
	if ref.ChatID == "" {
		replies = append(replies, Reply{Text: fmt.Sprintf(c.text("send_failed"), c.text("no_current"))})
		return replies
	}
	if err := c.ex.Send(ref.ChatID, pending); err != nil {
		replies = append(replies, Reply{Text: fmt.Sprintf(c.text("send_failed"), err)})
		return replies
	}
	// 新建移交成功（带暂存正文发送）属精确投递：退出群发聚合。
	c.agg.Discard()
	replies = append(replies, Reply{
		Text: c.text("handoff_done"),
		Sent: true,
	})
	return replies
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
		return single(fmt.Sprintf(c.text("switch_failed"), c.errText(err)))
	}
	if len(models) == 0 {
		return single(c.text("no_models"))
	}
	c.menu = &pendingMenu{kind: menuModels, options: models, targetChatID: chatID}
	return c.renderOptionsMarked(c.text("pick_model"), models, curID)
}

// gTag 是群发回执里一个目标的紧凑标签 "N【标题】";标题缺失退裸编号。
func gTag(r SessionRef) string {
	if t := sessionTag(r.Title); t != "" {
		return itoa(r.Num) + t
	}
	return itoa(r.Num)
}

// handleG 处理 "#g <编号列表> <text>"。单编号保持旧语义(sent_to 单行回执、
// 不进聚合但清聚合一一单发属精确投递);多编号顺序逐目标激活+发送,全部编号
// 必须有效(任一无效整条拒绝回 usage 风格的 no_mapping/usage_g)。编号解析后
// 转调 sendGroupTextsLocked（与 SendGroupTexts 共用核心循环）：标识去重、
// 部分失败不中断、≥2 成功进入汇报聚合、不改 current/归因水位。
// 经 Handle 全程持锁调用，可直接使用 sendGroupTextsLocked。
func (c *Console) handleG(arg string) []Reply {
	nums, text, ok := c.parseGTargets(arg)
	if !ok {
		return single(c.text("usage_g"))
	}
	refs := make([]SessionRef, 0, len(nums))
	for _, n := range nums {
		ref, okN := c.refByNum(n)
		if !okN {
			return single(c.text("no_mapping"))
		}
		refs = append(refs, ref)
	}
	// #g 是同一正文群发给所有目标：texts 与 refs 等长逐项重复。
	texts := make([]string, len(refs))
	for i := range texts {
		texts[i] = text
	}
	return c.sendGroupTextsLocked(refs, texts)
}

// SendGroupTexts 按给定 refs 逐会话投递各自文本（智能路由的群发执行入口）。
// 语义与 #g 完全同契约：去重（SessionID 退 ChatID）、TerminalActive 拦截、
// 未激活先 OpenSession、部分失败不中断、≥2 成功进聚合、不改 current 不抬
// 水位。texts 与 refs 一一对应（长度必须相等；防御性按较短者钳制）。
// refs/texts 为空直接返回 nil（不产生回执）。
func (c *Console) SendGroupTexts(refs []SessionRef, texts []string) []Reply {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sendGroupTextsLocked(refs, texts)
}

// sendGroupTextsLocked 是 handleG 与 SendGroupTexts 共用的核心循环
// （从 handleG 抽取，调用方须持 c.mu）：逐目标去重→TerminalActive 拦截→
// OpenSession→Send→succRefs≥2 时 c.agg.Enter→回执组装（group_sent/
// group_failed/sent_to）。文案形态与抽取前 handleG 一字不差。
func (c *Console) sendGroupTextsLocked(refs []SessionRef, texts []string) []Reply {
	n := len(refs)
	if len(texts) < n {
		n = len(texts)
	}
	refs = refs[:n]
	texts = texts[:n]
	if n == 0 {
		return nil
	}
	// 按 SessionID(退 ChatID)去重,顺序保持调用方输入顺序（#g 通常为编号升序）。
	seen := map[string]bool{}
	uniq := make([]SessionRef, 0, n)
	uniqTexts := make([]string, 0, n)
	for i, r := range refs {
		key := r.SessionID
		if key == "" {
			key = r.ChatID
		}
		if key != "" {
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		uniq = append(uniq, r)
		uniqTexts = append(uniqTexts, texts[i])
	}
	var succRefs []SessionRef
	var sentTags, failParts []string
	for i, ref := range uniq {
		if ref.TerminalActive {
			failParts = append(failParts, gTag(ref)+" — "+c.text("err.hub.terminal_active"))
			continue
		}
		if ref.ChatID == "" && ref.SessionID != "" {
			opened, err := c.ex.OpenSession(ref.SessionID)
			if err != nil {
				failParts = append(failParts, gTag(ref)+" — "+c.errText(err))
				continue
			}
			if opened.ChatID == "" {
				failParts = append(failParts, gTag(ref)+" — "+c.text("no_mapping"))
				continue
			}
			ref = opened
		}
		if ref.ChatID == "" {
			return []Reply{{Text: c.text("no_mapping")}}
		}
		if err := c.ex.Send(ref.ChatID, uniqTexts[i]); err != nil {
			failParts = append(failParts, gTag(ref)+" — "+c.errText(err))
			continue
		}
		succRefs = append(succRefs, ref)
		sentTags = append(sentTags, gTag(ref))
	}
	if len(uniq) <= 1 {
		if len(succRefs) == 1 {
			// 旧单发语义原样保留：单发属精确投递，退出聚合。
			c.agg.Discard()
			return single(fmt.Sprintf(c.text("sent_to"), sessionTag(succRefs[0].Title)))
		}
		// 单目标失败时透传具体原因（"3【C】 — 会话正以终端模式运行…"形态），
		// 兜底文案只在无任何失败原因可列时使用（实际上 failParts 分支都
		// 会先 append，故仅防御）。
		if len(failParts) > 0 {
			return single(failParts[0])
		}
		return single(fmt.Sprintf(c.text("send_failed"), c.text("no_current")))
	}
	var parts []string
	if len(sentTags) > 0 {
		parts = append(parts, fmt.Sprintf(c.text("group_sent"), strings.Join(sentTags, " ")))
	}
	if len(failParts) > 0 {
		parts = append(parts, fmt.Sprintf(c.text("group_failed"), strings.Join(failParts, "; ")))
	}
	if len(succRefs) >= 2 {
		c.agg.Enter(succRefs)
	}
	return single(strings.Join(parts, "\n\n"))
}

// parseGTargets 解析 "#g <编号列表> <正文>":编号段为数字串(全角兼容),用
// 半角逗号/全角逗号/顿号/空白分隔;第一个非编号 token 起为正文,编号段后可紧跟一个
// 全角/半角冒号(保留旧 "#g 1:hi" 兼容)。已知限制:正文不能以数字开头
// (数字 token 会被吞进编号段)。逗号/顿号后只接数字;接着是其他文本时该
// token 起为正文。任一编号非法或正文为空 → ok=false 整条拒绝。
func (c *Console) parseGTargets(arg string) (nums []int, text string, ok bool) {
	i := 0
	for {
		j := i
		for j < len(arg) {
			r, size := utf8.DecodeRuneInString(arg[j:])
			if (r >= '0' && r <= '9') || (r >= '０' && r <= '９') {
				j += size
				continue
			}
			break
		}
		if j == i {
			return nil, "", false // 无编号(或编号段后无正文)
		}
		n, err := strconv.Atoi(normalizeDigits(arg[i:j]))
		if err != nil || n < 1 {
			return nil, "", false
		}
		nums = append(nums, n)
		i = j
		if i >= len(arg) {
			return nil, "", false // 有编号无正文
		}
		r, size := utf8.DecodeRuneInString(arg[i:])
		switch r {
		case ',', '，', '、':
			i += size
			for i < len(arg) {
				r2, s2 := utf8.DecodeRuneInString(arg[i:])
				if r2 != ' ' && r2 != '\t' {
					break
				}
				i += s2
			}
			continue
		case ' ', '\t':
			// 空白既可能是编号分隔也可能是正文起点:lookahead 首个非空字符。
			k := i
			for k < len(arg) {
				r2, s2 := utf8.DecodeRuneInString(arg[k:])
				if r2 != ' ' && r2 != '\t' {
					break
				}
				k += s2
			}
			if k < len(arg) {
				r2, _ := utf8.DecodeRuneInString(arg[k:])
				if (r2 >= '0' && r2 <= '9') || (r2 >= '０' && r2 <= '９') {
					i = k
					continue
				}
			}
			i = k // 目录结束,正文从这起
		}
		break
	}
	text = strings.TrimSpace(arg[i:])
	text = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(text, ":"), "："))
	if text == "" {
		return nil, "", false
	}
	return nums, text, true
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
	c.digitRecent = false
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

// handleStatus 逐会话状态总览:标题行(运行中计数,复用 status_running) +
// 每会话一条列表行(formatRef 形态,状态词+摘要)。排序 running > perm >
// idle > gone,同级按 Num。无任务回 no_tasks。
func (c *Console) handleStatus() []Reply {
	rows := c.ex.SessionStatuses()
	if len(rows) == 0 {
		return single(c.text("no_tasks"))
	}
	rank := map[string]int{"running": 0, "perm": 1, "idle": 2, "gone": 3}
	sorted := make([]SessionStatus, len(rows))
	copy(sorted, rows)
	sort.SliceStable(sorted, func(i, j int) bool {
		ri := statusRank(rank, sorted[i].State)
		rj := statusRank(rank, sorted[j].State)
		if ri != rj {
			return ri < rj
		}
		return sorted[i].Ref.Num < sorted[j].Ref.Num
	})
	// 会话编号语境只在渲染时有:按排好序的次序统一编号 1..N(formatRef 缺
	// Num 会打印 "0.")。
	for i := range sorted {
		sorted[i].Ref.Num = i + 1
	}
	running := 0
	for _, r := range sorted {
		if r.State == "running" {
			running++
		}
	}
	lines := make([]string, 0, len(sorted))
	for _, r := range sorted {
		stateWord := c.text("stat_word_" + r.State)
		if strings.HasPrefix(stateWord, "stat_word_") {
			// 未命中文案表时会原样返回 key(cmdText 语义):此时退回 state 本身。
			stateWord = "?" + r.State
		}
		line := formatRef(r.Ref) + " " + stateWord
		if r.Summary != "" {
			line += " — " + r.Summary
		}
		lines = append(lines, line)
	}
	return single(fmt.Sprintf(c.text("status_running"), running) + "\n\n" + strings.Join(lines, "\n"))
}

// statusRank 对未知 state 兜底为最大档(排最后),防 desktop 侧拼错把未知态排最前。
func statusRank(rank map[string]int, state string) int {
	r, ok := rank[state]
	if !ok {
		return 1 << 30
	}
	return r
}

// #h 的条数默认值（列表固定取最近 N 条）与单条正文截断上限（rune 数）。
const (
	recentDefault = 10
	// recentTextCap 是列表里单条消息正文的最大回显长度，超长截断加省略号；
	// #h <序号> 查全文不受此限（微信端超长按 maxSendRunes 拆条投递）。
	recentTextCap = 80
)

// handleRecent 处理 "#h" 与 "#h <序号>"：无参回显 AgentHub 最近 10 条消息
// （存快照供序号索引）；带数字序号回显上次列表第 N 条的完整正文，无快照时
// 先自动取最近一批。条数参数已砍（2026-10-19 用户决策：#h <n> 语义让位序号）。
func (c *Console) handleRecent(arg string) []Reply {
	if arg == "" {
		msgs := c.ex.RecentMessages(recentDefault)
		if len(msgs) == 0 {
			return single(c.text("no_messages"))
		}
		c.lastRecent = msgs
		c.digitRecent = true // #h 后纯数字查全文
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
	// #h <序号> 同样进入查全文语境，便于随后再回纯数字。
	c.digitRecent = true
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

// #gx 的默认条数与上限（超出钳制，防刷屏）。
const (
	gxDefault = 5
	gxMax     = 20
)

// handleGX 处理 "#gx <编号> [条数]"：按上次列表编号取该会话最近 N 条可见
// 消息（默认 5、上限 20、越界钳 20），时间正序列出；快照进 lastRecent 且
// digitRecent=true（后续纯数字查全文与 #h 同语境）。
func (c *Console) handleGX(arg string) []Reply {
	fields := strings.Fields(normalizeDigits(arg))
	if len(fields) == 0 || len(fields) > 2 {
		return single(c.text("usage_gx"))
	}
	n, err := strconv.Atoi(fields[0])
	if err != nil || n < 1 {
		return single(c.text("usage_gx"))
	}
	count := gxDefault
	if len(fields) == 2 {
		cnt, err2 := strconv.Atoi(fields[1])
		if err2 != nil || cnt < 1 {
			return single(c.text("usage_gx"))
		}
		if cnt > gxMax {
			cnt = gxMax
		}
		count = cnt
	}
	ref, ok := c.refByNum(n)
	if !ok {
		return single(c.text("no_mapping"))
	}
	if ref.ChatID == "" {
		return single(c.text("gx_none"))
	}
	msgs := c.ex.SessionRecentMessages(ref.ChatID, count)
	if len(msgs) == 0 {
		return single(c.text("gx_none"))
	}
	c.lastRecent = msgs
	c.digitRecent = true
	return single(c.renderRecentList(msgs))
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
	// 引导与列表空行分隔，防 Markdown 懒续行吞进末项（同 list_hint）。
	return fmt.Sprintf(c.text("recent_header"), len(msgs)) + "\n\n" +
		strings.Join(lines, "\n") + "\n\n" + c.text("recent_hint")
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
// （行首 ▶ 会打断 Markdown 有序列表解析）。
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

// sendToCurrent 把普通文本发给精确命中的当前会话。无默认接收者（含最新
// 归因为未激活/终端）时进入待选列表（会话 + en），由用户回编号后发送。
func (c *Console) sendToCurrent(text string) []Reply {
	c.refreshCurrent()
	ref := c.current
	if !c.hasCurrent || !sessionDeliverable(ref) {
		replies, _ := c.listForPending(text)
		return replies
	}
	if err := c.ex.Send(ref.ChatID, text); err != nil {
		return single(fmt.Sprintf(c.text("send_failed"), err))
	}
	// 直投当前会话属精确投递：退出群发聚合。
	c.agg.Discard()
	return nil
}

// HandleQuoted 处理引用回复：按 chatID 定位会话，切换当前上下文后投递
// （2026-10-20 用户确认：切换，后续裸消息跟随被引用的 agent）。handled=false
// 表示定位失败（chatID/sessionHint 皆无法恢复），调用方回落普通 Handle/待选。
// sessionHint 来自出站 svrMap 落盘的 SessionID：chatID 过期且 hub 无快照时仍可 OpenSession。
// 切换/发送失败 handled=true 且回执错误文案；成功 replies 空（静默）。
func (c *Console) HandleQuoted(chatID, sessionHint, text string) (bool, []Reply) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if chatID == "" && sessionHint == "" {
		return false, nil
	}
	// #n/#m/#f 进行中：提示选数字，不切会话不投递（避免数字回复被误消费）。
	// menuPick（无精确目标待选）让路：引用是更强的显式目标，清掉旧暂存再投递。
	if c.menu != nil {
		if c.menu.kind != menuPick {
			return true, single(c.text("pick_number"))
		}
		c.menu = nil
	}
	if chatID != "" {
		for _, r := range c.ex.ListSessions() {
			if r.ChatID != chatID {
				continue
			}
			return true, c.sendQuoted(r, text)
		}
		// 列表无此 chatID（已关/重建）：尝试按 hub SessionID 恢复。
		if ref, ok := c.ex.RecoverSession(chatID); ok {
			return true, c.sendQuoted(ref, text)
		}
	}
	// svrMap 落盘带来的 SessionID 提示：hub 也无快照时仍可激活磁盘会话。
	if sessionHint != "" {
		return true, c.sendQuoted(SessionRef{SessionID: sessionHint}, text)
	}
	return false, nil
}

// sendQuoted 激活引用目标并直发正文；失败回错误回执（调用方 handled=true）。
func (c *Console) sendQuoted(r SessionRef, text string) []Reply {
	ref, err := c.activateRef(r)
	if err != nil {
		return single(fmt.Sprintf(c.text("switch_failed"), c.errText(err)))
	}
	// 直发被引用会话：不得走 sendToCurrent→refreshCurrent，否则并发/跃迁
	// 的 LastActivity 会把目标抢回「最后活跃」会话。
	if err := c.ex.Send(ref.ChatID, text); err != nil {
		return single(fmt.Sprintf(c.text("send_failed"), c.errText(err)))
	}
	// 引用回复属精确投递：退出群发聚合。
	c.agg.Discard()
	return nil
}

// pickNewSentinel 是待选菜单分组一首项（新建会话）的哨兵 ID。
const pickNewSentinel = "__new__"

// listForPending 列出分组菜单：分组一首项「新建」（选中转 #n 分步流程），
// 分组二最近 5 个会话；暂存正文等用户回编号。#c 取消丢弃正文。
// 无会话时仍 Pending=true（仅新建入口），便于桌面清空草稿。
func (c *Console) listForPending(text string) ([]Reply, bool) {
	sessions := c.ex.ListSessions()
	limit := len(sessions)
	if limit > 5 {
		limit = 5
	}
	shown := sessions[:limit]
	for i := range shown {
		shown[i].Num = i + 2 // 编号 1 留给「新建」哨兵
		shown[i].Current = false // 无精确目标：不标当前
	}
	c.lastList = shown
	c.digitRecent = false
	opts := make([]MenuOption, 0, limit+1)
	opts = append(opts, MenuOption{ID: pickNewSentinel, Label: c.text("pick_group_new_label")})
	for _, r := range shown {
		opts = append(opts, MenuOption{ID: itoa(r.Num), Label: ""}) // 占位，渲染走 formatRef
	}
	c.menu = &pendingMenu{
		kind:        menuPick,
		options:     opts,
		results:     shown,
		pendingText: text,
	}
	// 前端 ReactMarkdown 渲染会把单 \n 折行（soft-break），段间一律 "\n\n"
	// 段落级拼接（含每条目独立成段）；空行分隔条目在段间拼接下冗余，去掉。
	lines := []string{"1. " + c.text("pick_group_new_label")}
	if len(shown) > 0 {
		lines = append(lines, c.text("pick_group_recent"))
		for _, r := range shown {
			line := formatRef(r)
			if r.TerminalActive {
				line += " 〔" + c.text("tag_terminal") + "〕"
			}
			lines = append(lines, line)
		}
	}
	return single(strings.Join([]string{
		c.text("pick_target"),
		strings.Join(lines, "\n\n"),
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

// TryCancelPick 让待选菜单失效（AgentHub 已经另行投递）。清理 menuPick，以及
// 由 menuPick「新建」入口转入、仍带暂存正文的 menuNew（另行投递后旧正文同样
// 不得再被发送）；其余选择菜单不碰。lastList 保留，后续数字仍可按最近列表
// 切换。锁被占用（Console.Handle 全程持锁回调 Executor 发送）时返回 false 并
// 放弃：该链路任一 ex.Send 调用点进入前 menu 必非 menuPick，放弃取消无损语义。
func (c *Console) TryCancelPick() bool {
	if !c.mu.TryLock() {
		return false
	}
	defer c.mu.Unlock()
	if c.menu != nil && (c.menu.kind == menuPick || (c.menu.kind == menuNew && c.menu.pendingText != "")) {
		c.menu = nil
	}
	return true
}

// single 构造单条回复。
func single(text string) []Reply {
	return []Reply{{Text: text}}
}
