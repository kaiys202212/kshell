package weixin

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/hubtools"
)

// sendRetry 是出站普通失败的额外重试次数（首次 + 2 = 共 3 次尝试）。
const sendRetry = 2

// sendRetryWait 是重试间隔。
const sendRetryWait = 500 * time.Millisecond

// maxSendRunes 是单条微信文本消息的最大长度（超出按此上限拆成多条连续发送）。
const maxSendRunes = 1500

// reWireKey 判断文本是否形如 wire key（如 hub.welcome / err.xxx）：
// 小写字母开头、仅小写字母数字下划线点、无空格。这类文本是后端稳定 key，
// 推到微信只是噪音，跳过。
var reWireKey = regexp.MustCompile(`^[a-z][a-z0-9_.]*$`)

// BridgeConfig 是桥的运行配置（desktop 从 config.Weixin 读取注入）。
type BridgeConfig struct {
	// Wxid 是白名单用户 wxid；空串 = 拒绝所有入站。
	Wxid string
	// Language 是桥文案语言（"en" / "zh-CN"）。
	Language string
	// SvrMapPath 是出站消息 ID→会话归因落盘路径；空则用 defaultSvrMapPath。
	// 测试注入临时文件，避免写真实 ~/.kshell。
	SvrMapPath string
}

// ctxInfo 是最近一次白名单入站消息的发送凭据（出站镜像按它投递）。
type ctxInfo struct {
	userID       string
	contextToken string
	valid        bool
}

// ErrTranscriberNotConfigured 是语音转写未配置的哨兵：transcriber 实现方
// （desktop 装配）在模型平台缺 ASR 模型时返回它，桥据此回执「未配置」而非「失败」。
var ErrTranscriberNotConfigured = errors.New("err.weixin.voice_asr_unconfigured")

// ErrVoiceUnsupported 是「默认对话模型不支持音频输入」的哨兵：transcriber
// 实现方在 chat 回落转写被网关以不支持音频拒绝时返回它（desktop 层从
// hubtools.ErrChatAudioUnsupported 映射而来），桥据此回执配置指引而非透传
// 网关原始报错。
var ErrVoiceUnsupported = errors.New("err.weixin.voice_unsupported")

// Transcriber 把 WAV 音频转成文字（OpenAI 兼容 ASR 端点，desktop 装配注入）。
type Transcriber func(ctx context.Context, wav []byte) (string, error)

// queuedFile 是一条待转交附件：已下载解密落盘，等用户的下一条文字合并投递。
type queuedFile struct {
	Name string
	Path string // 落盘绝对路径
}

// pendingMsg 是 context_token 失效（-14）时缓存的待发消息。
// 补发时用各自存的凭据，而非最新入站的。
type pendingMsg struct {
	userID       string
	contextToken string
	text         string
	chatID       string // 出站文本归属的 agent 会话（引用归因映射用，可空）
	sessionID    string // 磁盘会话 ID（落盘归因，可空）
	title        string // 会话标题快照（可空）
}

// Bridge 是微信消息桥：入站经白名单过滤后交给命令层，出站把 hub 的 agent
// 消息转纯文本推送。发送直接经持有的 iLink Client 完成。
type Bridge struct {
	client  *Client
	ex      hub.Executor
	console *hub.Console
	wxid    string
	lang    string

	mu          sync.Mutex
	ctx         ctxInfo      // 最近入站消息的发送凭据
	pending     []pendingMsg // -14 时缓存的待发
	undelivered int          // 发送失败计数（#st 用）
	queue       []queuedFile // 待转交附件（等下一条可转发文字合并投递）
	queueAt     time.Time    // 最近一次附件入队时间（整批以最新一张为基准的过期计时）；mu 保护
	// svrMap 是「出站消息 ID（message_id）→ 会话归因」映射（rememberSvrAttr
	// 写，resolveQuote 读）。按 wxid 落盘到 svrMapPath，跨重启仍可按
	// SessionID 恢复已关会话。svrOrder 记录插入序，超上限丢最旧。
	svrMap     map[string]svrAttr
	svrOrder   []string
	svrMapPath string

	// filesDir 是附件落盘目录；NewBridge 给默认值，测试注入临时目录。
	filesDir string
	// transcriber 是语音转写函数（desktop 注入）；nil = 未装配（纯语音回执提示）。
	transcriber Transcriber
	// router 是入站文本智能路由钩子（desktop 注入）；nil = 未装配（全部走命令层）。
	router InputRouter
	// managerRouter 是管理者循环路由钩子（desktop 注入）；nil = 未装配。
	// 优先级在 router 之前：命中即回显结论 Replies，不走决策执行。
	managerRouter ManagerRouter
	// askResumer 是挂起反问显式续跑钩子（desktop 注入）；nil = 回落
	// managerRouter。合并文本超过 agentapi 短输入门槛，必须走 Resume。
	askResumer AskResumer
	// askPendingFn 报告是否有未过期挂起反问（desktop 实时问 agentapi）；
	// nil 视为无挂起（未改造的旧装配路径零影响）。
	askPendingFn func() bool
	// directPatternsFn 是逃生门白名单查询函数（desktop 注入，实时读配置）；
	// nil 时回落 hub.DefaultDirectPatterns。
	directPatternsFn func() []string
	// routeGate 是智能链路启用门禁（desktop 注入，实时读 AgentHub 总开关）；
	// nil 视为允许（未改造的旧装配路径零影响）。false 时自由文本不进智能链路，
	// 按旧语义直投当前会话——"未启用智能路由"是正常用法，不能拦截。
	routeGate RouteGate
	// routeRound 是连续反问轮次（反问链最多 hubtools.AskMaxRounds 轮）；mu 保护。
	// 只增不减的正确性依赖入站单 goroutine 串行处理（Stop/重绑前先停轮询）。
	routeRound int
	// toolName 按工具 ID 查展示名（desktop 注入，与欢迎词归因同源）；
	// nil 或查不到时出站标题栏回落工具 ID。
	toolName func(string) string

	// outbound 是串行出站队列：DeliverHubMessage 只入队，单 worker 按序
	// deliver，避免 hub 消息因网络耗时不等而在微信侧乱序。
	outbound chan outMsg
	// outMu 保护 outbound 的关闭状态：入队与 Close 并发时防 send-on-closed。
	outMu     sync.Mutex
	outClosed bool
	// workerDone 关闭表示出站 worker 已退出（Close 等待用）。
	workerDone chan struct{}
	// workerOnce 保证 worker 惰性启动一次。
	workerOnce sync.Once
	// flushHook 仅供测试：worker 开始处理一条消息前回调（FlushForTest 用）。
	flushHook func()

	// onLearnWxid 非空时，wxid 首次从入站消息学习得到即回调（desktop 落盘）。
	// 由 SetOnLearnWxid 在 Start 前装配，此后视为只读。
	onLearnWxid func(wxid string)

	// sleep 供测试注入 no-op，避免重试真实等待。
	sleep func(time.Duration)
	// nowFn 供测试注入假时钟（附件过期判定用），避免真实等待；nil 回落 time.Now。
	nowFn func() time.Time
}

// outboundCap 是出站队列容量：满时新消息直接丢弃计 undelivered，
// 不阻塞 hub 写入路径。
const outboundCap = 128

// NewBridge 创建微信桥；内部持有独立的命令状态机。
func NewBridge(client *Client, ex hub.Executor, cfg BridgeConfig) *Bridge {
	path := cfg.SvrMapPath
	if path == "" {
		path = defaultSvrMapPath()
	}
	m, order := loadSvrMapAccount(path, cfg.Wxid)
	return &Bridge{
		client:     client,
		ex:         ex,
		console:    hub.NewConsoleLang(ex, cfg.Language),
		wxid:       cfg.Wxid,
		lang:       cfg.Language,
		outbound:   make(chan outMsg, outboundCap),
		svrMap:     m,
		svrOrder:   order,
		svrMapPath: path,
		filesDir:   defaultFilesDir(),
		workerDone: make(chan struct{}),
		sleep:      time.Sleep,
	}
}

// defaultFilesDir 返回附件落盘默认目录 ~/.kshell/weixin-files；取不到主目录
// 时回落系统临时目录（附件功能降级仍可用）。
func defaultFilesDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "kshell-weixin-files")
	}
	return filepath.Join(home, ".kshell", "weixin-files")
}

// SetTranscriber 装配语音转写函数（可随时替换：desktop 读实时配置）。
func (b *Bridge) SetTranscriber(fn Transcriber) {
	b.mu.Lock()
	b.transcriber = fn
	b.mu.Unlock()
}

// SetToolName 装配工具展示名查询函数（desktop 注入，可随时替换）。
func (b *Bridge) SetToolName(fn func(string) string) {
	b.mu.Lock()
	b.toolName = fn
	b.mu.Unlock()
}

// transcriberFn 加锁读当前转写函数。
func (b *Bridge) transcriberFn() Transcriber {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.transcriber
}

// SetRouter 装配入站文本智能路由钩子（可随时替换：desktop 读实时配置）。
func (b *Bridge) SetRouter(fn InputRouter) {
	b.mu.Lock()
	b.router = fn
	b.mu.Unlock()
}

// SetManagerRouter 装配管理者循环路由钩子（可随时替换：desktop 读实时装配）。
func (b *Bridge) SetManagerRouter(fn ManagerRouter) {
	b.mu.Lock()
	b.managerRouter = fn
	b.mu.Unlock()
}

// managerRouterFn 加锁读当前管理者路由钩子。
func (b *Bridge) managerRouterFn() ManagerRouter {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.managerRouter
}

// SetAskResumer 装配挂起反问显式续跑钩子（可随时替换：desktop 读实时装配）。
func (b *Bridge) SetAskResumer(fn AskResumer) {
	b.mu.Lock()
	b.askResumer = fn
	b.mu.Unlock()
}

// askResumerFn 加锁读当前挂起续跑钩子。
func (b *Bridge) askResumerFn() AskResumer {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.askResumer
}

// SetAskPending 装配挂起反问存在性查询（可随时替换：desktop 实时问 agentapi）。
func (b *Bridge) SetAskPending(fn func() bool) {
	b.mu.Lock()
	b.askPendingFn = fn
	b.mu.Unlock()
}

// askPending 报告当前是否有未过期挂起反问；未装配视为无。
func (b *Bridge) askPending() bool {
	b.mu.Lock()
	fn := b.askPendingFn
	b.mu.Unlock()
	return fn != nil && fn()
}

// SetDirectPatterns 装配逃生门白名单查询函数（可随时替换：desktop 读实时配置）。
func (b *Bridge) SetDirectPatterns(fn func() []string) {
	b.mu.Lock()
	b.directPatternsFn = fn
	b.mu.Unlock()
}

// SetRouteGate 装配智能链路启用门禁（可随时替换：desktop 读实时配置）。
func (b *Bridge) SetRouteGate(fn RouteGate) {
	b.mu.Lock()
	b.routeGate = fn
	b.mu.Unlock()
}

// routeGateAllows 报告智能链路是否启用：未装配门禁（nil）视为允许，
// 保持既有装配路径零影响；装配了则实时问一次（改配置即时生效）。
func (b *Bridge) routeGateAllows() bool {
	b.mu.Lock()
	fn := b.routeGate
	b.mu.Unlock()
	if fn == nil {
		return true
	}
	return fn()
}

// routerFn 加锁读当前路由钩子。
func (b *Bridge) routerFn() InputRouter {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.router
}

// directPatterns 取当前逃生门白名单：未装配或返回空时回落内置默认集。
func (b *Bridge) directPatterns() []string {
	b.mu.Lock()
	fn := b.directPatternsFn
	b.mu.Unlock()
	if fn == nil {
		return hub.DefaultDirectPatterns()
	}
	if pats := fn(); len(pats) > 0 {
		return pats
	}
	return hub.DefaultDirectPatterns()
}

// routeRoundValue 加锁读连续反问轮次。
func (b *Bridge) routeRoundValue() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.routeRound
}

// setRouteRound 覆盖连续反问轮次。
func (b *Bridge) setRouteRound(n int) {
	b.mu.Lock()
	b.routeRound = n
	b.mu.Unlock()
}

// incRouteRound 反问轮次 +1。
func (b *Bridge) incRouteRound() {
	b.mu.Lock()
	b.routeRound++
	b.mu.Unlock()
}

// QueuedFiles 返回当前待转交附件数（#st 尾注用）。
func (b *Bridge) QueuedFiles() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.queue)
}

// Undelivered 返回累计发送失败条数（#st 状态展示用）。
func (b *Bridge) Undelivered() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.undelivered
}

// flushSentinel 是 FlushForTest 投入的哨兵文本；worker 见到即回调 hook 并
// 跳过真实投递（无网络副作用，不污染计数）。
const flushSentinel = "\x00flush-sentinel"

// FlushForTest 阻塞等待出站队列排空（worker 已处理完当前队列所有消息）。
// 仅测试用：worker 串行消费，哨兵被处理即代表它之前的消息全部 deliver 完毕。
func (b *Bridge) FlushForTest() {
	b.startOutboundWorker()
	done := make(chan struct{})
	var once sync.Once
	b.flushHook = func() { once.Do(func() { close(done) }) }
	// 哨兵必须走普通队列才能保证「之前消息已处理完」的顺序语义。
	b.enqueueOutbound(outMsg{text: flushSentinel})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		panic("FlushForTest: 等待出站队列排空超时")
	}
}

// PendingCount 返回因会话过期而排队等待补发的条数。
func (b *Bridge) PendingCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending)
}

// SetOnLearnWxid 装配 wxid 学习回调（Start 前调用，此后只读）。
func (b *Bridge) SetOnLearnWxid(fn func(wxid string)) { b.onLearnWxid = fn }

// TryCancelPick 让命令层待选菜单失效（跨渠道投递成功后由 Runtime 调用）。
func (b *Bridge) TryCancelPick() bool {
	if b == nil || b.console == nil {
		return false
	}
	return b.console.TryCancelPick()
}

// PickTarget 进入待选菜单并暂存正文（与桌面 HubPickTarget 同语义；测试与
// 跨渠道清理回归用）。生产入站归因失败路径经 dispatchConsole 调用 console。
func (b *Bridge) PickTarget(text string) ([]hub.Reply, bool) {
	return b.console.PickTarget(text)
}

// HandleConsole 把文本交给命令层（测试：清待选后回数字不得再发旧正文）。
func (b *Bridge) HandleConsole(text string) []hub.Reply {
	return b.console.Handle(text)
}

// OpenPermissionMenu 让命令层进入权限等待状态（1=允许 / 2=拒绝）。
// desktop 在收到 chat:permission 事件时转发进来，使微信侧数字回复生效。
func (b *Bridge) OpenPermissionMenu(chatID, reqID, desc string) {
	b.console.OpenPermissionMenu(chatID, reqID, desc)
}

// Wxid 返回当前白名单用户（可能为学习前的空串；测试与状态展示用）。
func (b *Bridge) Wxid() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.wxid
}

// HandleInbound 处理一条纯文本入站消息（无附件的历史入口）。
func (b *Bridge) HandleInbound(fromUserID, contextToken, text string) {
	b.HandleInboundMsg(context.Background(), InboundMsg{FromUserID: fromUserID, ContextToken: contextToken, Text: text})
}

// HandleInboundMsg 处理一条入站消息：白名单过滤 → 记录发送上下文 → 补发
// pending → 附件下载落盘入队 → 文本与队列合并投递 → 回执逐条投递。
// ctx 来自长轮询 goroutine：下载附件可耗时，Stop/重绑取消要能穿透。
//
// 附件语义（2026-10-18 与用户确认）：微信客户端把「文字+附件」拆成多条消息
// 发送，附件到达即下载落盘进队列等文字；收到可转发文字时把队列全部附件以
// 「[附件] 名 已保存至 路径」说明行前置合并为一次投递；命令/纯数字不消费
// 队列；投递失败（无会话/发送错误）时队列保留并补发回执，附件不丢。
func (b *Bridge) HandleInboundMsg(ctx context.Context, m InboundMsg) {
	if !b.admit(m.FromUserID) {
		return
	}
	// 记录发送上下文：出站回执按最新入站凭据投递。
	b.mu.Lock()
	b.ctx = ctxInfo{userID: m.FromUserID, contextToken: m.ContextToken, valid: true}
	b.mu.Unlock()

	// typing 只是体验优化，失败静默。
	_ = b.client.SendTyping(context.Background(), m.FromUserID, m.ContextToken, 1)

	b.flushPending()

	// 惰性过期：先清超时附件批（回执随本条消息回复发出），再处理本条消息
	// 的附件/文字，保证下游所有队列读取方看到的是过期后的状态。
	expiredNote := b.purgeExpiredQueue()

	receipt := b.ingestAttachments(ctx, m.Attachments)

	text := m.Text
	// 语音：下载 → SILK→WAV → 落盘留档。文本为空时走本地/云端转写，转写
	// 成功即按可转发文字投递（不占附件队列，保证智能路由正常）；转写失败/
	// 未配置/结果为空才把音频入队，等用户下一条文字一并转交给多模态模型。
	var formatNote string
	if m.Voice != nil {
		note, transcript, terr := b.ingestVoice(ctx, m.Voice, strings.TrimSpace(text) == "")
		receipt = joinNotes(receipt, note)
		var uf *UnsupportedVoiceFormatError
		if errors.As(terr, &uf) {
			// 非 SILK 载荷：无论服务端是否给了转写文本都要回执，避免
			// 解码失败被静默吞掉（用户至少能看到真实格式与落盘样本）。
			// 单独存为 formatNote：即使后续走合并投递（merged），该回执
			// 也不能被 `!merged` 条件抑制。
			formatNote = voiceFormatReceipt(b.lang, uf)
		} else if strings.TrimSpace(text) == "" {
			switch {
			case errors.Is(terr, ErrTranscriberNotConfigured):
				receipt = joinNotes(receipt, bridgeText(b.lang, "voice.unconfigured"))
			case errors.Is(terr, ErrVoiceUnsupported):
				// 模型不支持音频输入：给配置指引，不透传网关报错。
				receipt = joinNotes(receipt, bridgeText(b.lang, "voice.unsupported"))
			case terr != nil:
				receipt = joinNotes(receipt, fmt.Sprintf(bridgeText(b.lang, "voice.failed"), terr))
			case strings.TrimSpace(transcript) == "":
				receipt = joinNotes(receipt, fmt.Sprintf(bridgeText(b.lang, "voice.failed"), bridgeText(b.lang, "voice.empty")))
			default:
				text = transcript
			}
		}
	}

	var replies []hub.Reply
	merged := false
	trimmed := strings.TrimSpace(text)
	// 引用归因只对可转发文字生效：命令（#l 等）与纯数字（编号/菜单）保持
	// 原语义，引用前缀不劫持它们（2026-10-20 设计决策）。
	quotedChatID, quotedSession := "", ""
	if m.Ref != nil && isForwardableText(text) {
		quotedChatID, quotedSession = b.resolveQuote(m.Ref)
	}
	// 可转发文字（非命令/纯数字）且队列非空：合并投递。
	// quoted：用户明确引用了某条消息（可转发正文）。归因失败时不得发当前会话，
	// 须进待选让用户选目标（见 dispatchConsole）。
	quoted := m.Ref != nil && isForwardableText(text)
	if isForwardableText(text) && b.QueuedFiles() > 0 {
		text = b.queuedPrefix() + text
		replies = b.dispatchConsole(ctx, text, quotedChatID, quotedSession, quoted)
		// 合并成功的三种形态：确定性直投（无回复）、管理者/反问续跑接管
		// （dispatchConsole 已消费队列）、路由决策已发送（Sent 回执）。
		// 任一成立都不再补「附件待转交」回执，避免刚转交又提示仍在队列。
		forwarded := len(replies) == 0 || b.QueuedFiles() == 0
		for _, r := range replies {
			if r.Sent {
				forwarded = true
				break
			}
		}
		if forwarded {
			b.clearQueue()
			merged = true
			if len(replies) == 0 {
				// 合并成功转发（agent 稍后回复）：微信侧补既有 sent_ack。
				replies = []hub.Reply{{Text: b.deliveredAck()}}
			}
		}
		// 转发失败/待选/反问（有回复但未发送）：队列保留，回执照发。
	} else if trimmed != "" {
		replies = b.dispatchConsole(ctx, text, quotedChatID, quotedSession, quoted)
		if len(replies) == 0 {
			// 命令层无回复 = 文本已成功转发给当前会话（agent 稍后回复）。
			// 微信侧补一条已接收确认，与桌面端 AgentHub 的 ack 卡片同一文案语义
			//（ui.hub.ack_delivered 不镜像到微信，这里是对应的微信侧回执）。
			replies = []hub.Reply{{Text: b.deliveredAck()}}
		}
	}
	// 未合并时补附件回执（收到/失败）；合并成功时已含说明行不再重复。
	if !merged && receipt != "" {
		replies = append(replies, hub.Reply{Text: receipt})
	}
	// 非 SILK 格式回执独立于 merged 抑制：即便语音文本已随附件合并投递，
	// 也要让用户知道音频未解码，并给出可定位的样本。
	if formatNote != "" {
		replies = append(replies, hub.Reply{Text: formatNote})
	}
	// 超时丢弃回执独立于 merged 抑制（与 formatNote 同待遇）：即便本条消息
	// 合并/投递成功，也要让用户知道上一批附件已被忽略，不静默消失。
	if expiredNote != "" {
		replies = append(replies, hub.Reply{Text: expiredNote})
	}
	// #st 末尾追加待转交附件数，防用户遗忘队列（N>0 才追加）。按空白取第一
	// 段与 "#st"/"＃st" 精确比较，避免 "#stop" 等未来命令误伤。
	if first := strings.Fields(trimmed); len(first) > 0 && (first[0] == "#st" || first[0] == "＃st") {
		if n := b.QueuedFiles(); n > 0 {
			replies = append(replies, hub.Reply{
				Text: fmt.Sprintf(bridgeText(b.lang, "file.queued_st"), n),
			})
		}
	}
	// 待选编号发送成功：暂存正文（可能已带附件说明前缀）已投递，消费队列，
	// 避免下一条普通文本重复附带同一批附件。
	for _, r := range replies {
		if r.Sent {
			b.clearQueue()
			break
		}
	}
	for _, r := range replies {
		b.deliver(r.Text, "", "", "") // 命令回复/ack 不归属会话，不进引用映射
	}
	// 回复已发出，取消输入状态。
	_ = b.client.SendTyping(context.Background(), m.FromUserID, m.ContextToken, 2)
}

// dispatchConsole 把可转发文本交给命令层。
// quotedChatID / sessionHint 任一非空时先走引用路由（切换到被引用会话再投递）。
// quoted 表示用户明确引用了消息：归因失败或 chatID 不在列表时进待选
// （PickTarget），不得回落发「当前会话」——引用已表明不想跟默认目标。
// 无引用（quoted=false）且非逃生门/菜单挂起/无附件队列时，自由文本（含纯
// 数字，语境由路由器决断）先问智能路由器；路由器消费则按决策落 Console。
//
// 智能链路失败的处理（2026-11-18 真机事故复盘）：管理者循环与单发路由都未
// 接管时**不得**回落 Handle(原文)——那会按归因水位把正文直投"当前会话"，
// 与用户正文里点名的项目/工具无关（事故里投给了 strategy 的 agent，它再用
// kshell CLI 跨项目自建会话，看起来像"多路投递"）。失败一律转 PickTarget
// 待选并附一条说明原因的回执，让用户自己指目标。
//
// 门禁关闭（AgentHub 总开关 off）时整条智能链路不参与，保持旧直投语义。
func (b *Bridge) dispatchConsole(ctx context.Context, text, quotedChatID, sessionHint string, quoted bool) []hub.Reply {
	// 引用与待选分支不进路由：反问链被打断，轮次清零（防悬挂）。
	if quotedChatID != "" || sessionHint != "" {
		b.setRouteRound(0)
		if handled, replies := b.console.HandleQuoted(quotedChatID, sessionHint, text); handled {
			return replies
		}
	}
	if quoted {
		b.setRouteRound(0)
		replies, _ := b.console.PickTarget(text)
		return replies
	}
	fn := b.routerFn()
	mgr := b.managerRouterFn()
	ask := b.askResumerFn()
	queued := b.QueuedFiles() > 0
	// 智能链路是否启用：任一钩子装配且门禁允许。未启用按旧语义直投当前会话
	//（"未启用智能路由"是正常用法，不能拦截）。
	smartOn := (fn != nil || mgr != nil || ask != nil) && b.routeGateAllows()
	// 只有合并文本且智能链路启用才需要问挂起态；其余保持既有路由开销。
	pendingAsk := queued && smartOn && b.askPending()
	// 路由资格判定（T2 规格）：纯数字（菜单/编号切换）也问路由器，语境由路由
	// 器决断；命令/逃生门、菜单挂起一律走确定性 Handle。
	// 附件合并文本只在「有挂起反问」时进入智能链路——回答必须续跑原任务；
	// 无挂起时不自动猜目标（下方 smartOn+queued 分支转待选）。
	// 不进路由的分支同时清零轮次，保证反问链被打断后轮次不悬挂。
	routeable := smartOn &&
		!b.console.Busy() &&
		(!queued || pendingAsk) &&
		!hub.IsDirectPass(text, b.directPatterns())
	if routeable {
		// 管理者循环优先（逃生门之后、单发路由之前）：命中即把结论 Replies
		// 逐条回显，不再走 executeRouteText 决策执行；接管同时清零 InputRouter
		// 反问轮次（反问链已移交 agentapi pendingAsk 管理）。Replies 为空的
		// 接管属异常形态，记一条失败原因继续走单发路由，避免误报"已送达"。
		failureKey := ""
		switch {
		case queued && pendingAsk && ask != nil:
			// 2026-11-19 真机复盘：合并文本带附件说明行，必然超过 agentapi
			// 的短输入门槛（20 rune），Run 会当成新需求清挂起。必须显式
			// Resume 才能把回答续进原任务。
			out, handled := ask(ctx, text)
			if handled && len(out.Replies) > 0 {
				b.setRouteRound(0)
				// 续跑成功 = 合并文本（含附件说明行）已被循环接收，附件不再
				// 等下一条文字，直接消费队列。
				b.clearQueue()
				return b.managerReplies(out)
			}
			failureKey = out.FailureKey
		case mgr != nil:
			out, handled := mgr(ctx, text)
			if handled && len(out.Replies) > 0 {
				b.setRouteRound(0)
				if queued {
					b.clearQueue()
				}
				return b.managerReplies(out)
			}
			failureKey = out.FailureKey
		}
		// 反问链已满：本轮不再走路由，直接进待选（不再降级直投原文）。
		if b.routeRoundValue() >= hubtools.AskMaxRounds {
			b.setRouteRound(0)
		} else if fn != nil {
			if d, handled := fn(ctx, text); handled {
				if replies := b.executeRouteText(ctx, text, d); len(replies) > 0 {
					if d.Action == "ask" {
						b.incRouteRound()
					} else {
						b.setRouteRound(0)
					}
					return replies
				}
				// 路由器消费但决策不可执行（编号漂移等）：轮次清零，转待选。
			}
			b.setRouteRound(0)
		}
		return b.degradeToPick(text, failureKey)
	}
	// 智能链路启用时，附件合并文本绝不直投当前会话：没有挂起反问就无法
	// 确定目标，转待选让用户指。附件队列保留（待选成功后由 Sent 逻辑消费）。
	// 未启用智能链路（门禁关闭/无钩子）时保持旧语义，由 Console.Handle 处理。
	if queued && smartOn {
		b.setRouteRound(0)
		return b.degradeToPick(text, "err.router.attachment_needs_target")
	}
	b.setRouteRound(0)
	return b.console.Handle(text)
}

// managerReplies 把管理者接管结果转成桥回复：挂起反问在首条加任务归因
// 前缀，让用户看清「回答将续跑哪条任务」；非挂起结论原样回显。
func (b *Bridge) managerReplies(out ManagerOutcome) []hub.Reply {
	replies := make([]hub.Reply, 0, len(out.Replies))
	for i, r := range out.Replies {
		if i == 0 && out.Suspended && out.Attribution != "" {
			r = fmt.Sprintf(bridgeText(b.lang, "ask.attribution"), out.Attribution) + r
		}
		replies = append(replies, hub.Reply{Text: r})
	}
	return replies
}

// degradeToPick 是智能链路失败的统一出口：把原文暂存进待选菜单并回一条
// 说明失败原因的降级回执。**绝不直投当前会话**——正文点名的目标与归因水位
// 无关，猜错会把任务投进无关项目（真机事故）。
//
// 裸数字例外：数字是既有确定性语义（#l/#h 后的编号切换、菜单选择），不是
// "猜目标"的自由文本。把它塞进新待选菜单会把编号答案当正文再挂一层菜单，
// 破坏命令层语境，故数字仍交 Handle 按编号处理。
//
// 待选建立失败（无会话 / 其他菜单占用）时仍回执说明，正文交给 PickTarget
// 的既有回复（pick_busy 等）体现；用户可 #l 自行指目标。
func (b *Bridge) degradeToPick(text, failureKey string) []hub.Reply {
	if hub.IsBareNumber(text) {
		return b.console.Handle(text)
	}
	reason := b.routeFailureReason(failureKey)
	replies, pending := b.console.PickTarget(text)
	if !pending {
		out := make([]hub.Reply, 0, len(replies)+1)
		out = append(out, hub.Reply{Text: fmt.Sprintf(bridgeText(b.lang, "route.pick_unavailable"), reason)})
		out = append(out, replies...)
		return out
	}
	out := make([]hub.Reply, 0, len(replies)+1)
	out = append(out, hub.Reply{Text: fmt.Sprintf(bridgeText(b.lang, "route.pick_degraded"), reason)})
	out = append(out, replies...)
	return out
}

// routeFailureReason 把失败 key 渲染成用户可读原因；空/未知 key 回落
// 「未识别到投递目标」（模型都没跑起来的缺省归因）。未知 key 必须回落而不是
// 原样透出——wire key 直接显示给用户比没有原因更糟。
func (b *Bridge) routeFailureReason(key string) string {
	if _, ok := bridgeTexts[key]; !ok {
		key = "err.router.no_target"
	}
	return bridgeText(b.lang, key)
}

// executeRouteText 把路由决策落成 bridge 侧 Console 动作（desktop executeRoute
// 的微信同构实现）。返回空切片/nil 表示无法执行，调用方转待选（degradeToPick，
// 不再直投当前会话）。
//
//   - group: ListSessions 编号 1..N（与 #l 同源）→ SendGroupTexts；
//     texts 优先 PerTargetText[num] 退 Text，任一目标空文本整批降级。
//   - single 非空 targets: 恰 1 个合法编号 → 单目标 SendGroupTexts；空 targets
//     返回 nil（调用方转待选，不猜当前会话）。
//   - status: 合成 "#st" 经 Console.Handle。
//   - expand: 合成 "#gx N [count]" 经 Console.Handle（<=0 省略条数）。
//   - ask: 单条回执 = CmdText(lang, "route_ask_prefix") + Question。
//
// 锁纪律：Console 的 Handle/SendGroupTexts 各自持锁且按序调用，本函数不持
// bridge 锁，也不手写 Console 锁。ctx/original 仅用于接口对齐与未来观测，
// 降级处理始终由调用方用原文完成（degradeToPick）。
//   - 形参 ctx/text 当前未使用：ctx 预留观测埋点，text 保留签名对齐调用方
//     （降级转待选时由调用方持有原文），二者均不消费。
func (b *Bridge) executeRouteText(_ context.Context, _ string, d hubtools.RouteDecision) []hub.Reply {
	switch d.Action {
	case "group":
		byNum := b.sessionNumIndex()
		refs := make([]hub.SessionRef, 0, len(d.Targets))
		texts := make([]string, 0, len(d.Targets))
		seen := make(map[int]bool, len(d.Targets))
		for _, n := range d.Targets {
			if seen[n] {
				continue
			}
			seen[n] = true
			ref, ok := byNum[n]
			if !ok {
				return nil
			}
			text := d.TextFor(n)
			// 任一目标文本为空则整批降级（不做部分投递，避免空串计成功）。
			if strings.TrimSpace(text) == "" {
				return nil
			}
			refs = append(refs, ref)
			texts = append(texts, text)
		}
		if len(refs) < 2 {
			return nil
		}
		return b.console.SendGroupTexts(refs, texts)

	case "single":
		if len(d.Targets) != 1 {
			return nil
		}
		n := d.Targets[0]
		ref, ok := b.sessionNumIndex()[n]
		if !ok {
			return nil
		}
		text := d.TextFor(n)
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return b.console.SendGroupTexts([]hub.SessionRef{ref}, []string{text})

	case "status":
		return b.console.Handle("#st")

	case "expand":
		if d.ExpandNum <= 0 {
			return nil
		}
		if d.ExpandCount > 0 {
			return b.console.Handle(fmt.Sprintf("#gx %d %d", d.ExpandNum, d.ExpandCount))
		}
		return b.console.Handle(fmt.Sprintf("#gx %d", d.ExpandNum))

	case "ask":
		q := strings.TrimSpace(d.Question)
		if q == "" {
			return nil
		}
		return []hub.Reply{{Text: hub.CmdText(b.lang, "route_ask_prefix") + q}}
	}
	return nil
}

// sessionNumIndex 构建编号 → SessionRef 映射（编号=ListSessions 索引+1，与
// #l/路由快照同源）。仅 group/single 分支需要。
//
// 已知暴露面（编号 TOCTOU）：路由快照与执行映射是两次独立的 ListSessions
// 调用，窗口内会话增删会使编号漂移（决策指向的编号在执行时可能映射到另一
// 会话）。后续可改为决策随快照一起返回 SessionRef，消除二次映射。
func (b *Bridge) sessionNumIndex() map[int]hub.SessionRef {
	sessions := b.ex.ListSessions()
	byNum := make(map[int]hub.SessionRef, len(sessions))
	for i := range sessions {
		ref := sessions[i]
		ref.Num = i + 1
		byNum[ref.Num] = ref
	}
	return byNum
}

// deliveredAck 是「文本已成功转发给当前会话」的微信侧回执，与桌面端
// AgentHub 的 ack 卡片同一文案语义，带【会话名】归因前缀（无活跃会话不加）。
func (b *Bridge) deliveredAck() string {
	ack := hub.CmdText(b.lang, "sent_ack")
	if title := b.console.CurrentTitle(); title != "" {
		ack = "【" + title + "】" + ack
	}
	return ack
}

// admit 白名单过滤与 wxid 首条学习；未通过返回 false（静默丢弃）。
func (b *Bridge) admit(fromUserID string) bool {
	// wxid 待学习：iLink 扫码响应不带 wxid，首条白名单外的入站消息
	// 即视为机主本人（首扫即绑），学习一次后按白名单过滤。学习与
	// 白名单判断共用 mu，保证并发消息里只有一条能成为「首条」。
	b.mu.Lock()
	if b.wxid == "" {
		if fromUserID == "" {
			b.mu.Unlock()
			return false
		}
		b.wxid = fromUserID
		// 学习后加载该账号落盘归因（扫码绑定路径 Configure 时 wxid 常为空）。
		if b.svrMapPath != "" {
			m, order := loadSvrMapAccount(b.svrMapPath, fromUserID)
			b.svrMap = m
			b.svrOrder = order
		}
		fn := b.onLearnWxid
		b.mu.Unlock()
		log.Printf("[weixin] 学习白名单 wxid=%s（首条消息）", fromUserID)
		if fn != nil {
			fn(fromUserID)
		}
		return true
	}
	ok := fromUserID == b.wxid
	b.mu.Unlock()
	if !ok {
		log.Printf("[weixin] 非白名单消息丢弃 from=%s", fromUserID)
	}
	return ok
}

// ingestAttachments 下载落盘每个附件并入队；返回微信侧回执文本（无新附件
// 或全部成功且即将合并时也可为空——回执是否发出由调用方按合并结果决定）。
// 下载/落盘在长轮询 goroutine 内同步执行：附件体积受微信限制，秒级阻塞可接受；
// ctx 取消（Stop/重绑）时未完成的附件按失败处理，不阻塞退出。
func (b *Bridge) ingestAttachments(ctx context.Context, atts []InboundAttachment) string {
	if len(atts) == 0 {
		return ""
	}
	var got, failed []string
	for _, att := range atts {
		name := att.Name
		if name == "" {
			name = att.Kind + ".bin"
		}
		data, err := b.client.DownloadMedia(ctx, att)
		if err == nil {
			var path string
			path, err = saveAttachment(b.filesDir, name, data)
			if err == nil {
				b.mu.Lock()
				b.queue = append(b.queue, queuedFile{Name: name, Path: path})
				b.queueAt = b.now() // 整批以最新一张为基准：新附件续期过期计时
				total := len(b.queue)
				b.mu.Unlock()
				log.Printf("[weixin] 附件已落盘待转交 name=%s path=%s 队列=%d", name, path, total)
				got = append(got, name)
				continue
			}
		}
		log.Printf("[weixin] 附件获取失败 name=%s: %v", name, err)
		failed = append(failed, name)
	}
	var parts []string
	if len(got) > 0 {
		b.mu.Lock()
		total := len(b.queue)
		b.mu.Unlock()
		parts = append(parts, fmt.Sprintf(bridgeText(b.lang, "file.received"),
			strings.Join(got, "、"), total))
	}
	for _, name := range failed {
		parts = append(parts, fmt.Sprintf(bridgeText(b.lang, "file.failed"), name))
	}
	return strings.Join(parts, "\n")
}

// ingestVoice 完成语音的文件化与转写：CDN 下载 → SILK 解码转 WAV → 落盘留档。
// needTranscribe 时交给注入的 transcriber 得转写文本。
//
// 队列语义（2026-11-20 真机复盘）：**只有拿不到可用文字时**才把音频放进
// 附件转交队列（转写失败/未配置/结果为空），作为「发一条文字即可连带转交
// 音频给多模态模型」的兜底。转写成功（含服务端直给文本）时音频仅留档、
// 不入队——否则文本会带附件说明行，被智能链路判成「附件消息」而降级待选。
//
// 返回：note 是微信侧回执片段（仅入队时非空，file.received 的「待转交」
// 不能对已直接投递的语音发送）；transcript 与 err 语义同前。
func (b *Bridge) ingestVoice(ctx context.Context, v *InboundAttachment, needTranscribe bool) (note, transcript string, err error) {
	wav, err := b.voiceWAV(ctx, v)
	if err != nil {
		return "", "", err
	}
	name := "voice.wav"
	path, err := saveAttachment(b.filesDir, name, wav)
	if err != nil {
		log.Printf("[weixin] 语音落盘失败 dir=%s err=%v", b.filesDir, err)
		return "", "", err
	}
	// enqueue 把音频放进附件队列并返回「已收到」回执；仅兜底路径调用。
	enqueue := func() string {
		b.mu.Lock()
		b.queue = append(b.queue, queuedFile{Name: name, Path: path})
		b.queueAt = b.now() // 语音与附件同队列同待遇：入队即续期过期计时
		total := len(b.queue)
		b.mu.Unlock()
		log.Printf("[weixin] 语音已落盘待转交 path=%s 队列=%d", path, total)
		return fmt.Sprintf(bridgeText(b.lang, "file.received"), name, total)
	}
	if !needTranscribe {
		// 服务端已提供转写文本：音频只留档，不占队列。
		log.Printf("[weixin] 语音已留档（服务端转写可用，不入队） path=%s", path)
		return "", "", nil
	}
	fn := b.transcriberFn()
	if fn == nil {
		log.Printf("[weixin] 语音转写未配置（无 ASR 模型） path=%s", path)
		return enqueue(), "", ErrTranscriberNotConfigured
	}
	// 转写结果与耗时必须可观测：2026-11-20 真机事故里，网关 200 返回的
	// 「拒答」被当成转写投递，日志只有「已落盘」，完全看不出中间发生过什么。
	start := time.Now()
	transcript, err = fn(ctx, wav)
	elapsed := time.Since(start).Round(time.Millisecond)
	trimmedTranscript := strings.TrimSpace(transcript)
	if err != nil {
		// rejected 标记这轮是「模型/网关拒绝音频」而非网络或鉴权故障，
		// 便于日志侧区分偶发失败与配置性缺陷。
		log.Printf("[weixin] 语音转写失败 path=%s elapsed=%s rejected=%t err=%v",
			path, elapsed, errors.Is(err, ErrVoiceUnsupported), err)
		return enqueue(), "", err
	}
	if trimmedTranscript == "" {
		log.Printf("[weixin] 语音转写结果为空 path=%s elapsed=%s", path, elapsed)
		return enqueue(), transcript, nil
	}
	log.Printf("[weixin] 语音转写完成 path=%s len=%d elapsed=%s prefix=%q",
		path, len([]rune(trimmedTranscript)), elapsed, voicePrefix(trimmedTranscript))
	return "", transcript, nil
}

// voicePrefix 取转写文本前 40 个字符（按字符截断），供日志快速判断内容是
// 真实转写还是模型拒答。
func voicePrefix(s string) string {
	r := []rune(s)
	if len(r) > 40 {
		return string(r[:40]) + "…"
	}
	return s
}

// maxVoiceDumpSize 是失败语音原始载荷的落盘上限：保留足够识别格式的前缀
// 即可，避免异常大载荷反复占满 weixin-files。
const maxVoiceDumpSize = 1 << 20

// dumpRawVoice 把解密后的失败语音载荷落盘（限 1MB），返回绝对路径；
// 落盘失败只记日志并返回空串（诊断信息不应阻断回执）。
func (b *Bridge) dumpRawVoice(data []byte) string {
	raw := data
	if len(raw) > maxVoiceDumpSize {
		raw = raw[:maxVoiceDumpSize]
	}
	path, err := saveAttachment(b.filesDir, "voice-raw.bin", raw)
	if err != nil {
		log.Printf("[weixin] 语音失败样本落盘失败 len=%d err=%v", len(data), err)
		return ""
	}
	return path
}

// voiceWAV 下载语音媒体并解码为 WAV（SILK / OPUS_RAW_V1 → 24kHz 单声道 PCM，
// voice.go）。失败按阶段落日志：下载与解码分开记录，解码失败附带格式与首部
// 字节，不支持的载荷额外保存原始样本，供后续确认上游实际编码。
func (b *Bridge) voiceWAV(ctx context.Context, v *InboundAttachment) ([]byte, error) {
	data, err := b.client.DownloadMedia(ctx, *v)
	if err != nil {
		log.Printf("[weixin] 语音下载失败 size=%d err=%v", v.Size, err)
		return nil, fmt.Errorf("下载语音: %w", err)
	}
	wav, err := decodeVoice(data)
	if err != nil {
		var uf *UnsupportedVoiceFormatError
		if errors.As(err, &uf) {
			uf.DumpPath = b.dumpRawVoice(data)
			log.Printf("[weixin] 语音解码失败 format=%s len=%d head=%X dump=%s err=%v",
				uf.Format, len(data), uf.Head, uf.DumpPath, err)
		} else {
			log.Printf("[weixin] 语音解码失败 len=%d head=%X err=%v", len(data), voiceHeadBytes(data), err)
		}
		return nil, err
	}
	return wav, nil
}

// voiceFormatReceipt 生成非 SILK 载荷的用户回执：有失败样本时给出路径，
// 便于用户把文件交给开发者确认实际编码。
func voiceFormatReceipt(lang string, uf *UnsupportedVoiceFormatError) string {
	if uf.DumpPath != "" {
		return fmt.Sprintf(bridgeText(lang, "voice.unsupported_format_dump"), uf.Format, uf.DumpPath)
	}
	return fmt.Sprintf(bridgeText(lang, "voice.unsupported_format"), uf.Format)
}

// joinNotes 用换行合并回执片段（忽略空片段）。
func joinNotes(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "\n" + b
}

// queuedPrefix 取队列全部附件的说明行块（每附件一行 + 空行分隔），不消费队列；
// 仅在合并投递成功后由 clearQueue 清空。
func (b *Bridge) queuedPrefix() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var sb strings.Builder
	for _, q := range b.queue {
		sb.WriteString(fmt.Sprintf(bridgeText(b.lang, "file.line"), q.Name, q.Path))
		sb.WriteByte('\n')
	}
	sb.WriteByte('\n')
	return sb.String()
}

// clearQueue 清空待转交附件队列（合并投递成功后调用）。
func (b *Bridge) clearQueue() {
	b.mu.Lock()
	b.queue = nil
	b.queueAt = time.Time{}
	b.mu.Unlock()
}

// now 取当前时间：测试注入假时钟（nowFn），nil 回落 time.Now。
func (b *Bridge) now() time.Time {
	if b.nowFn != nil {
		return b.nowFn()
	}
	return time.Now()
}

// attachmentTTL 是附件等待配套文字的最长时间；超时在下一条消息入口惰性丢弃。
const attachmentTTL = time.Minute

// purgeExpiredQueue 惰性过期：距最近一次附件入队超过 TTL 且期间无可转发文字
// 消费时，整批丢弃队列附件并返回给用户的超时回执（无过期返回空串）。
// 计时以最新一张入队时间为基准（新附件续期整批）＝空闲超时语义，多图连发
// （微信逐条推送 + 下载秒级耗时）不会被最早一张的计时误伤。
//
// 两个守卫期间不丢：挂起反问（askPending）是 2026-11-19 续跑流要保护的
// 「用户思考后补文字回答」，且此时附件本就不会触发待选菜单；待选菜单挂起
// （console Busy）的暂存正文已含附件说明行，丢弃会造成悬空引用。守卫在持
// b.mu 之前无锁快照（console 回调进 bridge 有持锁回调死锁前科）；入站单
// goroutine 串行保证快照与清理之间队列无并发写。
//
// 只在 HandleInboundMsg 入口调用：合并判定、queued 标记、待选分支、#st 尾注
// 全部在其后执行，看到的都是过期后的队列状态——附件丢弃后不再弹
// 「附件消息需要指定目标会话」待选菜单正是靠这个时序。
func (b *Bridge) purgeExpiredQueue() string {
	busy := b.console.Busy()
	askPending := b.askPendingFn != nil && b.askPendingFn()
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.queue) == 0 || busy || askPending {
		return ""
	}
	if now := b.now(); now.Sub(b.queueAt) < attachmentTTL {
		return ""
	}
	names := make([]string, 0, len(b.queue))
	for _, q := range b.queue {
		names = append(names, q.Name)
	}
	n := len(b.queue)
	b.queue = nil
	b.queueAt = time.Time{}
	log.Printf("[weixin] 附件超时忽略 count=%d names=%s", n, strings.Join(names, "、"))
	return fmt.Sprintf(bridgeText(b.lang, "file.expired"), n, strings.Join(names, "、"))
}

// isForwardableText 判断文本是否可携带附件队列转发：非空、非命令（# 与全角＃）、
// 非纯数字（菜单/编号切换）。命令语义不应被附件说明行污染，也不消费队列。
func isForwardableText(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "＃") {
		return false
	}
	for _, r := range t {
		if r < '0' || r > '9' {
			return true
		}
	}
	return false // 全数字：菜单选择/编号切换
}

// DeliverHubMessage 把 hub 的 agent 消息镜像到微信出站队列（串行 worker
// 按序发送，保证顺序；队列满则丢弃计 undelivered，不阻塞调用方）。
// 只镜像 agent 正文：用户消息、已撤回、ui.hub.* 事件、hub.welcome 欢迎词
// （微信侧由命令层欢迎词覆盖，避免重复问候）与 wire key 形态文本
// （后端稳定 key，非自然语言）一律跳过，防止刷屏。
func (b *Bridge) DeliverHubMessage(m hub.Message) {
	if m.Kind != "agent" || m.Status == hub.StatusRecalled {
		return
	}
	if strings.HasPrefix(m.Event, "ui.hub.") || strings.HasPrefix(m.Text, "ui.hub.") ||
		(m.Text == "hub.welcome" || strings.HasPrefix(m.Text, "hub.welcome|")) ||
		(m.Event == "" && isWireKey(m.Text)) {
		return
	}
	text := markdownToText(m.Text)
	if bar := b.titleBar(m); bar != "" {
		text = bar + "\n" + text
	}
	b.enqueueOutbound(outMsg{
		chatID: m.ChatID, sessionID: m.SessionID, title: m.SessionTitle, text: text,
	})
}

// outMsg 是一条待投递的出站文本。chatID 是归属 agent 会话：
// 发送成功后缓存「响应 message_id → 归因」供引用回复；空串不缓存。
type outMsg struct {
	chatID    string
	sessionID string
	title     string
	text      string
}

// titleBar 组装出站镜像消息的标题栏（独立一行，与正文换行分隔，不再同行拼接）：
// 项目名 · agent 展示名 · 会话标题，缺项跳过，全空返回空串。项目名取工作区
// 路径基名；agent 名查注入的展示名（回落工具 ID）。
func (b *Bridge) titleBar(m hub.Message) string {
	var parts []string
	if m.Workspace != "" {
		parts = append(parts, filepath.Base(m.Workspace))
	}
	if m.Tool != "" {
		b.mu.Lock()
		fn := b.toolName
		b.mu.Unlock()
		name := ""
		if fn != nil {
			name = fn(m.Tool)
		}
		if name == "" {
			name = m.Tool
		}
		parts = append(parts, name)
	}
	if m.SessionTitle != "" {
		parts = append(parts, m.SessionTitle)
	}
	if len(parts) == 0 {
		return ""
	}
	return "【" + strings.Join(parts, " · ") + "】"
}

// enqueueOutbound 把一条文本放入串行出站队列；满或已关闭则丢弃计 undelivered。
// 首次入队时惰性启动出站 worker：生产路径只有这里会往队列投消息，
// worker 只在 FlushForTest（测试）与 Close（排空退出）被调是不够的——
// 否则队列永远无人消费，agent 回复全部滞留（2026-10-18 真机「微信收不到
// agent 回复、命令回复正常」的根因）。
func (b *Bridge) enqueueOutbound(m outMsg) {
	b.startOutboundWorker()
	b.outMu.Lock()
	if b.outClosed {
		b.outMu.Unlock()
		b.mu.Lock()
		b.undelivered++
		b.mu.Unlock()
		return
	}
	select {
	case b.outbound <- m:
		b.outMu.Unlock()
	default:
		b.outMu.Unlock()
		b.mu.Lock()
		b.undelivered++
		b.mu.Unlock()
	}
}

// startOutboundWorker 惰性启动单个出站 worker goroutine（与 Close 互斥，
// 避免「先入队后关通道」与「worker 从未启动」交错）。
func (b *Bridge) startOutboundWorker() {
	b.workerOnce.Do(func() {
		go func() {
			defer close(b.workerDone)
			for m := range b.outbound {
				if m.text == flushSentinel {
					if b.flushHook != nil {
						b.flushHook()
					}
					continue
				}
				b.deliver(m.text, m.chatID, m.sessionID, m.title)
			}
		}()
	})
}

// Close 停止出站投递：关闭队列并等待 worker 排空退出。幂等；与入队并发
// 安全（outMu 保证已关闭后入队直接丢弃，不触发 send-on-closed）。
func (b *Bridge) Close() {
	b.startOutboundWorker()
	b.outMu.Lock()
	if !b.outClosed {
		b.outClosed = true
		close(b.outbound)
	}
	b.outMu.Unlock()
	<-b.workerDone
}

// isWireKey 报告文本是否为后端稳定 key 形态。
func isWireKey(s string) bool {
	return s != "" && !strings.ContainsAny(s, " \t\n") && len(s) < 64 && reWireKey.MatchString(s)
}

// rememberSvrID 是 rememberSvrAttr 的兼容入口（仅 chatID，无 SessionID/标题）。
func (b *Bridge) rememberSvrID(svrID, chatID string) {
	b.rememberSvrAttr(svrID, chatID, "", "")
}

// rememberSvrAttr 记录「出站消息 ID → 会话归因」并异步落盘；svrID 或 chatID
// 为空、重复时不记（拿不到 message_id 即降级标题匹配，不报错）。
func (b *Bridge) rememberSvrAttr(svrID, chatID, sessionID, title string) {
	if svrID == "" || chatID == "" {
		return
	}
	b.mu.Lock()
	if _, dup := b.svrMap[svrID]; dup {
		b.mu.Unlock()
		return
	}
	b.svrMap[svrID] = svrAttr{
		ChatID: chatID, SessionID: sessionID, Title: title, Ts: time.Now().Unix(),
	}
	b.svrOrder = append(b.svrOrder, svrID)
	for len(b.svrOrder) > svrMapPersistCap {
		old := b.svrOrder[0]
		b.svrOrder = b.svrOrder[1:]
		delete(b.svrMap, old)
	}
	path, wxid := b.svrMapPath, b.wxid
	snap := make(map[string]svrAttr, len(b.svrMap))
	for k, v := range b.svrMap {
		snap[k] = v
	}
	order := append([]string(nil), b.svrOrder...)
	b.mu.Unlock()
	if err := saveSvrMapAccount(path, wxid, snap, order); err != nil {
		log.Printf("[weixin] 落盘 svrMap 失败: %v", err)
	}
}

// resolveQuote 按引用归因出目标 chatID 与 sessionHint：消息 ID 精确映射优先，
// 标题栏匹配兜底；都不命中返回空（调用方对「有引用」进待选，无引用才发当前）。
// sessionHint 供 chatID 过期且 hub 无快照时 OpenSession。
// 标题栏优先匹配与出站 titleBar 同构的「项目 · 工具 · 会话」全文，再回落
// 纯会话标题（ack【会话名】形态）；微信截断无闭合】时按前缀匹配复合栏。
func (b *Bridge) resolveQuote(ref *RefMsg) (chatID, sessionID string) {
	if ref == nil {
		return "", ""
	}
	b.mu.Lock()
	attr := b.svrMap[ref.SvrID]
	b.mu.Unlock()
	if attr.ChatID != "" {
		return attr.ChatID, attr.SessionID
	}
	title := quoteTitle(ref.Text)
	if title == "" {
		return "", ""
	}
	var titleOnly, titleOnlySID string
	var prefixHit, prefixSID string
	for _, s := range b.ex.ListSessions() {
		if s.ChatID == "" {
			continue
		}
		bar := b.sessionBarInner(s)
		if bar == title {
			return s.ChatID, s.SessionID
		}
		// 截断摘要：栏内容以 title 为前缀（或 title 以栏为前缀，防极端短截断）。
		if prefixHit == "" && bar != "" &&
			(strings.HasPrefix(bar, title) || strings.HasPrefix(title, bar)) {
			prefixHit, prefixSID = s.ChatID, s.SessionID
		}
		if s.Title == title && titleOnly == "" {
			titleOnly, titleOnlySID = s.ChatID, s.SessionID
		}
	}
	if prefixHit != "" {
		return prefixHit, prefixSID
	}
	return titleOnly, titleOnlySID
}

// sessionBarInner 组装与 titleBar 同构的栏内文案（无【】包裹），供引用
// 标题兜底比对。工具展示名优先 SessionRef.ToolName，再查注入的 toolName，
// 最后回落 Tool ID。
func (b *Bridge) sessionBarInner(s hub.SessionRef) string {
	var parts []string
	if s.Workspace != "" {
		parts = append(parts, filepath.Base(s.Workspace))
	}
	if s.Tool != "" || s.ToolName != "" {
		name := s.ToolName
		if name == "" {
			b.mu.Lock()
			fn := b.toolName
			b.mu.Unlock()
			if fn != nil {
				name = fn(s.Tool)
			}
		}
		if name == "" {
			name = s.Tool
		}
		if name != "" {
			parts = append(parts, name)
		}
	}
	if s.Title != "" {
		parts = append(parts, s.Title)
	}
	return strings.Join(parts, " · ")
}

// quoteTitle 从引用文本里提取出站的【标题栏】前缀；无前缀返回空串。
// 微信摘要常截断：有开【无闭】时仍返回栏内已见片段，供 resolveQuote 前缀匹配。
var reQuoteTitle = regexp.MustCompile(`^【([^】]+)】`)

func quoteTitle(text string) string {
	text = strings.TrimSpace(text)
	m := reQuoteTitle.FindStringSubmatch(text)
	if len(m) >= 2 {
		return m[1]
	}
	if strings.HasPrefix(text, "【") {
		rest := strings.TrimPrefix(text, "【")
		if rest != "" {
			return rest
		}
	}
	return ""
}

// deliver 投递一条文本：硬换行 → 按 maxSendRunes 拆片 → 用当前上下文逐片发送；
// -14 时剩余分片入 pending，其他失败重试 2 次后计 undelivered。
// chatID/sessionID/title 是该文本归属的 agent 会话归因，发送成功且响应带回
// message_id 时记入映射（chatID 空则跳过）；同一原文的多分片共用归因。
func (b *Bridge) deliver(text, chatID, sessionID, title string) {
	// PC 端微信按 Markdown 渲染 ClawBot 气泡，单个 \n 被折叠成空格；
	// 统一升级为硬换行（行尾两空格），手机端纯文本渲染不受影响。先变换再拆片，
	// 保证每片发送长度仍 ≤ maxSendRunes。
	chunks := splitForWeixin(hardBreaks(text))
	b.mu.Lock()
	ctx := b.ctx
	b.mu.Unlock()
	if !ctx.valid {
		b.mu.Lock()
		b.undelivered++
		b.mu.Unlock()
		return
	}
	for i, chunk := range chunks {
		svrID, err := b.client.SendMessage(context.Background(), ctx.userID, ctx.contextToken, chunk)
		if errors.Is(err, ErrSessionExpired) {
			b.mu.Lock()
			for _, rest := range chunks[i:] {
				b.pending = append(b.pending, pendingMsg{
					userID: ctx.userID, contextToken: ctx.contextToken, text: rest,
					chatID: chatID, sessionID: sessionID, title: title,
				})
			}
			b.mu.Unlock()
			return
		}
		if err == nil {
			b.rememberSvrAttr(svrID, chatID, sessionID, title)
			continue
		}
		// 普通失败：重试 sendRetry 次。
		ok := false
		for j := 0; j < sendRetry; j++ {
			b.sleep(sendRetryWait)
			if svrID, err = b.client.SendMessage(context.Background(), ctx.userID, ctx.contextToken, chunk); err == nil {
				b.rememberSvrAttr(svrID, chatID, sessionID, title)
				ok = true
				break
			}
		}
		if !ok {
			b.mu.Lock()
			b.undelivered += len(chunks) - i
			b.mu.Unlock()
			return
		}
	}
}

// flushPending 依次补发积压消息：成功一条删一条；失败保留并停止本轮。
// 只在本轮实际成功补发 ≥1 条时清零 undelivered；pending 空时不动
// （普通发送失败的计数不得被一条无补发的入站消息误清）。
// 从头弹出，与并发的 pending 追加互不覆盖。
func (b *Bridge) flushPending() {
	sent := 0
	for {
		b.mu.Lock()
		if len(b.pending) == 0 {
			if sent > 0 {
				b.undelivered = 0
			}
			b.mu.Unlock()
			return
		}
		pm := b.pending[0]
		b.mu.Unlock()

		svrID, err := b.client.SendMessage(context.Background(), pm.userID, pm.contextToken, pm.text)
		if err != nil {
			return // 失败保留剩余 pending，等下次入站再补
		}
		b.rememberSvrAttr(svrID, pm.chatID, pm.sessionID, pm.title)
		sent++
		b.mu.Lock()
		b.pending = b.pending[1:]
		b.mu.Unlock()
	}
}

// splitForWeixin 按 maxSendRunes 把文本拆成若干片（按 rune，避免切坏中文）。
// 不超过上限时返回单元素切片；空串返回含一个空串的切片，由调用方决定是否发送。
func splitForWeixin(s string) []string {
	runes := []rune(s)
	if len(runes) <= maxSendRunes {
		return []string{s}
	}
	out := make([]string, 0, (len(runes)+maxSendRunes-1)/maxSendRunes)
	for len(runes) > 0 {
		n := maxSendRunes
		if n > len(runes) {
			n = len(runes)
		}
		out = append(out, string(runes[:n]))
		runes = runes[n:]
	}
	return out
}
