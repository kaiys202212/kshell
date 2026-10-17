package weixin

import (
	"context"
	"errors"
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

	// onLearnWxid 非空时，wxid 首次从入站消息学习得到即回调（desktop 落盘）。
	// 由 SetOnLearnWxid 在 Start 前装配，此后视为只读。
	onLearnWxid func(wxid string)
	// onPermission 非空时，命令层进入权限等待即回调（desktop 弹窗）。
	onPermission func(chatID, reqID, desc string)

	// sleep 供测试注入 no-op，避免重试真实等待。
	sleep func(time.Duration)
}

// NewBridge 创建微信桥；内部持有独立的命令状态机。
func NewBridge(client *Client, ex hub.Executor, cfg BridgeConfig) *Bridge {
	return &Bridge{
		client:  client,
		ex:      ex,
		console: hub.NewConsole(ex),
		wxid:    cfg.Wxid,
		lang:    cfg.Language,
		sleep:   time.Sleep,
	}
}

// Undelivered 返回累计发送失败条数（#st 状态展示用）。
func (b *Bridge) Undelivered() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.undelivered
}

// PendingCount 返回因会话过期而排队等待补发的条数。
func (b *Bridge) PendingCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending)
}

// SetOnLearnWxid 装配 wxid 学习回调（Start 前调用，此后只读）。
func (b *Bridge) SetOnLearnWxid(fn func(wxid string)) { b.onLearnWxid = fn }

// SetOnPermission 装配权限请求转发回调：收到权限请求时通知调用方
// （desktop 把它转给前端弹窗与 hub 命令层的权限菜单）。
func (b *Bridge) SetOnPermission(fn func(chatID, reqID, desc string)) { b.onPermission = fn }

// Wxid 返回当前白名单用户（可能为学习前的空串；测试与状态展示用）。
func (b *Bridge) Wxid() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.wxid
}

// HandleInbound 处理一条入站消息：白名单过滤 → 记录发送上下文 →
// 补发 pending → 交命令层并把回复逐条投递。
func (b *Bridge) HandleInbound(fromUserID, contextToken, text string) {
	// wxid 待学习：iLink 扫码响应不带 wxid，首条白名单外的入站消息
	// 即视为机主本人（首扫即绑），学习一次后按白名单过滤。学习与
	// 白名单判断共用 mu，保证并发消息里只有一条能成为「首条」。
	b.mu.Lock()
	if b.wxid == "" {
		if fromUserID == "" {
			b.mu.Unlock()
			return
		}
		b.wxid = fromUserID
		fn := b.onLearnWxid
		b.mu.Unlock()
		if fn != nil {
			fn(fromUserID)
		}
	} else if fromUserID != b.wxid {
		b.mu.Unlock()
		return // 非白名单静默丢弃，不记录不回复
	} else {
		b.mu.Unlock()
	}
	b.mu.Lock()
	b.ctx = ctxInfo{userID: fromUserID, contextToken: contextToken, valid: true}
	b.mu.Unlock()

	// typing 只是体验优化，失败静默。
	_ = b.client.SendTyping(context.Background(), fromUserID, contextToken, 1)

	b.flushPending()

	for _, r := range b.console.Handle(text) {
		b.deliver(r.Text)
	}
	// 回复已发出，取消输入状态。
	_ = b.client.SendTyping(context.Background(), fromUserID, contextToken, 2)
}

// DeliverHubMessage 把 hub 的 agent 消息出站镜像到微信。
// 只镜像 agent 正文：用户消息、已撤回、ui.hub.* 事件与 wire key 形态文本
// （后端稳定 key，非自然语言）一律跳过，防止刷屏。
func (b *Bridge) DeliverHubMessage(m hub.Message) {
	if m.Kind != "agent" || m.Status == hub.StatusRecalled {
		return
	}
	if strings.HasPrefix(m.Event, "ui.hub.") || (m.Event == "" && isWireKey(m.Text)) {
		return
	}
	text := markdownToText(m.Text)
	if m.SessionTitle != "" {
		text = "[" + m.SessionTitle + "] " + text
	}
	b.deliver(text)
}

// isWireKey 报告文本是否为后端稳定 key 形态。
func isWireKey(s string) bool {
	return s != "" && !strings.ContainsAny(s, " \t\n") && len(s) < 64 && reWireKey.MatchString(s)
}

// deliver 投递一条文本：截断 → 用当前上下文发送；-14 入 pending，
// 其他失败重试 2 次后计 undelivered。
func (b *Bridge) deliver(text string) {
	text = truncateForWeixin(text, b.lang)
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
// 全部补发成功时清零 undelivered。从头弹出，与并发的 pending 追加互不覆盖。
func (b *Bridge) flushPending() {
	for {
		b.mu.Lock()
		if len(b.pending) == 0 {
			b.undelivered = 0
			b.mu.Unlock()
			return
		}
		pm := b.pending[0]
		b.mu.Unlock()

		if err := b.client.SendMessage(context.Background(), pm.userID, pm.contextToken, pm.text); err != nil {
			return // 失败保留剩余 pending，等下次入站再补
		}
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
