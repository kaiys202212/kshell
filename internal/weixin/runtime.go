package weixin

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/hubtools"
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

// InputRouter 是微信入站文本的智能路由钩子：返回 (决策, 是否消费)。
// handled=false 时调用方回落 Console.Handle(原文) 兜底。闭包内自行读配置
// （照 transcriber 范式每次实时读，改配置即时生效）。
type InputRouter func(ctx context.Context, input string) (hubtools.RouteDecision, bool)

// ManagerOutcome 是管理者循环的执行结果（直接回显，不走决策执行）。
// Replies 已是渲染后的自然语言结论/反问文案；挂起-续跑语境由 agentapi
// 自持，桥侧不感知。
//
// FailureKey 是 handled=false 时携带的失败原因（稳定 wire key，由桥层按语言
// 渲染）：管理者循环"尝试过但失败"与环境问题（模型不可用）必须让用户看见，
// 而不是静默降级成"投给当前会话"。空串表示未尝试或非失败回落（如总开关关闭）。
type ManagerOutcome struct {
	Replies    []string
	FailureKey string
}

// ManagerRouter 是管理者循环路由钩子：返回 (结果, 是否接管)。优先级在
// DirectPatterns 逃生门之后、InputRouter（RouteInput 单发）之前：命中即把
// Replies 逐条推回微信（正文已是自然语言结论/选项列表）；未接管（false）
// 回落 InputRouter 路径。
type ManagerRouter func(ctx context.Context, input string) (ManagerOutcome, bool)

// RouteGate 是智能链路的启用门禁（desktop 注入，实时读 AgentHub 总开关）。
// false 表示"未启用智能路由"：自由文本按旧语义直投当前会话，不拦截、不
// 进待选。智能链路一旦启用（true），任何失败都必须转待选（不再猜目标）。
type RouteGate func() bool

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
	// router 由 desktop 注入：入站文本智能路由（实时读配置，装配到 bridge）。
	router InputRouter
	// managerRouter 由 desktop 注入：管理者循环路由（优先于 router，装配到 bridge）。
	managerRouter ManagerRouter
	// directPatternsFn 由 desktop 注入：逃生门白名单（实时读配置，装配到 bridge）。
	directPatternsFn func() []string
	// routeGate 由 desktop 注入：智能链路启用门禁（实时读总开关，装配到 bridge）。
	routeGate RouteGate
	// toolName 由 desktop 注入：工具展示名（出站标题栏归因，装配到 bridge）。
	toolName func(string) string
	// policy 由 desktop 注入：群发聚合策略（桌面 Console 与出站镜像共享同一
	// 实例，装配到 bridge console）。Configure 创建新 bridge（重绑）时再次注入。
	policy *hub.MirrorPolicy
	// lang 文案语言。
	lang string

	// pollCount 累计启动过的轮询轮数（幂等性测试用）。
	pollCount int

	// bindStatus 是绑定 goroutine 的状态：idle/pending/ok/expired。
	bindStatus string
	// bindDone 关闭表示绑定 goroutine 已结束（防并发重复 Bind）。
	bindCancel context.CancelFunc
	// bindGen 是绑定代次：Bind/Unbind 递增。bindLoop 的成功回调据此丢弃
	// 过期结果，避免解绑后扫码回调把 token 写回并复活绑定。
	bindGen uint64
	// persistMu 串行化「凭据落盘 + 绑定态校验」：bindLoop 成功回调与 Unbind
	// 走同一把锁，保证解绑总是最后生效（不会出现 token 回写晚于清空）。
	persistMu sync.Mutex

	// —— 测试注入点（零值即生产行为）——
	baseURL           string              // client 的 baseURL（测试指到 httptest）
	backoff           func(time.Duration) // 退避等待（测试 no-op）
	sleepBetweenPolls func()              // 绑定轮询间隔（测试 no-op）
	hubSink           func(hub.Message)   // 桌面集成测试:OnHubMessage 记录桩
}

// Lang 返回桥文案语言（en / zh-CN）：desktop 出站聚合摘要行组词用。
func (r *Runtime) Lang() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lang
}

// SetHubSink 供桌面侧集成测试替换 OnHubMessage 目的地（记录桩，避免搭
// httptest 全链路）。生产代码不得调用。
func (r *Runtime) SetHubSink(fn func(hub.Message)) {
	r.mu.Lock()
	r.hubSink = fn
	r.mu.Unlock()
}

// SetMirrorPolicy 注入群发聚合策略（desktop 与共享 Console 同一实例）：转发给
// 已存在的 bridge 并记录，Configure 创建新 bridge（重绑）时再次注入。
func (r *Runtime) SetMirrorPolicy(p *hub.MirrorPolicy) {
	r.mu.Lock()
	r.policy = p
	bridge := r.bridge
	r.mu.Unlock()
	if bridge != nil {
		bridge.console.SetMirrorPolicy(p)
	}
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

// SetRouter 装配入站文本智能路由钩子：转发给已存在的 bridge 并记录，
// Configure 创建新 bridge（重绑）时再次注入。
func (r *Runtime) SetRouter(fn InputRouter) {
	r.mu.Lock()
	r.router = fn
	bridge := r.bridge
	r.mu.Unlock()
	if bridge != nil {
		bridge.SetRouter(fn)
	}
}

// SetDirectPatterns 装配逃生门白名单查询函数：转发给已存在的 bridge 并记录，
// Configure 创建新 bridge（重绑）时再次注入。
func (r *Runtime) SetDirectPatterns(fn func() []string) {
	r.mu.Lock()
	r.directPatternsFn = fn
	bridge := r.bridge
	r.mu.Unlock()
	if bridge != nil {
		bridge.SetDirectPatterns(fn)
	}
}

// SetManagerRouter 装配管理者循环路由钩子：转发给已存在的 bridge 并记录，
// Configure 创建新 bridge（重绑）时再次注入。
func (r *Runtime) SetManagerRouter(fn ManagerRouter) {
	r.mu.Lock()
	r.managerRouter = fn
	bridge := r.bridge
	r.mu.Unlock()
	if bridge != nil {
		bridge.SetManagerRouter(fn)
	}
}

// SetRouteGate 装配智能链路启用门禁：转发给已存在的 bridge 并记录，
// Configure 创建新 bridge（重绑）时再次注入。
func (r *Runtime) SetRouteGate(fn RouteGate) {
	r.mu.Lock()
	r.routeGate = fn
	bridge := r.bridge
	r.mu.Unlock()
	if bridge != nil {
		bridge.SetRouteGate(fn)
	}
}

// NotifyProgress 向微信推送一条中途进度提示（管理者循环 15s 节流后调用）：
// 经既有出站串行 worker 异步投递（不阻塞循环，失败计 undelivered），凭据取
// 最近一次入站消息的发送上下文——进度推送发生在处理该入站期间，上下文即正确。
// bridge 未创建时 no-op。
func (r *Runtime) NotifyProgress(step int) {
	bridge := r.GetBridge()
	if bridge == nil {
		return
	}
	bridge.enqueueOutbound(outMsg{
		text: fmt.Sprintf(bridgeText(bridge.lang, "manager.progress"), step),
	})
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

// TryCancelPick 让微信桥命令层的待选（及带暂存的 menuNew）失效。
// bridge 未装配时 no-op。供桌面端 SendChatPrompt 成功后跨渠道清待选。
func (r *Runtime) TryCancelPick() {
	if b := r.GetBridge(); b != nil {
		b.TryCancelPick()
	}
}

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
	if r.router != nil {
		r.bridge.SetRouter(r.router)
	}
	if r.managerRouter != nil {
		r.bridge.SetManagerRouter(r.managerRouter)
	}
	if r.directPatternsFn != nil {
		r.bridge.SetDirectPatterns(r.directPatternsFn)
	}
	if r.routeGate != nil {
		r.bridge.SetRouteGate(r.routeGate)
	}
	if r.toolName != nil {
		r.bridge.SetToolName(r.toolName)
	}
	if r.policy != nil {
		r.bridge.console.SetMirrorPolicy(r.policy)
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
// （宁可投给白名单用户也不能吞正常消息；错绑风险由白名单机制兜底）。
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
	r.stopLocked() // 同时作废旧绑定代次并取消上一次绑定轮询
	tmp := NewClient("", r.baseURL)
	r.bindStatus = "pending"
	gen := r.bindGen
	bctx, bcancel := context.WithCancel(context.Background())
	r.bindCancel = bcancel
	r.mu.Unlock()

	qr, err := tmp.GetQRCode(ctx)
	if err != nil {
		r.mu.Lock()
		if gen == r.bindGen {
			r.bindStatus = "expired"
		}
		r.mu.Unlock()
		bcancel()
		return "", err
	}
	go r.bindLoop(bctx, tmp, qr.Key, gen)
	return qr.ImgContent, nil
}

// bindSucceeded 处理扫码成功：校验绑定代次后落盘并转正。
// 与 Unbind 共享 persistMu，保证解绑（清空 + 取消）不会被迟到的成功回调覆盖；
// 代次已过期（期间发生解绑/重绑）时直接丢弃。
func (r *Runtime) bindSucceeded(gen uint64, token string) {
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	r.mu.Lock()
	if gen != r.bindGen {
		r.mu.Unlock()
		return
	}
	fn := r.persist
	r.mu.Unlock()
	if fn != nil {
		if err := fn(WeixinPersist{Enabled: true, Token: token, Wxid: ""}); err != nil {
			// 写回被拒（如关闭态门禁）时中止：不把运行时重新配置成在线，
			// 否则配置虽为空、桥却仍在收发。
			log.Printf("[weixin] 绑定凭据落盘失败: %v", err)
			r.mu.Lock()
			if gen == r.bindGen {
				r.bindStatus = "idle"
			}
			r.mu.Unlock()
			return
		}
	}
	// 落盘期间可能有新的 Bind/Stop：代次变了就不要再把本实例配置成在线。
	r.mu.Lock()
	if gen != r.bindGen {
		r.mu.Unlock()
		return
	}
	r.bindStatus = "ok"
	r.mu.Unlock()
	r.Configure(token, "")
}

// bindLoop 轮询扫码状态直至拿到 token（→ ok + 转正）或出错（→ expired）。
func (r *Runtime) bindLoop(ctx context.Context, tmp *Client, key string, gen uint64) {
	defer func() {
		r.mu.Lock()
		if gen == r.bindGen {
			r.bindCancel = nil
		}
		r.mu.Unlock()
	}()
	sleep := r.sleepBetweenPolls
	for {
		st, err := tmp.PollQRCodeStatus(ctx, key)
		if err == nil && st.Scanned {
			r.bindSucceeded(gen, st.BotToken)
			return
		}
		if err != nil || ctx.Err() != nil {
			r.mu.Lock()
			if gen == r.bindGen {
				r.bindStatus = "expired"
			}
			r.mu.Unlock()
			return
		}
		if sleep != nil {
			sleep()
		} else {
			select {
			case <-ctx.Done():
				r.mu.Lock()
				if gen == r.bindGen {
					r.bindStatus = "expired"
				}
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
// bindGen 递增使迟到的扫码成功回调失效；persist 在 persistMu 内进行，
// 与 bindSucceeded 串行，保证解绑结果最终生效。
func (r *Runtime) Unbind() {
	// 先拿 persistMu 再动状态，与 bindSucceeded 的「persistMu → mu」同序：
	// 若成功回调已进入落盘+Configure，本函数会等它完成后再做停轮询/清状态，
	// 保证解绑是最后生效的一步（不会有内存里的桥复活）。
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	r.mu.Lock()
	r.stopLocked()
	r.bindStatus = "idle"
	r.client = nil
	r.bridge = nil
	fn := r.persist
	r.mu.Unlock()
	if fn != nil {
		if err := fn(WeixinPersist{Enabled: false, Token: "", Wxid: ""}); err != nil {
			log.Printf("[weixin] 解绑凭据落盘失败: %v", err)
		}
	}
}

// Stop 停止长轮询与进行中的扫码绑定（Shutdown / 桌面重建 Runtime 时调用；
// 凭据保留，重启后 Configure 即可恢复）。与 bindSucceeded 共享 persistMu，
// 避免「Stop 之后迟到的绑定回调又把本实例 Configure 成活」。
func (r *Runtime) Stop() {
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	r.mu.Lock()
	r.stopLocked()
	r.mu.Unlock()
}

// stopLocked 停止轮询与出站 worker（调用方持锁）。
// bridge.Close 会等 worker 排空退出；持锁调用安全：worker 的 deliver 只
// 竞争 bridge 自身的 mu，不会回头取 Runtime.mu。
func (r *Runtime) stopLocked() {
	// 作废进行中的扫码绑定：代次递增让迟到的成功回调失效，并取消轮询。
	// 必须在这里做：Configure/Bind/Stop 都经本函数，重建 Runtime 时（desktop
	// startWeixin 换实例）孤儿绑定不能继续活着。
	r.bindGen++
	if r.bindCancel != nil {
		r.bindCancel()
		r.bindCancel = nil
	}
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
	sink := r.hubSink
	bridge := r.bridge
	r.mu.Unlock()
	if sink != nil { // 测试桩:不进 bridge 出站队列
		sink(m)
		return
	}
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
