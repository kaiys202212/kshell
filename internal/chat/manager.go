package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yangk/kshell/internal/acp"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

// attachCreatedGrace 打开新建聊天后，仍可能绑上稍早落盘的新会话（agent 写盘略早于 Open）。
const attachCreatedGrace = 2 * time.Minute

var (
	errNoBackend = errors.New("err.chat.not_ready")
	errNotFound  = errors.New("err.chat.gone")
)

// maxHistory 限制每会话保留的时间线事件数，防止长会话内存无限增长。
const maxHistory = 500

// Logf 是宿主注入的日志钩子（desktop 侧重定向到 ~/.kshell/logs/desktop.log）。
// 用于诊断「状态卡 running」问题：turn 起止、在途工具登记/摘除、静默回退
// 定时排布与放弃原因。nil（默认，含测试）时静默。
var Logf func(format string, args ...any)

func logf(format string, args ...any) {
	if Logf != nil {
		Logf(format, args...)
	}
}

// stderrTailLimit 限制异常停止时附加的 stderr 尾部字节数：足以容纳限流等
// 错误文本，又不至于把长调试日志整段灌进时间线。
const stderrTailLimit = 4096

type session struct {
	id        string
	key       string
	m         *Manager // 反向引用：静默回退定时器回调需要
	info      Info
	conn      Conn
	sessionID string
	handler   *sessionHandler

	ctx    context.Context
	cancel context.CancelFunc

	seq     int64
	history []Update
	tools   map[string]*ToolCall

	autoSeq  int    // 合成 messageId 的轮次计数（分片缺 messageId 时用）
	lastType string // 上一事件类型；用于判断连续同类分片

	pending map[string]chan permissionResult
	exited  bool

	openedAt       time.Time
	knownIDs       map[string]bool // 打开时扫描结果里已有的磁盘会话，禁止绑到本新建聊天
	promptTitleSet bool

	queue []queuedMsg // 忙时排队的用户消息（按 blocks 整体投递），turn_done 后按序送出

	modelState  *acp.SessionModelState   // agent 以 v1 models 形态报告的可用模型
	modelConfig *acp.SessionConfigOption // agent 以 configOptions(id=model) 形态报告的模型选项
	commands    []Command                // 最近一次 available_commands_update 的技能列表

	// 状态活动跟随：提前返回 prompt 的 agent（codebuddy 等）随后仍推工具/思考
	// 流水，Info.Status 必须跟随实际活动，否则 ListChats/chat:meta 携带的 ready
	// 会在前端镜像同步时冲掉已拉回的 running（任务在跑却显示「空闲」）。
	promptInFlight bool            // 是否有在途 session/prompt（投递忙闲唯一依据）
	inFlightTools  map[string]bool // 未到终态的工具调用（子任务长静默期的运行信号）
	lastActivity   time.Time       // 最近一次活动类更新时刻
	idleTimer      *time.Timer     // 静默回退定时器（持锁操作）
}

type permissionResult struct {
	outcome string
	option  string
}

// queuedMsg 是忙时排队的用户消息：blocks 为投递内容，hubMsgID 为关联的
// AgentHub 消息 ID（供撤回定位与投递回调；普通 Prompt 为空串）。
type queuedMsg struct {
	blocks   []acp.ContentBlock
	hubMsgID string
}

type sessionHandler struct {
	m  *Manager
	id string
}

type Manager struct {
	backend         Backend
	onUpdate        func(id string, u Update)
	onPermission    func(id string, r PermissionRequest)
	onExit          func(id string, code int, errMsg string)
	onInfo          func(Info)                      // 锁外；首条用户消息改写标题时
	onCommands      func(id string, cmds []Command) // 锁外；技能列表推送
	settingsMetaFor func(chatID string) string      // 锁外；_meta 载荷工厂（claude settings 注入）
	autoAllow       func() bool                     // bypass：自动选 allow 类 option，不弹窗
	mcpFor          func(chatID string) []acp.McpServerStdio
	// Hub 状态回调（锁外执行）：onHubQueued 在投递竞态兜底改入队时触发，
	// onHubDelivered 在带 hubMsgID 的条目真正发起 conn.Prompt 前触发。
	onHubQueued    func(hubMsgID string)
	onHubDelivered func(hubMsgID string)

	// bootID 是本 Manager 实例的唯一标识，参与会话 ID 生成。纯递增序列
	// c1/c2/... 在进程重启后归零，落盘的 chatId（需求绑定、hub 归因）会撞上
	// 新一轮同名 ID，造成假绑定。组成：纳秒时间戳 + PID + 包级实例计数，
	// 后两者兜底同刻度多实例（Windows 计时器精度粗）与多进程并发。
	bootID  string
	mu      sync.Mutex
	byKey   map[string]*session
	byID    map[string]*session
	order   []*session
	nextSeq int
}

func NewManager(b Backend,
	onUpdate func(id string, u Update),
	onPermission func(id string, r PermissionRequest),
	onExit func(id string, code int, errMsg string)) *Manager {
	return &Manager{
		backend: b, onUpdate: onUpdate, onPermission: onPermission, onExit: onExit,
		byKey: make(map[string]*session), byID: make(map[string]*session),
		bootID: fmt.Sprintf("%s%x_%x", strconv.FormatInt(time.Now().UnixNano(), 36), os.Getpid(), managerSeq.Add(1)),
	}
}

// managerSeq 是包级 Manager 实例计数，混入 bootID 防同一计时器刻度内
// 创建的多个实例拿到相同 bootID（Windows 纳秒计时器实际精度粗）。
var managerSeq atomic.Int64

// SetAutoAllowPermission 设置权限自动放行回调（nil=不自动放行）。
func (m *Manager) SetAutoAllowPermission(fn func() bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.autoAllow = fn
}

// SetOnInfo 注册标题变化回调（锁外执行）。
func (m *Manager) SetOnInfo(fn func(Info)) {
	m.mu.Lock()
	m.onInfo = fn
	m.mu.Unlock()
}

// SetOnCommands 注册技能列表推送回调（锁外执行）。
func (m *Manager) SetOnCommands(fn func(id string, cmds []Command)) {
	m.mu.Lock()
	m.onCommands = fn
	m.mu.Unlock()
}

// SetOnHubCallbacks 注册 Hub 队列状态回调（锁外执行；任一可为 nil）。
// queued：deliverTurn 竞态兜底入队时触发（PromptBlocksHub 的忙分支由返回值表达，不触发）；
// delivered：带 hubMsgID 的条目真正发起 conn.Prompt 前触发。
func (m *Manager) SetOnHubCallbacks(queued, delivered func(hubMsgID string)) {
	m.mu.Lock()
	m.onHubQueued = queued
	m.onHubDelivered = delivered
	m.mu.Unlock()
}

// SetMCPFactory 在 session/new 与 session/load 前把 MCP 服务器交给连接。
func (m *Manager) SetMCPFactory(fn func(chatID string) []acp.McpServerStdio) {
	m.mu.Lock()
	m.mcpFor = fn
	m.mu.Unlock()
}

// SetSettingsMetaFactory 在 session/new 与 session/load 前把 agent 专属的
// _meta 载荷（如 claude 的 settings 注入）交给连接；返回空串表示不注入。
func (m *Manager) SetSettingsMetaFactory(fn func(chatID string) string) {
	m.mu.Lock()
	m.settingsMetaFor = fn
	m.mu.Unlock()
}

// applySettingsMeta 把 _meta 载荷交给连接：spec 直通载荷优先（单一来源，
// 来自 launch 层组装的 ACPSettingsJSON），为空时回退 factory。
func (m *Manager) applySettingsMeta(conn Conn, id, specPayload string) {
	payload := specPayload
	if payload == "" {
		m.mu.Lock()
		factory := m.settingsMetaFor
		m.mu.Unlock()
		if factory == nil {
			return
		}
		payload = factory(id)
	}
	if payload == "" {
		return
	}
	setter, ok := conn.(interface{ SetSettingsMeta(string) })
	if !ok {
		return
	}
	setter.SetSettingsMeta(payload)
}

func (m *Manager) applyMCP(conn Conn, id string) {
	m.mu.Lock()
	factory := m.mcpFor
	m.mu.Unlock()
	if factory == nil {
		return
	}
	setter, ok := conn.(interface{ SetMCP([]acp.McpServerStdio) })
	if !ok {
		return
	}
	setter.SetMCP(factory(id))
}

// notePromptTitle 用新建会话的第一条用户消息替换占位标题。调用方持锁。
func (s *session) notePromptTitle(text string) (Info, bool) {
	if s.promptTitleSet || s.info.Kind != KindNew {
		return Info{}, false
	}
	s.promptTitleSet = true
	s.info.Prompted = true
	if title := providers.ClipPromptTitle(text); title != "" {
		s.info.Title = title
	}
	return s.info, true
}

// stopIdleLocked 停掉静默回退定时器（调用方持锁）。
func (s *session) stopIdleLocked() {
	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}
}

// scheduleIdleFallbackLocked 重排静默回退定时器（调用方持锁）。
// 有明确在途信号（在途 prompt / 在途工具）时不安排：前者由 turn_done 收尾，
// 后者等工具终态后再排。
func (s *session) scheduleIdleFallbackLocked() {
	s.stopIdleLocked()
	if s.promptInFlight || len(s.inFlightTools) > 0 {
		// 诊断：不排定时器的原因必须留痕，否则「在途信号永久悬挂 → 兜底失效」不可见
		logf("chat: idle 定时不排 id=%s promptInFlight=%v inFlightTools=%d %v",
			s.id, s.promptInFlight, len(s.inFlightTools), s.inFlightTools)
		return
	}
	sess := s
	s.idleTimer = time.AfterFunc(activityIdleFallback, func() { sess.m.idleFallback(sess) })
}

// idleFallback 静默回退：running 会话持续无活动且无在途信号时回 ready，
// 并经 onInfo 推送（desktop 层 Emit chat:meta，前端既有通路同步状态）。
func (m *Manager) idleFallback(s *session) {
	m.mu.Lock()
	if m.byID[s.id] != s || s.exited || s.info.Status != StatusRunning ||
		s.promptInFlight || len(s.inFlightTools) > 0 ||
		time.Since(s.lastActivity) < activityIdleFallback {
		// 诊断：定时器到点却放弃回退，原因必须留痕（哪个守卫没过）
		logf("chat: 静默回退放弃 id=%s 在册=%v 已退出=%v 状态=%s promptInFlight=%v inFlightTools=%d 近期活动=%v",
			s.id, m.byID[s.id] == s, s.exited, s.info.Status, s.promptInFlight, len(s.inFlightTools), time.Since(s.lastActivity))
		m.mu.Unlock()
		return
	}
	s.info.Status = StatusReady
	s.stopIdleLocked()
	onInfo := m.onInfo
	out := s.info
	m.mu.Unlock()
	logf("chat: 静默回退生效 id=%s 状态回 ready", s.id)
	if onInfo != nil {
		onInfo(out)
	}
}

// handshakeTimeout 限制 initialize + session/new|load 的总耗时。
// 已知 codebuddy --acp 的 session/new 可能死锁（kshell 侧无解），不设超时会让
// Wails 绑定 goroutine 永久挂起、前端「打开会话」永远 pending。
// var 仅为测试可缩短；生产代码不得修改。
var handshakeTimeout = 30 * time.Second

// activityIdleFallback 是「活动拉回 running 后」的静默回退阈值：turn_done 后
// agent 仍推流水使状态保持 running，若持续无活动且无在途信号（在途 prompt /
// 在途工具），状态回 ready 并经 onInfo 推送。var 仅为测试可缩短。
var activityIdleFallback = 2 * time.Minute

// isTerminalToolStatus 报告工具调用是否已到终态（不再算在途）。
// ACP 规范取值 pending/in_progress/completed/failed；防御性把 error 也当终态。
func isTerminalToolStatus(status string) bool {
	return status == "completed" || status == "failed" || status == "error"
}

// wrapHandshakeErr 把握手超时映射成前端可翻译的 wire key；其他错误原样返回。
func wrapHandshakeErr(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("err.chat.handshake_timeout")
	}
	return err
}

// Open 起进程并完成 initialize + session/new|load；sessionID 非空则 load。
func (m *Manager) Open(key string, info Info, spec Spec, sessionID string) (Info, error) {
	if m.backend == nil {
		return Info{}, errNoBackend
	}
	if key == "" {
		return Info{}, errors.New("err.chat.empty_key")
	}

	// 已退出的旧会话：先整体摘除再重建，避免 byID/order 泄漏。
	m.mu.Lock()
	old, ok := m.byKey[key]
	if ok && !old.exited {
		out := old.info
		m.mu.Unlock()
		return out, nil
	}
	m.mu.Unlock()
	if ok {
		m.remove(old)
		if old.cancel != nil {
			old.cancel()
		}
		if old.conn != nil {
			go old.conn.Close()
		}
	}

	m.mu.Lock()
	m.nextSeq++
	// ID 含 bootID：进程内单调递增，跨进程重启不重复（bootID 取创建时刻，
	// 纳秒级精度足以区分两次启动），防落盘 chatId 跨重启碰撞造成假绑定。
	s := &session{
		id:            fmt.Sprintf("c%s_%d", m.bootID, m.nextSeq),
		key:           key,
		m:             m,
		info:          info,
		pending:       make(map[string]chan permissionResult),
		tools:         make(map[string]*ToolCall),
		inFlightTools: make(map[string]bool),
		openedAt:      time.Now(),
		knownIDs:      map[string]bool{},
	}
	// 重建场景（同 key 的旧会话已退出被摘除）按「新会话是否重放历史」分流：
	//  - 不重放（sessionID 为空：KindNew 新起 / KindSession 降级按工作区新起）：
	//    agent 不会重推历史，前端时间线里的旧事件就是全部上下文，继承旧 history
	//    与 Seq 水位让新事件续在旧时间线之后（Seq 从 0 重启会被前端
	//    「Seq <= last 丢弃」门控吞掉，时间线冻结）。
	//  - 重放（session/load）：agent 会重推历史分片，继承旧时间线会导致整段
	//    重复——此时 fresh 起步，由重建方 emit chat:meta(Rebuilt) 通知前端重置
	//    Seq 门控并重拉 ChatHistory。
	if ok && sessionID == "" {
		s.seq = old.seq
		s.history = old.history
		s.autoSeq = old.autoSeq
		s.lastType = old.lastType
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.handler = &sessionHandler{m: m, id: s.id}
	s.info.ID = s.id
	s.info.Key = key
	s.info.Status = StatusStarting
	s.knownIDs = map[string]bool{}
	for _, sid := range info.KnownSessionIDs {
		if sid != "" {
			s.knownIDs[sid] = true
		}
	}
	s.info.KnownSessionIDs = nil
	m.byKey[key] = s
	m.byID[s.id] = s
	m.order = append(m.order, s)
	m.mu.Unlock()

	conn, err := m.backend.Start(spec, s.handler)
	if err != nil {
		m.fail(s.id, 0, err.Error())
		return Info{}, err
	}

	// conn/sessionID 在锁内发布，避免 watchExit 等 goroutine 读取到未同步的值。
	m.mu.Lock()
	if cur := m.byID[s.id]; cur != s {
		m.mu.Unlock()
		_ = conn.Close()
		return Info{}, errNotFound
	}
	s.conn = conn
	m.mu.Unlock()

	// 握手阶段（Initialize/session new/load）包独立超时：agent 挂死时 Open 能返回错误，
	// Prompt 仍用长命 s.ctx（轮次可持续任意久）。
	hsCtx, hsCancel := context.WithTimeout(s.ctx, handshakeTimeout)
	defer hsCancel()

	init, err := conn.Initialize(hsCtx)
	if err != nil {
		_ = conn.Close()
		err = wrapHandshakeErr(err)
		m.fail(s.id, 0, err.Error())
		return Info{}, err
	}
	// 协议版本不符时拒绝继续，避免按错误语义解读消息。
	if init.ProtocolVersion != 1 {
		msg := fmt.Sprintf("err.chat.unsupported_protocol|%d", init.ProtocolVersion)
		_ = conn.Close()
		m.fail(s.id, 0, msg)
		return Info{}, errors.New(msg)
	}
	m.applyMCP(conn, s.id)
	m.applySettingsMeta(conn, s.id, spec.ACPSettingsJSON)

	if sessionID != "" {
		// load 前先确认对方声明了该能力，否则会拿到无意义的 RPC 错误。
		if !init.AgentCapabilities.LoadSession {
			msg := "err.chat.no_session_load"
			_ = conn.Close()
			m.fail(s.id, 0, msg)
			return Info{}, errors.New(msg)
		}
		start, err := conn.LoadSession(hsCtx, sessionID, info.Workspace)
		if err != nil {
			_ = conn.Close()
			err = wrapHandshakeErr(err)
			m.fail(s.id, 0, err.Error())
			return Info{}, err
		}
		s.modelState = start.Models
		s.modelConfig = start.ModelConfig
	} else {
		start, err := conn.NewSession(hsCtx, info.Workspace)
		if err != nil {
			_ = conn.Close()
			err = wrapHandshakeErr(err)
			m.fail(s.id, 0, err.Error())
			return Info{}, err
		}
		s.modelState = start.Models
		s.modelConfig = start.ModelConfig
		sessionID = start.SessionID
	}

	m.mu.Lock()
	s.sessionID = sessionID
	// Info.SessionID 统一表示「绑定的磁盘会话 ID」：恢复型聊天传入的就是磁盘 ID，保持不变；
	// 新建聊天协议层拿到的是 ACP 内部 ID（与磁盘会话 ID 不同体系），Info 留空等扫描回填。
	if info.SessionID == "" && info.Kind == KindSession {
		s.info.SessionID = sessionID
	}
	s.info.Status = StatusReady
	// session/load 重放可能在握手期推来历史工具更新并登记进 inFlightTools；
	// 重放的执行态在本进程并不真实在跑，清空防 turn_done 因「在途非空」永久保持 running
	s.inFlightTools = make(map[string]bool)
	out := s.info
	// starting 期间入队的消息没有 turn_done 可等，必须在此主动投递首条，
	// 否则会静默永不投递（队列里已补过时间线，投递时不再补）。
	var next queuedMsg
	hasNext := false
	if len(s.queue) > 0 {
		next = s.queue[0]
		s.queue = s.queue[1:]
		hasNext = true
	}
	m.mu.Unlock()

	go m.watchExit(s)
	if hasNext {
		_ = m.deliverTurn(s.id, s, next, false)
	}
	return out, nil
}

// remove 从三张表中摘除会话并取消其挂起权限；连接由调用方在锁外关闭。
func (m *Manager) remove(s *session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.byKey, s.key)
	delete(m.byID, s.id)
	for i, cur := range m.order {
		if cur == s {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	pending := s.pending
	s.pending = make(map[string]chan permissionResult)
	cancelPending(pending)
	s.stopIdleLocked() // 摘除后定时器不得再触发（防泄漏与悬垂）
}

// cancelPending 非阻塞地让所有等待中的权限请求以 cancelled 结束。
func cancelPending(pending map[string]chan permissionResult) {
	for _, ch := range pending {
		select {
		case ch <- permissionResult{outcome: "cancelled"}:
		default:
		}
	}
}

// watchExit 等进程退出：若未主动关闭则标记退出、取消 ctx 与挂起权限，并回调 onExit。
func (m *Manager) watchExit(s *session) {
	code, err := s.conn.Wait()
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	m.mu.Lock()
	if s.exited {
		m.mu.Unlock()
		return
	}
	logf("chat: 进程退出 id=%s code=%d err=%s inFlightTools=%d promptInFlight=%v",
		s.id, code, errMsg, len(s.inFlightTools), s.promptInFlight)
	s.exited = true
	s.info.Status = StatusExited
	s.info.ExitCode = code
	s.queue = nil // 进程已退出，排队的消息不再有投递机会
	s.stopIdleLocked()
	pending := s.pending
	s.pending = make(map[string]chan permissionResult)
	m.mu.Unlock()

	// 进程已消失，等待中的权限请求不可能再被回答，全部以 cancelled 收尾。
	cancelPending(pending)
	if s.cancel != nil {
		s.cancel()
	}
	if m.onExit != nil {
		m.onExit(s.id, code, errMsg)
	}
}

func (m *Manager) fail(id string, code int, msg string) {
	m.mu.Lock()
	s := m.byID[id]
	if s == nil {
		m.mu.Unlock()
		return
	}
	s.exited = true
	s.info.Status = StatusExited
	s.info.ExitCode = code
	s.info.Error = msg
	s.queue = nil // 失败会话不再投递排队消息
	m.mu.Unlock()

	// 握手/启动失败不留下幽灵会话。这里不回调 onExit：失败会话的 id 从未返回给
	// 前端（Open 直接报错），推 chat:exit 前端也无从对应，属无效事件。
	m.remove(s)
	if s.cancel != nil {
		s.cancel()
	}
}

// flattenBlocks 把内容块展开成时间线用文本：text 块按序拼接，非文本块忽略
//（附件另有 AttachmentMeta 展示）。
func flattenBlocks(blocks []acp.ContentBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// attachmentMetas 提取非文本块的展示信息。
func attachmentMetas(blocks []acp.ContentBlock) []AttachmentMeta {
	var out []AttachmentMeta
	for _, b := range blocks {
		switch b.Type {
		case "text":
		case "image", "audio":
			out = append(out, AttachmentMeta{MimeType: b.MimeType})
		default:
			out = append(out, AttachmentMeta{Name: b.Name, MimeType: b.MimeType})
		}
	}
	return out
}

// Prompt 向会话发起一轮纯文本输入；忙时入队，当前轮结束后自动投递。
func (m *Manager) Prompt(id, text string) error {
	_, err := m.PromptBlocksHub(id, []acp.ContentBlock{{Type: "text", Text: text}}, "")
	return err
}

// PromptBlocks 向会话发起一轮多形态内容块输入（文本 + 附件）；忙时入队。
func (m *Manager) PromptBlocks(id string, blocks []acp.ContentBlock) error {
	_, err := m.PromptBlocksHub(id, blocks, "")
	return err
}

// PromptHub 向会话发起一轮纯文本输入并携带 AgentHub 消息 ID：
// 返回 queued=true 表示忙时已入队（false 为立即投递）。
func (m *Manager) PromptHub(id, text, hubMsgID string) (bool, error) {
	return m.PromptBlocksHub(id, []acp.ContentBlock{{Type: "text", Text: text}}, hubMsgID)
}

// PromptBlocksHub 同 PromptBlocks，但队列条目携带 hubMsgID；返回是否入队。
func (m *Manager) PromptBlocksHub(id string, blocks []acp.ContentBlock, hubMsgID string) (bool, error) {
	if len(blocks) == 0 {
		return false, nil
	}
	qm := queuedMsg{blocks: blocks, hubMsgID: hubMsgID}
	m.mu.Lock()
	s := m.byID[id]
	if s == nil || s.exited {
		m.mu.Unlock()
		return false, errNotFound
	}
	// 忙时入队：消息不丢，且照常补进时间线，界面立刻可见。
	// 忙闲只看在途 prompt 与握手期，不看展示状态——活动跟随会让状态在 turn_done
	// 后仍为 running，此时 agent 已结束当前 turn，新消息必须立即投递而非卡队。
	if s.info.Status == StatusStarting || s.promptInFlight {
		s.queue = append(s.queue, qm)
		userUpdate := s.appendUserBlocksLocked(blocks)
		onUpdate := m.onUpdate
		m.mu.Unlock()
		if onUpdate != nil {
			onUpdate(id, userUpdate)
		}
		return true, nil
	}
	m.mu.Unlock()
	return false, m.deliverTurn(id, s, qm, true)
}

// RecallQueued 从指定会话的队列中移除 hubMsgID 对应的条目（真移除，agent 收不到）；
// 未找到（不存在或已投递）返回 false。
func (m *Manager) RecallQueued(id, hubMsgID string) bool {
	if hubMsgID == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.byID[id]
	if s == nil {
		return false
	}
	for i, q := range s.queue {
		if q.hubMsgID == hubMsgID {
			s.queue = append(s.queue[:i], s.queue[i+1:]...)
			return true
		}
	}
	return false
}

// deliverTurn 发起一轮；record 为 true 时补 user 时间线并 emit。
// 队列里的消息在入队时已补过时间线（见 PromptBlocksHub 忙分支），投递时必须 record=false，
// 否则同一条用户消息会在时间线里出现两次。
func (m *Manager) deliverTurn(id string, s *session, qm queuedMsg, record bool) error {
	blocks := qm.blocks
	m.mu.Lock()
	if cur := m.byID[id]; cur != s || s.exited {
		m.mu.Unlock()
		return errNotFound
	}
	if s.info.Status == StatusStarting || s.promptInFlight {
		// 竞态兜底：期间又有轮次启动，改入队；补时间线与 emit 与 PromptBlocksHub 忙分支一致，
		// 否则该消息要等下一轮收尾才可见。
		s.queue = append(s.queue, qm)
		var userUpdate Update
		onUpdate := m.onUpdate
		if record {
			userUpdate = s.appendUserBlocksLocked(blocks)
		}
		onQueued := m.onHubQueued
		m.mu.Unlock()
		if record && onUpdate != nil {
			onUpdate(id, userUpdate)
		}
		if qm.hubMsgID != "" && onQueued != nil {
			onQueued(qm.hubMsgID)
		}
		return nil
	}
	s.info.Status = StatusRunning
	// 在途 prompt 是投递忙闲依据；置位时停掉静默回退定时器（turn_done 会收尾）
	s.promptInFlight = true
	s.stopIdleLocked()
	// 标题改写与 record 无关：即使是队列投递（record=false），这也是该新建会话的第一条用户消息。
	infoSnap, titleChanged := s.notePromptTitle(flattenBlocks(blocks))
	var userUpdate Update
	if record {
		userUpdate = s.appendUserBlocksLocked(blocks)
	}
	sid := s.sessionID
	conn := s.conn
	ctx := s.ctx
	onInfo := m.onInfo
	onUpdate := m.onUpdate
	m.mu.Unlock()

	if titleChanged && onInfo != nil {
		onInfo(infoSnap)
	}
	if record && onUpdate != nil {
		onUpdate(id, userUpdate)
	}

	go func() {
		// 带 hubMsgID 的条目在此刻真正开始投递：先回调再发（回调做状态修正，锁外执行）。
		if qm.hubMsgID != "" {
			m.mu.Lock()
			onDelivered := m.onHubDelivered
			m.mu.Unlock()
			if onDelivered != nil {
				onDelivered(qm.hubMsgID)
			}
		}
		logf("chat: turn 发起 id=%s inFlightTools=%d", id, len(s.inFlightTools))
		stop, err := conn.Prompt(ctx, sid, blocks)
		m.mu.Lock()
		if cur := m.byID[id]; cur != s || s.exited {
			m.mu.Unlock()
			return
		}
		s.promptInFlight = false
		logf("chat: turn 结束 id=%s err=%v stop=%q inFlightTools=%d %v",
			id, err, stop, len(s.inFlightTools), s.inFlightTools)
		if err != nil {
			// 轮次异常结束：在途工具不会再有终态更新，清掉防状态永久卡 running
			s.inFlightTools = make(map[string]bool)
		}
		// 提前返回 prompt 的 agent（codebuddy 等）可能仍有在途工具（子任务）：
		// 保持 running，等工具终态后的静默回退收尾；否则照旧回 ready。
		// turn_done 事件两种情况都照发，前端由后续活动更新拉回（5474856 规则）。
		if len(s.inFlightTools) > 0 {
			s.scheduleIdleFallbackLocked()
		} else {
			s.info.Status = StatusReady
		}
		s.seq++
		u := Update{Seq: s.seq, Type: "turn_done", StopReason: stop}
		if err != nil {
			u.Type = "error"
			u.Text = err.Error()
			s.info.Error = err.Error()
		} else if stop != "" && stop != "end_turn" && stop != "cancelled" {
			// 异常提前停止（refusal/max_tokens 等）：上游 CLI 常把 429 等 API 错误
			// 映射成 refusal，具体原因只在 stderr 里，附尾部供前端归因展示。
			u.Text = strings.TrimSpace(acp.StderrTail(conn.Stderr(), stderrTailLimit))
		}
		s.history = append(s.history, u)
		if len(s.history) > maxHistory {
			s.history = append([]Update(nil), s.history[len(s.history)-maxHistory:]...)
		}
		// 取出队列下一条（若有），在锁内决定，锁外发起；入队时已补时间线，投递不再补
		var next queuedMsg
		hasNext := false
		if !s.exited && len(s.queue) > 0 {
			next = s.queue[0]
			s.queue = s.queue[1:]
			hasNext = true
		}
		m.mu.Unlock()
		if m.onUpdate != nil {
			m.onUpdate(id, u)
		}
		if hasNext {
			_ = m.deliverTurn(id, s, next, false)
		}
	}()
	return nil
}

// appendUserBlocksLocked 追加一条 user 消息进时间线并返回该事件（调用方持锁）。
// 文本取 text 块拼接；非文本块记入 Attachments 供前端渲染附件标识。
// 超过 maxHistory 时截断保留尾部，避免长会话内存无限增长。
func (s *session) appendUserBlocksLocked(blocks []acp.ContentBlock) Update {
	s.seq++
	u := Update{
		Seq:         s.seq,
		Type:        "user",
		MessageID:   fmt.Sprintf("user:%d", s.seq),
		Text:        flattenBlocks(blocks),
		Attachments: attachmentMetas(blocks),
	}
	s.history = append(s.history, u)
	if len(s.history) > maxHistory {
		s.history = append([]Update(nil), s.history[len(s.history)-maxHistory:]...)
	}
	return u
}

func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	s := m.byID[id]
	if s == nil {
		m.mu.Unlock()
		return errNotFound
	}
	sid := s.sessionID
	conn := s.conn
	pending := s.pending
	s.pending = make(map[string]chan permissionResult)
	m.mu.Unlock()

	cancelPending(pending)
	if sid != "" && conn != nil {
		_ = conn.Cancel(sid)
	}
	return nil
}

func (m *Manager) RespondPermission(id, requestID, optionID string) error {
	return m.resolvePermission(id, requestID, permissionResult{outcome: "selected", option: optionID})
}

func (m *Manager) CancelPermission(id, requestID string) error {
	return m.resolvePermission(id, requestID, permissionResult{outcome: "cancelled"})
}

func (m *Manager) resolvePermission(id, requestID string, r permissionResult) error {
	m.mu.Lock()
	s := m.byID[id]
	if s == nil {
		m.mu.Unlock()
		return errNotFound
	}
	ch := s.pending[requestID]
	delete(s.pending, requestID)
	m.mu.Unlock()
	if ch == nil {
		return errNotFound
	}
	ch <- r
	return nil
}

// Close 结束聊天并从 List 摘除。幂等。
// 必须摘除：前端关闭页签后会在 scan:done 时用 ListChats 整表重建镜像，
// 若只标 exited 仍留在 List，已关页签会被「复活」。自然退出仍保留在 List
// （供前端显示退出态），直到用户点关闭才走本方法。
func (m *Manager) Close(id string) error {
	m.mu.Lock()
	s := m.byID[id]
	if s == nil {
		m.mu.Unlock()
		return nil
	}
	s.exited = true
	s.info.Status = StatusExited
	s.queue = nil // 关闭后不再投递已排队的消息
	if s.cancel != nil {
		s.cancel()
	}
	pending := s.pending
	s.pending = make(map[string]chan permissionResult)
	conn := s.conn
	m.mu.Unlock()

	cancelPending(pending)
	// 先关连接再摘除：watchExit 看到 exited 后直接返回，不会误推 onExit
	var closeErr error
	if conn != nil {
		closeErr = conn.Close()
	}
	m.remove(s)
	return closeErr
}

func (m *Manager) CloseAll() {
	m.mu.Lock()
	all := make([]*session, len(m.order))
	copy(all, m.order)
	m.mu.Unlock()
	for _, s := range all {
		_ = m.Close(s.id)
	}
}

func (m *Manager) List() []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Info, 0, len(m.order))
	for _, s := range m.order {
		out = append(out, s.info)
	}
	return out
}

// Get 返回指定聊天的 Info 快照（含打开 Key）；不存在时 ok=false。
// 供 desktop 层在 update 回调里反查 payload 归因字段（Key/ToolID/Workspace）。
func (m *Manager) Get(id string) (Info, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.byID[id]; s != nil {
		return s.info, true
	}
	return Info{}, false
}

// RememberKnownIDs 记下打开本聊天时已存在的磁盘会话 ID，扫描回填时不得把它们绑上来。
func (m *Manager) RememberKnownIDs(id string, ids []string) {
	if id == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.byID[id]
	if s == nil {
		return
	}
	known := make(map[string]bool, len(ids))
	for _, sid := range ids {
		if sid != "" {
			known[sid] = true
		}
	}
	s.knownIDs = known
}

// AttachSession 把扫描发现的磁盘会话绑定到匹配的新建聊天上：
// 新建（KindNew）聊天启动时磁盘上还没有会话记录，Info.SessionID 为空、标题是占位文案；
// 扫描发现新会话后由 desktop 层调用本方法回填，页签标题与前端「恢复/切换」判断都依赖它。
// 只绑未退出、未绑定（Info.SessionID 为空）的新建聊天；工作区路径归一化后比较，
// toolID 与聊天 ToolID 不一致时不绑（ToolID 为空表示由 launch 选首选，允许绑定）。
// 打开时已知的会话 ID、以及 CreatedAt 明显早于打开时刻的旧对话一律跳过，避免页签套用左侧列表标题。
// 绑定 SessionID 后，仅当 messages>0 且 title 非空才改页签标题（用户已发言）。
// 只改 Info：协议层继续用 agent 返回的内部 sessionID，Prompt/Cancel 不受影响。
func (m *Manager) AttachSession(sessionID, workspace, toolID, title string, messages int, createdAt time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	ws := discovery.NormalizePath(workspace)
	for _, s := range m.order {
		info := &s.info
		if s.exited || info.Kind != KindNew || info.SessionID != "" {
			continue
		}
		if s.knownIDs[sessionID] {
			continue
		}
		if !createdAt.IsZero() && createdAt.Before(s.openedAt.Add(-attachCreatedGrace)) {
			continue
		}
		if discovery.NormalizePath(info.Workspace) != ws {
			continue
		}
		if info.ToolID != "" && toolID != "" && info.ToolID != toolID {
			continue
		}
		info.SessionID = sessionID
		if messages > 0 && title != "" {
			info.Title = title
		}
		return true
	}
	return false
}

// UpdateSessionTitle 按磁盘会话 ID 更新已绑定聊天的标题。
// 必须已有用户消息（messages>0）且标题非空；用于扫描后把后来落盘的真实标题刷到页签。
// 命中时经 onInfo 推送更新后的 Info（锁外；desktop 层 Emit chat:meta 并回填
// 需求的会话标题快照）。
func (m *Manager) UpdateSessionTitle(sessionID, title string, messages int) bool {
	if sessionID == "" || title == "" || messages <= 0 {
		return false
	}
	m.mu.Lock()
	var out Info
	updated := false
	for _, s := range m.order {
		if s.info.SessionID != sessionID || s.info.Title == title {
			continue
		}
		s.info.Title = title
		out = s.info
		updated = true
		break
	}
	onInfo := m.onInfo
	m.mu.Unlock()
	if updated && onInfo != nil {
		onInfo(out)
	}
	return updated
}

func (m *Manager) History(id string) []Update {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.byID[id]
	if s == nil {
		return nil
	}
	out := make([]Update, len(s.history))
	copy(out, s.history)
	return out
}

// Models 返回会话的模型切换视图；agent 未报告任何模型信息时 ok=false。
func (m *Manager) Models(id string) (ModelsView, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.byID[id]
	if s == nil {
		return ModelsView{}, false
	}
	if ms := s.modelState; ms != nil {
		out := ModelsView{Current: ms.CurrentModelID, Models: make([]ModelView, 0, len(ms.AvailableModels))}
		for _, mo := range ms.AvailableModels {
			out.Models = append(out.Models, ModelView{ID: mo.ModelID, Name: mo.Name, Description: mo.Description})
		}
		return out, true
	}
	if mc := s.modelConfig; mc != nil {
		out := ModelsView{Models: make([]ModelView, 0, len(mc.Options))}
		var current string
		_ = json.Unmarshal(mc.CurrentValue, &current)
		out.Current = current
		for _, o := range mc.Options {
			out.Models = append(out.Models, ModelView{ID: o.Value, Name: o.Name, Description: o.Description})
		}
		return out, true
	}
	return ModelsView{}, false
}

// modelSetTimeout 限制 SetModel 单次 RPC 的耗时。agent（codebuddy 有 RPC 死锁
// 前科）在 turn 流式输出中可能不应答 set_model，无超时会让前端 runAgentCreate
// 永挂、创建窗体关不上；超时错误上抛后前端走「切模型失败只提示不回滚」分支。
// var 仅为测试可缩短；生产代码不得修改。
var modelSetTimeout = 30 * time.Second

// wrapModelTimeout 把 SetModel 超时映射成前端可翻译的 wire key；其他错误原样返回。
func wrapModelTimeout(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("err.chat.model_timeout")
	}
	return err
}

// SetModel 运行时切换会话模型；成功后乐观更新缓存视图供前端即时反映。
func (m *Manager) SetModel(id, modelID string) error {
	m.mu.Lock()
	s := m.byID[id]
	if s == nil || s.exited {
		m.mu.Unlock()
		return errNotFound
	}
	conn := s.conn
	sid := s.sessionID
	ctx := s.ctx
	m.mu.Unlock()
	if conn == nil || sid == "" {
		return errNoBackend
	}
	rpcCtx, cancel := context.WithTimeout(ctx, modelSetTimeout)
	defer cancel()
	if err := wrapModelTimeout(conn.SetModel(rpcCtx, sid, modelID)); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// RPC 期间会话可能已退出/重建：只更新仍在册的会话，避免写悬垂指针。
	if cur := m.byID[id]; cur != s || s.exited {
		return nil
	}
	if s.modelState != nil {
		s.modelState.CurrentModelID = modelID
	}
	if s.modelConfig != nil {
		v, _ := json.Marshal(modelID)
		s.modelConfig.CurrentValue = v
	}
	return nil
}

// Commands 返回会话最近的技能/命令列表；未收到过推送时为空。
func (m *Manager) Commands(id string) []Command {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.byID[id]
	if s == nil {
		return nil
	}
	return append([]Command(nil), s.commands...)
}

func (h *sessionHandler) SessionUpdate(sessionID string, raw json.RawMessage) {
	h.m.onSessionUpdate(h.id, sessionID, raw)
}

func (h *sessionHandler) RequestPermission(ctx context.Context, sessionID, requestID string, p acp.RequestPermissionParams) (acp.PermissionOutcome, error) {
	return h.m.awaitPermission(ctx, h.id, sessionID, requestID, p)
}

// acpUpdate 按 ACP 规范的小写字段解码 session/update（前端模型是 PascalCase，故单独解码再转换）。
type acpUpdate struct {
	SessionUpdate     string          `json:"sessionUpdate"`
	MessageID         string          `json:"messageId"`
	Content           json.RawMessage `json:"content"`
	ToolCallID        string          `json:"toolCallId"`
	Title             string          `json:"title"`
	Name              string          `json:"name"`
	Kind              string          `json:"kind"`
	Status            string          `json:"status"`
	RawInput          json.RawMessage `json:"rawInput"`
	RawOutput         json.RawMessage `json:"rawOutput"`
	Entries           []acpPlanEntry  `json:"entries"`
	AvailableCommands []acpCommand    `json:"availableCommands"`
}

type acpPlanEntry struct {
	Content  string `json:"content"`
	Priority string `json:"priority"`
	Status   string `json:"status"`
}

type acpCommand struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Input       *struct {
		Hint string `json:"hint"`
	} `json:"input"`
}

// normalizeUpdate 把 ACP session/update 转成前端时间线事件；不支持的变体返回 ok=false。
func normalizeUpdate(raw json.RawMessage) (Update, bool) {
	var u acpUpdate
	if err := json.Unmarshal(raw, &u); err != nil {
		return Update{}, false
	}
	switch u.SessionUpdate {
	case "user_message_chunk":
		return Update{Type: "user", MessageID: u.MessageID, Text: contentText(u.Content)}, true
	case "agent_message_chunk":
		return Update{Type: "assistant", MessageID: u.MessageID, Text: contentText(u.Content)}, true
	case "agent_thought_chunk":
		return Update{Type: "thought", MessageID: u.MessageID, Text: contentText(u.Content)}, true
	case "tool_call", "tool_call_update":
		if u.ToolCallID == "" {
			return Update{}, false
		}
		var probe struct {
			Content []json.RawMessage `json:"content"`
		}
		var content []json.RawMessage
		if err := json.Unmarshal(raw, &probe); err == nil {
			content = probe.Content
		}
		tc := ToolCall{
			ToolCallID: u.ToolCallID, Name: u.Name, Title: u.Title,
			Kind: u.Kind, Status: u.Status, Content: content,
			RawInput: u.RawInput, RawOutput: u.RawOutput,
		}
		return Update{Type: "tool", ToolCallID: tc.ToolCallID, Tool: &tc}, true
	case "plan":
		plan := make([]PlanEntry, 0, len(u.Entries))
		for _, e := range u.Entries {
			plan = append(plan, PlanEntry{Content: e.Content, Priority: e.Priority, Status: e.Status})
		}
		return Update{Type: "plan", Plan: plan}, true
	case "available_commands_update":
		cmds := make([]Command, 0, len(u.AvailableCommands))
		for _, c := range u.AvailableCommands {
			hint := ""
			if c.Input != nil {
				hint = c.Input.Hint
			}
			cmds = append(cmds, Command{Name: c.Name, Description: c.Description, InputHint: hint})
		}
		return Update{Type: "commands", Commands: cmds}, true
	default:
		return Update{}, false
	}
}

func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var block struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &block); err == nil && block.Text != "" {
		return block.Text
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}

func (m *Manager) onSessionUpdate(id, sessionID string, raw json.RawMessage) {
	u, ok := normalizeUpdate(raw)
	if !ok {
		return
	}
	// 技能列表是会话状态而非时间线事件：存会话、单独推送，不进历史。
	if u.Type == "commands" {
		m.mu.Lock()
		s := m.byID[id]
		if s != nil {
			s.commands = u.Commands
		}
		cb := m.onCommands
		m.mu.Unlock()
		if cb != nil {
			cb(id, u.Commands)
		}
		return
	}
	m.mu.Lock()
	s := m.byID[id]
	if s == nil {
		m.mu.Unlock()
		return
	}
	// tool_call_update 常是稀疏增量，需与已有快照合并后再发前端。
	if u.Type == "tool" && u.Tool != nil {
		merged := mergeToolCall(s.tools[u.ToolCallID], u.Tool)
		s.tools[u.ToolCallID] = merged
		u.Tool = merged
		u.ToolCallID = merged.ToolCallID
	}
	// 文本类分片常缺 messageId：按连续同类分片合成 key，供前端归并同一消息。
	if u.Type == "user" || u.Type == "assistant" || u.Type == "thought" {
		if u.MessageID == "" {
			if s.lastType != u.Type {
				s.autoSeq++
			}
			u.MessageID = fmt.Sprintf("auto:%s:%d", u.Type, s.autoSeq)
		}
		s.lastType = u.Type
	} else {
		s.lastType = "" // 工具/计划/结束事件打断连续分片
	}
	s.seq++
	u.Seq = s.seq
	s.history = append(s.history, u)
	if len(s.history) > maxHistory {
		s.history = append([]Update(nil), s.history[len(s.history)-maxHistory:]...)
	}
	// 状态活动跟随（与前端 5474856 同规则）：活动类更新把 ready 拉回 running，
	// 并维护在途工具集合与静默回退定时器。user/commands 不算 agent 活动。
	switch u.Type {
	case "thought", "assistant", "plan":
		s.lastActivity = time.Now()
		if s.info.Status == StatusReady && !s.exited {
			s.info.Status = StatusRunning
		}
		s.scheduleIdleFallbackLocked()
	case "tool":
		s.lastActivity = time.Now()
		if u.Tool != nil {
			if isTerminalToolStatus(u.Tool.Status) {
				delete(s.inFlightTools, u.ToolCallID)
				// 诊断：终态摘除后剩余在途数决定兜底定时器能否重排
				logf("chat: 工具终态 id=%s tool=%s status=%s 剩余在途=%d",
					id, u.Tool.Name, u.Tool.Status, len(s.inFlightTools))
			} else {
				s.inFlightTools[u.ToolCallID] = true
			}
		}
		if s.info.Status == StatusReady && !s.exited {
			s.info.Status = StatusRunning
		}
		s.scheduleIdleFallbackLocked()
	}
	m.mu.Unlock()
	if m.onUpdate != nil {
		m.onUpdate(id, u)
	}
}

// mergeToolCall 保留 prev 的非空字段，用 next 的非空字段覆盖；Content 为空时沿用 prev。
func mergeToolCall(prev, next *ToolCall) *ToolCall {
	if prev == nil {
		return next
	}
	out := *prev
	if next.Name != "" {
		out.Name = next.Name
	}
	if next.Title != "" {
		out.Title = next.Title
	}
	if next.Kind != "" {
		out.Kind = next.Kind
	}
	if next.Status != "" {
		out.Status = next.Status
	}
	if len(next.Content) > 0 {
		out.Content = next.Content
	}
	if len(next.RawInput) > 0 {
		out.RawInput = next.RawInput
	}
	if len(next.RawOutput) > 0 {
		out.RawOutput = next.RawOutput
	}
	return &out
}

// interactiveAskTools 是「向用户征求决策」的交互类工具名：bypass 自动放行只该
// 作用于「执行许可」，提问/计划批准被静默代答等于剥夺用户的决策权。
// 名单按需追加（AskUserQuestion=提问，ExitPlanMode=计划批准）。
var interactiveAskTools = []string{"AskUserQuestion", "ExitPlanMode"}

// isInteractiveAskTool 报告该权限请求对应的工具是否属交互类。
// 工具名从已收到的 session/update tool_call（s.tools）反查：agent 实测先发
// tool_call 更新再发 request_permission（同一 stdout 流内有序），正常时序下
// 必有记录；极端乱序查不到时按普通工具处理（维持 bypass 语义，宁可漏判）。
func (m *Manager) isInteractiveAskTool(id, toolCallID string) bool {
	if toolCallID == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.byID[id]
	if s == nil {
		return false
	}
	tc := s.tools[toolCallID]
	if tc == nil {
		return false
	}
	for _, name := range interactiveAskTools {
		if strings.EqualFold(tc.Name, name) {
			return true
		}
	}
	return false
}

func (m *Manager) awaitPermission(ctx context.Context, id, sessionID, requestID string, p acp.RequestPermissionParams) (acp.PermissionOutcome, error) {
	if optID, ok := pickAllowOption(p.Options); ok && !m.isInteractiveAskTool(id, p.ToolCall.ToolCallID) {
		m.mu.Lock()
		fn := m.autoAllow
		m.mu.Unlock()
		if fn != nil && fn() {
			return acp.PermissionOutcome{Outcome: "selected", OptionID: optID}, nil
		}
	}

	ch := make(chan permissionResult, 1)
	m.mu.Lock()
	s := m.byID[id]
	if s == nil {
		m.mu.Unlock()
		return acp.PermissionOutcome{Outcome: "cancelled"}, nil
	}
	s.pending[requestID] = ch
	m.mu.Unlock()

	// Options 预置非 nil 空切片：agent 推送空选项列表时避免序列化成 null，
	// 前端 permission.Options.map 不至于在 null 上崩溃（渲染层另有 ?? 兜底）。
	req := PermissionRequest{RequestID: requestID, SessionID: sessionID, ToolCall: toToolCall(p.ToolCall), Options: []PermissionOption{}}
	for _, o := range p.Options {
		req.Options = append(req.Options, PermissionOption{OptionID: o.OptionID, Name: o.Name, Kind: o.Kind})
	}
	if m.onPermission != nil {
		m.onPermission(id, req)
	}

	select {
	case r := <-ch:
		if r.outcome == "cancelled" {
			return acp.PermissionOutcome{Outcome: "cancelled"}, nil
		}
		return acp.PermissionOutcome{Outcome: "selected", OptionID: r.option}, nil
	case <-ctx.Done():
		return acp.PermissionOutcome{Outcome: "cancelled"}, nil
	}
}

// pickAllowOption 按 kind 优先选 allow_once → allow_always → allow（含子串匹配）。
func pickAllowOption(opts []acp.PermissionOption) (string, bool) {
	priority := []string{"allow_once", "allow_always", "allow"}
	for _, kind := range priority {
		for _, o := range opts {
			if o.Kind == kind || o.OptionID == kind {
				return o.OptionID, true
			}
		}
	}
	for _, o := range opts {
		k := strings.ToLower(o.Kind + " " + o.OptionID)
		if strings.Contains(k, "allow") && !strings.Contains(k, "deny") {
			return o.OptionID, true
		}
	}
	return "", false
}

func toToolCall(t acp.ToolCall) ToolCall {
	return ToolCall{
		ToolCallID: t.ToolCallID,
		Name:       t.Name,
		Title:      t.Title,
		Kind:       t.Kind,
		Status:     t.Status,
		Content:    t.Content,
		RawInput:   t.RawInput,
		RawOutput:  t.RawOutput,
	}
}
