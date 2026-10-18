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
)

// sendRetry 是出站普通失败的额外重试次数（首次 + 2 = 共 3 次尝试）。
const sendRetry = 2

// sendRetryWait 是重试间隔。
const sendRetryWait = 500 * time.Millisecond

// maxSendRunes 是单条微信文本消息的最大长度（超出截断并提示去桌面端看全文）。
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
}

// ctxInfo 是最近一次白名单入站消息的发送凭据（出站镜像按它投递）。
type ctxInfo struct {
	userID       string
	contextToken string
	valid        bool
}

// ErrTranscriberNotConfigured 是语音转写未配置的哨兵：transcriber 实现方
//（desktop 装配）在模型平台缺 ASR 模型时返回它，桥据此回执「未配置」而非「失败」。
var ErrTranscriberNotConfigured = errors.New("err.weixin.voice_asr_unconfigured")

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

	// filesDir 是附件落盘目录；NewBridge 给默认值，测试注入临时目录。
	filesDir string
	// transcriber 是语音转写函数（desktop 注入）；nil = 未装配（纯语音回执提示）。
	transcriber Transcriber

	// outbound 是串行出站队列：DeliverHubMessage 只入队，单 worker 按序
	// deliver，避免 hub 消息因网络耗时不等而在微信侧乱序。
	outbound chan string
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
}

// outboundCap 是出站队列容量：满时新消息直接丢弃计 undelivered，
// 不阻塞 hub 写入路径。
const outboundCap = 128

// NewBridge 创建微信桥；内部持有独立的命令状态机。
func NewBridge(client *Client, ex hub.Executor, cfg BridgeConfig) *Bridge {
	return &Bridge{
		client:     client,
		ex:         ex,
		console:    hub.NewConsoleLang(ex, cfg.Language),
		wxid:       cfg.Wxid,
		lang:       cfg.Language,
		outbound:   make(chan string, outboundCap),
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

// transcriberFn 加锁读当前转写函数。
func (b *Bridge) transcriberFn() Transcriber {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.transcriber
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
	b.enqueueOutbound(flushSentinel)
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

	receipt := b.ingestAttachments(ctx, m.Attachments)

	text := m.Text
	// 纯语音（服务端无转写）：下载 → SILK→WAV → 模型转写；转写文本按
	// 可转发文字处理（参与附件队列合并）；失败/未配置转回执。
	if strings.TrimSpace(text) == "" && m.Voice != nil {
		if transcript, err := b.transcribeVoice(ctx, m.Voice); err != nil {
			if errors.Is(err, ErrTranscriberNotConfigured) {
				receipt = joinNotes(receipt, bridgeText(b.lang, "voice.unconfigured"))
			} else {
				receipt = joinNotes(receipt, fmt.Sprintf(bridgeText(b.lang, "voice.failed"), err))
			}
		} else if strings.TrimSpace(transcript) == "" {
			receipt = joinNotes(receipt, fmt.Sprintf(bridgeText(b.lang, "voice.failed"), "空转写结果"))
		} else {
			text = transcript
		}
	}

	var replies []hub.Reply
	merged := false
	trimmed := strings.TrimSpace(text)
	// 可转发文字（非命令/纯数字）且队列非空：合并投递。
	if isForwardableText(text) && b.QueuedFiles() > 0 {
		text = b.queuedPrefix() + text
		replies = b.console.Handle(text)
		if len(replies) == 0 {
			// 合并成功转发（agent 稍后回复）：清队列，附件信息已在投递文本里，
			// 不再单独发附件回执；微信侧补既有 sent_ack。
			b.clearQueue()
			merged = true
			replies = []hub.Reply{{Text: b.deliveredAck()}}
		}
		// 转发失败（无会话/发送错误等有回复场景）：队列保留，回执照发。
	} else if trimmed != "" {
		replies = b.console.Handle(text)
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
	// #st 末尾追加待转交附件数，防用户遗忘队列（N>0 才追加）。按空白取第一
	// 段与 "#st"/"＃st" 精确比较，避免 "#stop" 等未来命令误伤。
	if first := strings.Fields(trimmed); len(first) > 0 && (first[0] == "#st" || first[0] == "＃st") {
		if n := b.QueuedFiles(); n > 0 {
			replies = append(replies, hub.Reply{
				Text: fmt.Sprintf(bridgeText(b.lang, "file.queued_st"), n),
			})
		}
	}
	for _, r := range replies {
		b.deliver(r.Text)
	}
	// 回复已发出，取消输入状态。
	_ = b.client.SendTyping(context.Background(), m.FromUserID, m.ContextToken, 2)
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

// transcribeVoice 完成语音到文字的完整链路：CDN 下载 → SILK 解码转 WAV →
// 注入的 transcriber。未装配 transcriber 时直接返回哨兵（不下载）。
func (b *Bridge) transcribeVoice(ctx context.Context, v *InboundAttachment) (string, error) {
	fn := b.transcriberFn()
	if fn == nil {
		return "", ErrTranscriberNotConfigured
	}
	data, err := b.client.DownloadMedia(ctx, *v)
	if err != nil {
		return "", fmt.Errorf("下载语音: %w", err)
	}
	wav, err := silkToWAV(data)
	if err != nil {
		return "", err
	}
	return fn(ctx, wav)
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
	b.mu.Unlock()
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
//（微信侧由命令层欢迎词覆盖，避免重复问候）与 wire key 形态文本
//（后端稳定 key，非自然语言）一律跳过，防止刷屏。
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
	if m.SessionTitle != "" {
		text = "【" + m.SessionTitle + "】" + text
	}
	b.enqueueOutbound(text)
}

// enqueueOutbound 把一条文本放入串行出站队列；满或已关闭则丢弃计 undelivered。
// 首次入队时惰性启动出站 worker：生产路径只有这里会往队列投消息，
// worker 只在 FlushForTest（测试）与 Close（排空退出）被调是不够的——
// 否则队列永远无人消费，agent 回复全部滞留（2026-10-18 真机「微信收不到
// agent 回复、命令回复正常」的根因）。
func (b *Bridge) enqueueOutbound(text string) {
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
	case b.outbound <- text:
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
			for text := range b.outbound {
				if text == flushSentinel {
					if b.flushHook != nil {
						b.flushHook()
					}
					continue
				}
				b.deliver(text)
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

// deliver 投递一条文本：截断 → 用当前上下文发送；-14 入 pending，
// 其他失败重试 2 次后计 undelivered。
func (b *Bridge) deliver(text string) {
	text = truncateForWeixin(text, b.lang)
	// PC 端微信按 Markdown 渲染 ClawBot 气泡，单个 \n 被折叠成空格；
	// 统一升级为硬换行（行尾两空格），手机端纯文本渲染不受影响。
	text = hardBreaks(text)
	b.mu.Lock()
	ctx := b.ctx
	b.mu.Unlock()
	if !ctx.valid {
		b.mu.Lock()
		b.undelivered++
		b.mu.Unlock()
		return
	}
	err := b.client.SendMessage(context.Background(), ctx.userID, ctx.contextToken, text)
	if errors.Is(err, ErrSessionExpired) {
		b.mu.Lock()
		b.pending = append(b.pending, pendingMsg{userID: ctx.userID, contextToken: ctx.contextToken, text: text})
		b.mu.Unlock()
		return
	}
	if err == nil {
		return
	}
	// 普通失败：重试 sendRetry 次。
	for i := 0; i < sendRetry; i++ {
		b.sleep(sendRetryWait)
		if err = b.client.SendMessage(context.Background(), ctx.userID, ctx.contextToken, text); err == nil {
			return
		}
	}
	b.mu.Lock()
	b.undelivered++
	b.mu.Unlock()
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

		if err := b.client.SendMessage(context.Background(), pm.userID, pm.contextToken, pm.text); err != nil {
			return // 失败保留剩余 pending，等下次入站再补
		}
		sent++
		b.mu.Lock()
		b.pending = b.pending[1:]
		b.mu.Unlock()
	}
}

// truncateForWeixin 截断到 maxSendRunes 并追加「完整内容见桌面端」提示。
// 提示语较长时可能略微超出上限（≤ maxSendRunes + 10），不影响发送。
func truncateForWeixin(s, lang string) string {
	runes := []rune(s)
	if len(runes) <= maxSendRunes {
		return s
	}
	hint := bridgeText(lang, "msg.truncated")
	cut := maxSendRunes - len([]rune(hint))
	if cut < 0 {
		cut = 0
	}
	return string(runes[:cut]) + hint
}
