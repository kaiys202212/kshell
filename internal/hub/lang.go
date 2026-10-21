package hub

// 命令层用户可见文案双语表（key → en/zh）。
// Console 的所有回复经 cmdText(lang, key) 取用，禁止在处理函数里硬编码
// 自然语言句（会话标题、工具名等运行时数据除外）。

type langPair struct{ En, Zh string }

var cmdTexts = map[string]langPair{
	// help 是 Markdown 文案：AgentHub 与微信气泡均按 Markdown 渲染，用列表
	// 与加粗表达结构（单个 \n 会被 soft-break 折叠；行首 # 命令词会被解析成
	// 标题，故一律列表项形式）。
	"help": {`**Commands**

- #?  Help
- #l  Recent sessions (first 10)
- #la All sessions
- #en [text]  Quick new session (text sent after ready)
- #n  Step-by-step new session
- #s  Interrupt current task
- #m  Switch model
- #h  Recent hub messages; #h <number> shows one message in full
- #g <number> <text>  Send to a session
- #f <keyword>  Search sessions
- #st Status
- #c  Cancel selection
- <number>  After #h: view that message; after #l/#la/#f: switch session
- Plain text = send to current session`,
		`**命令列表**

- #?  帮助
- #l  最近会话(前10)
- #la 全部会话
- #en [内容] 快速新建会话（内容自动下发）
- #n  分步新建会话
- #s  中断当前任务
- #m  切换模型
- #h  查看最近消息；#h <序号> 看某条全文
- #g <编号> <内容>  向指定会话发送
- #f <关键词>  搜索会话
- #st 状态
- #c  取消选择
- 数字  #h 后查看该条全文；#l/#la/#f 后切换会话
- 直接输入文字 = 发给当前会话`},

	"no_current":        {"No active session", "当前无活跃会话"},
	"no_current_list":   {"No active session, use #l to list", "当前无活跃会话，#l 查看"},
	"no_sessions":       {"No sessions yet", "暂无会话"},
	// AgentHub 只读语义（规则1/2）：列表标记与 Executor 拦截错误。错误 key 用
	// wire key 原样作键，errText 据此翻译；前端 locales 的 err.hub.* 与此同源，
	// 一侧改动须同步另一侧。
	"tag_terminal":            {"terminal", "终端"},
	"err.hub.terminal_active": {"Session is running in a terminal, close it first", "会话正以终端模式运行，请先关闭对应终端页签"},
	"err.hub.acp_unsupported": {"This tool has no chat mode", "该工具不支持对话模式"},
	"no_mapping":        {"No such number, use #l to list", "编号不存在，#l 查看"},
	// #h 序号越界专用：no_mapping 引导 #l 是切会话，语境不对（评审 M-1）。
	"recent_no_mapping": {"No such number, use #h for recent messages", "编号不存在，#h 查看最近消息"},
	"switch_failed":     {"Switch failed: %s", "切换失败: %s"},
	"create_failed":     {"Create failed: %s", "创建失败: %s"},
	"send_failed":       {"Send failed: %s", "发送失败: %s"},
	"cancelled":         {"Cancelled", "已取消"},
	"nothing_to_cancel": {"Nothing to cancel", "无进行中的选择"},
	"pick_number":       {"Reply with a number, or #c to cancel", "请回复数字选择，或 #c 取消"},
	"pick_target":       {"No exact session matched — reply with a number to send this message:", "未精确匹配到会话——回复编号，将把这条消息发送到所选会话:"},
	// 待选分组菜单：分组一「新建」（选中转 #n 分步流程），分组二最近会话。
	"pick_group_new_label": {"new session", "新建 session"},
	"pick_group_new":       {"--- New session ---", "--- 新建会话 ---"},
	"pick_group_recent":    {"--- Recent sessions ---", "--- 最近会话 ---"},
	// 分步新建完成并移交暂存正文后的第一人称确认（与 sent_ack 同语气）。
	"handoff_done":      {"Task handed off — I'll keep you posted here.", "任务移交完成——进展我会随时在这里同步。"},
	"pick_busy":         {"Finish or cancel the current selection first (#c), then resend", "请先完成或取消当前选择（#c），再重新发送"},
	"stopped":           {"Interrupt sent", "已发送中断指令"},
	"unknown_cmd":       {"Unknown command, use #? for help", "未知命令，#? 查看帮助"},
	"usage_g":           {"Usage: #g <number> <text>", "用法: #g <编号> <内容>"},
	"usage_f":           {"Usage: #f <keyword>", "用法: #f <关键词>"},
	"search_no_match":   {"No matching sessions", "未找到匹配会话"},
	"perm_done":         {"Permission request handled", "已处理权限请求"},
	"perm_stale":        {"That choice is no longer valid, try again", "该选择已失效，请重新操作"},
	"perm_hint":         {"Reply 1 (allow) or 2 (deny)", "请回复 1（允许）或 2（拒绝）"},
	"list_hint":         {"Reply with a number to switch session", "回复数字可切换到对应会话"},
	"sent_ack":          {"Got your message — I'm on it.", "收到，我马上开始。"},

	// 创建成功后的 agent 第一人称欢迎词（%s = 工具展示名）；文案与 AgentHub
	// 前端 hub.welcome key 保持一致，一侧改动须同步另一侧与 locales。
	"session_ready": {"Hi! This is %s. The session is ready — type any task below and I will start working on it.",
		"你好！我是 %s，会话已就绪——在下方输入任务，我马上开始。"},

	// helpText 之外的状态行/标签
	"current_session":    {"Current session: %s", "当前会话: %s"},
	"none":               {"none", "无"},
	"switched":           {"Switched: %s", "已切换: %s"},
	"model_switched":     {"Model switched: %s", "已切换模型: %s"},
	"pick_tool":          {"Pick a tool:", "选择工具:"},
	"pick_workspace":     {"Pick a workspace:", "选择项目:"},
	"pick_model":         {"Pick a model:", "选择模型:"},
	"no_models":          {"No selectable models for this session", "该会话无可选模型"},
	"no_tools":           {"no tools available", "无可用工具"},
	"no_workspaces":      {"no workspaces available", "无可用工作区"},
	"sent_to":            {"Sent to: %s", "已发送到: %s"},
	"status_running":     {"Running tasks: %d", "运行中任务: %d"},
	"status_undelivered": {"Undelivered messages: %d", "未送达消息: %d"},

	// #h 最近消息（Markdown 列表 + 序号查全文详情头）
	"usage_h":       {"Usage: #h or #h <number>", "用法: #h 或 #h <序号>"},
	"no_messages":   {"No hub messages yet", "暂无 AgentHub 消息"},
	"recent_header": {"**Recent %d messages:**", "**最近 %d 条消息:**"},
	"recent_hint":   {"Reply with a number to view full message", "回复数字可查看该条全文"},
	"msg_detail":    {"**Message %d** [%s][%s]", "**消息 %d** [%s][%s]"},
	"msg_user":      {"user", "用户"},
	"msg_agent":     {"agent", "agent"},
}

// CmdText 按语言取命令层文案：zh-CN 用中文，其余英文；未知 key 原样返回。
// 供命令层之外的渠道层取用（如微信桥的入站已接收确认）。
func CmdText(lang, key string) string { return cmdText(lang, key) }

// cmdText 按语言取命令层文案：zh-CN 用中文，其余英文；未知 key 原样返回。
func cmdText(lang, key string) string {
	p, ok := cmdTexts[key]
	if !ok {
		return key
	}
	if lang == "zh-CN" {
		return p.Zh
	}
	return p.En
}

// text 是 Console 的便捷取文案入口。
func (c *Console) text(key string) string {
	return cmdText(c.lang, key)
}
