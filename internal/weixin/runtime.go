package weixin

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/yangk/kshell/internal/hub"
)

// bindPollInterval 是扫码绑定的轮询间隔。
const bindPollInterval = 2 * time.Second

// 初始长轮询失败退避与上限（指数退避：2s → 4s → … → 60s 封顶）。
const (
	pollBackoffInit = 2 * time.Second
	pollBackoffMax  = 60 * time.Second
)

// WeixinPersist 是凭据变更落盘的载荷（desktop 包装 saveConfig 实现）。
type WeixinPersist struct {
	Enabled bool
	Token   string
	Wxid    string
}

// WeixinStatus 是微信桥的运行状态快照。
type WeixinStatus struct {
	Bound       bool   // 已绑定（有 token）
	Running     bool   // 长轮询运行中
	Wxid        string // 白名单用户（可能为空：尚未收到首条消息）
	Pending     int    // -14 排队待发条数
	Undelivered int    // 发送失败累计条数
}

// Runtime 是微信桥的运行时封装：管理长轮询 goroutine 生命周期、绑定状态与
// 配置落盘回调。desktop 在 Startup 时创建并注入回调，此后只经绑定方法交互。
type Runtime struct {
	mu      sync.Mutex
	client  *Client
	bridge  *Bridge
	ex      hub.Executor
	cancel  context.CancelFunc // 轮询取消
	running bool

	// persist 由 desktop 注入：token/wxid 变更落盘（saveConfig 包装）。
	persist func(cfg WeixinPersist) error
	// onLearnWxid 由 desktop 注入：wxid 首次从入站消息学习时落盘。
	onLearnWxid func(wxid string)
	// transcriber 由 desktop 注入：语音转写（实时配置，装配到 bridge）。
	transcriber Transcriber
	// toolName 由 desktop 注入：工具展示名（出站标题栏归因，装配到 bridge）。
	toolName func(string) string
	// lang 文案语言。
	lang string

	// pollCount 累计启动过的轮询轮数（幂等性测试用）。
	pollCount int

	// bindStatus 是绑定 goroutine 的状态：idle/pending/ok/expired。
	bindStatus string
	// bindDone 关闭表示绑定 goroutine 已结束（防并发重复 Bind）。
	bindCancel context.CancelFunc

	// —— 测试注入点（零值即生产行为）——
	baseURL           string              // client 的 baseURL（测试指到 httptest）
	backoff           func(time.Duration) // 退避等待（测试 no-op）
	sleepBetweenPolls func()              // 绑定轮询间隔（测试 no-op）
}

// NewRuntime 创建微信运行时。lang 为桥文案语言（en / zh-CN）。
func NewRuntime(ex hub.Executor, lang string) *Runtime {
	return &Runtime{ex: ex, lang: lang}
}

// SetPersist 装配落盘回调（Start 前调用）。
func (r *Runtime) SetPersist(fn func(WeixinPersist) error) { r.persist = fn }

// SetOnLearnWxid 装配 wxid 学习回调（Configure 前调用；转交给 Bridge）。
func (r *Runtime) SetOnLearnWxid(fn func(wxid string)) { r.onLearnWxid = fn }

// SetTranscriber 装配语音转写函数：转发给已存在的 bridge 并记录，Configure
// 创建新 bridge（重绑）时再次注入。
func (r *Runtime) SetTranscriber(fn Transcriber) {
	r.mu.Lock()
	r.transcriber = fn
	bridge := r.bridge
	r.mu.Unlock()
	if bridge != nil {
		bridge.SetTranscriber(fn)
	}
}

// SetToolName 装配工具展示名查询函数（出站标题栏归因用）：转发给已存在的
// bridge 并记录，Configure 创建新 bridge（重绑）时再次注入。
func (r *Runtime) SetToolName(fn func(string) string) {
	r.mu.Lock()
	r.toolName = fn
	bridge := r.bridge
	r.mu.Unlock()
	if bridge != nil {
		bridge.SetToolName(fn)
	}
}

// SetBaseURL 覆盖 iLink 客户端的基础地址（测试注入 httptest 用；须在 Configure 前调用）。
func (r *Runtime) SetBaseURL(url string) { r.baseURL = url }

// GetBridge 返回当前桥（未 Configure 时为 nil）。
func (r *Runtime) GetBridge() *Bridge {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bridge
}

// HasBridge 报告桥是否已创建。
func (r *Runtime) HasBridge() bool { return r.GetBridge() != nil }

// Client 返回当前 iLink 客户端（未绑定/已解绑为 nil；测试与状态展示用）。
func (r *Runtime) Client() *Client {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.client
}

// Running 报告长轮询是否运行中。
func (r *Runtime) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// runningPolls 返回累计启动过的轮询轮数（幂等性测试用）。
func (r *Runtime) runningPolls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pollCount
}

// Configure 注入已保存的凭据（desktop Startup 时从 config 读出调用；绑定成功后
// 也调用）。token 非空时创建 Client 与 Bridge 并 Start；为空则只创建空 Runtime
// 状态（等用户扫码绑定）。
func (r *Runtime) Configure(token, wxid string) {
	r.mu.Lock()
	if token == "" {
		r.mu.Unlock()
		return
	}
	r.stopLocked()
	r.client = NewClient(token, r.baseURL)
	r.bridge = NewBridge(r.client, r.ex, BridgeConfig{Wxid: wxid, Language: r.lang})
	if r.transcriber != nil {
		r.bridge.SetTranscriber(r.transcriber)
	}
	if r.toolName != nil {
		r.bridge.SetToolName(r.toolName)
	}
	if r.onLearnWxid != nil {
		r.bridge.SetOnLearnWxid(r.onLearnWxid)
	}
	r.mu.Unlock()
	r.Start()
}

// Start 启动长轮询 goroutine：已运行或无凭据时 no-op（幂等）。
func (r *Runtime) Start() {
	r.mu.Lock()
	if r.running || r.client == nil {
		r.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.running = true
	r.pollCount++
	client := r.client
	bridge := r.bridge
	r.mu.Unlock()
	go r.pollLoop(ctx, client, bridge)
}

// backlogWindow 判定入站消息是否属于重启/重绑前的历史积压：消息创建时间早于
// 本进程启动时间一定余量即视为积压。2026-10-18 真机教训：曾按「cursor="" 首轮
// 无条件丢弃」实现，用户扫码后立即发的消息正好落在首轮窗口被吞（wxid 学不到、
// 无回复）。改为按消息时间判定：积压丢、新鲜投，不依赖轮次。
const backlogWindow = 2 * time.Minute

// startedAt 是包级进程启动时刻（Runtime 创建时取，重绑不重置——重绑窗口内
// 也不该把更早的陌生消息当新鲜处理；2 分钟余量覆盖绑定操作耗时）。
var startedAt = time.Now()

// isBacklog 报告消息是否为历史积压：服务端未给时间戳（CreateMs==0）时保守放行
//（宁可投给白名单用户也不能吞正常消息；错绑风险由白名单机制兜底）。
func isBacklog(m InboundMsg) bool {
	if m.CreateMs == 0 {
		return false
	}
	return time.UnixMilli(m.CreateMs).Before(startedAt.Add(-backlogWindow))
}

// pollLoop 长轮询主循环：游标游走取消息 → 白名单过滤（bridge）→ 命令层。
// ErrSessionExpired 时停止（用户需重扫码）；其他错误指数退避重试。
func (r *Runtime) pollLoop(ctx context.Context, client *Client, bridge *Bridge) {
	backoff := pollBackoffInit
	cursor := ""
	defer func() {
		r.mu.Lock()
		if r.cancel != nil {
			r.cancel = nil
		}
		r.running = false
		r.mu.Unlock()
	}()
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		msgs, next, err := client.GetUpdates(ctx, cursor)
		if errors.Is(err, ErrSessionExpired) {
			log.Printf("[weixin] 会话过期（errcode=-14），停止轮询，请重新扫码绑定")
			return
		}
		if err != nil {
			if ctx.Err() != nil {
				return // Stop 取消引发的网络错误：不算退避场景
			}
			log.Printf("[weixin] 长轮询失败，退避重试: %v", err)
			if wait(r, &backoff, ctx) {
				return
			}
			continue
		}
		backoff = pollBackoffInit // 成功重置退避
		cursor = next
		for _, m := range msgs {
			if isBacklog(m) {
				// 重启/重绑前的积压：只随游标推进自然丢弃，不投递。
				// 防陌生人消息错绑 wxid、旧文本被当命令执行。
				log.Printf("[weixin] 丢弃积压消息 from=%s create_ms=%d", m.FromUserID, m.CreateMs)
				continue
			}
			bridge.HandleInboundMsg(ctx, m)
		}
	}
}

// wait 退避等待：等待被取消返回 true（退出循环），否则 false（继续重试）。
func wait(r *Runtime, backoff *time.Duration, ctx context.Context) bool {
	d := *backoff
	fn := r.backoff
	if fn == nil {
		select {
		case <-ctx.Done():
			return true
		case <-time.After(d):
		}
	} else {
		fn(d) // 测试 no-op
	}
	*backoff *= 2
	if *backoff > pollBackoffMax {
		*backoff = pollBackoffMax
	}
	return false
}

// Bind 发起扫码绑定：临时 Client（无 token）取二维码；内部 goroutine 以
// bindPollInterval 轮询 PollQRCodeStatus，拿到 token 后经 persist 落盘并
// 转正为运行态（Configure）。总是允许重新绑定（-14 过期停轮询后恢复路径）：
// 已有凭据时先停旧轮询，扫码成功后凭据被覆盖。仅绑定流程进行中
// （bindStatus==pending）拒绝，防并发重复起绑定 goroutine。返回二维码内容 URL。
func (r *Runtime) Bind(ctx context.Context) (string, error) {
	r.mu.Lock()
	if r.bindStatus == "pending" {
		r.mu.Unlock()
		return "", errors.New("err.weixin.bind_in_progress")
	}
	// 已有凭据（正常绑定或 -14 过期）：停旧轮询，等待扫码结果覆盖。
	r.stopLocked()
	tmp := NewClient("", r.baseURL)
	r.bindStatus = "pending"
	bctx, bcancel := context.WithCancel(context.Background())
	r.bindCancel = bcancel
	r.mu.Unlock()

	qr, err := tmp.GetQRCode(ctx)
	if err != nil {
		r.mu.Lock()
		r.bindStatus = "expired"
		bcancel()
		r.mu.Unlock()
		return "", err
	}
	go r.bindLoop(bctx, tmp, qr.Key)
	return qr.ImgContent, nil
}

// bindLoop 轮询扫码状态直至拿到 token（→ ok + 转正）或出错（→ expired）。
func (r *Runtime) bindLoop(ctx context.Context, tmp *Client, key string) {
	defer func() {
		r.mu.Lock()
		r.bindCancel = nil
		r.mu.Unlock()
	}()
	sleep := r.sleepBetweenPolls
	for {
		st, err := tmp.PollQRCodeStatus(ctx, key)
		if err == nil && st.Scanned {
			r.mu.Lock()
			r.bindStatus = "ok"
			r.mu.Unlock()
			if r.persist != nil {
				_ = r.persist(WeixinPersist{Enabled: true, Token: st.BotToken, Wxid: ""})
			}
			r.Configure(st.BotToken, "")
			return
		}
		if err != nil || ctx.Err() != nil {
			r.mu.Lock()
			r.bindStatus = "expired"
			r.mu.Unlock()
			return
		}
		if sleep != nil {
			sleep()
		} else {
			select {
			case <-ctx.Done():
				r.mu.Lock()
				r.bindStatus = "expired"
				r.mu.Unlock()
				return
			case <-time.After(bindPollInterval):
			}
		}
	}
}

// BindStatus 返回绑定状态：idle / pending / ok / expired。
func (r *Runtime) BindStatus() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.bindStatus == "" {
		return "idle"
	}
	return r.bindStatus
}

// Unbind 解绑：停轮询与绑定轮询、persist 清空凭据、清 client/bridge。
func (r *Runtime) Unbind() {
	r.mu.Lock()
	r.stopLocked()
	if r.bindCancel != nil {
		r.bindCancel()
		r.bindCancel = nil
	}
	r.bindStatus = "idle"
	r.client = nil
	r.bridge = nil
	fn := r.persist
	r.mu.Unlock()
	if fn != nil {
		_ = fn(WeixinPersist{Enabled: false, Token: "", Wxid: ""})
	}
}

// Stop 停止长轮询（Shutdown 时调用；凭据保留，重启后 Configure 即可恢复）。
func (r *Runtime) Stop() {
	r.mu.Lock()
	r.stopLocked()
	r.mu.Unlock()
}

// stopLocked 停止轮询与出站 worker（调用方持锁）。
// bridge.Close 会等 worker 排空退出；持锁调用安全：worker 的 deliver 只
// 竞争 bridge 自身的 mu，不会回头取 Runtime.mu。
func (r *Runtime) stopLocked() {
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	r.running = false
	if r.bridge != nil {
		r.bridge.Close()
	}
}

// Status 汇总当前运行状态快照。
func (r *Runtime) Status() WeixinStatus {
	r.mu.Lock()
	st := WeixinStatus{
		Bound:   r.client != nil,
		Running: r.running,
	}
	var bridge *Bridge
	if r.bridge != nil {
		bridge = r.bridge
		st.Wxid = r.bridge.Wxid()
	}
	r.mu.Unlock()
	if bridge != nil {
		st.Pending = bridge.PendingCount()
		st.Undelivered = bridge.Undelivered()
	}
	return st
}

// OnHubMessage 把 hub 出站消息镜像到微信：同步调用（bridge 内部只入队即
// 返回，串行 worker 保证按序投递），不再为每条消息起 goroutine。
// bridge 未创建时安全跳过。
func (r *Runtime) OnHubMessage(m hub.Message) {
	r.mu.Lock()
	bridge := r.bridge
	r.mu.Unlock()
	if bridge == nil {
		return
	}
	bridge.DeliverHubMessage(m)
}

// Undelivered 返回累计发送失败条数（executor.Status 用）；无 bridge 时为 0。
func (r *Runtime) Undelivered() int {
	r.mu.Lock()
	bridge := r.bridge
	r.mu.Unlock()
	if bridge == nil {
		return 0
	}
	return bridge.Undelivered()
}
