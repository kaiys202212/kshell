package desktop

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/agentapi"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/hubtools"
	"github.com/yangk/kshell/internal/weixin"
)

// managerProgressInterval 是微信管理者循环的中途进度推送阈值：循环开始后
// 超过该时长且尚未推送过时，借 Progress 回调推一条「仍在处理」；单轮最多
// 一次。包级变量便于测试注入极短值。
var managerProgressInterval = 15 * time.Second

// startWeixin 装配微信运行时（Startup 末尾调用；applyAgentHubGates 在总开关
// 翻转/保存设置时也会重调）：读配置、注入回调，凭据齐全则启动长轮询。
// 每次调用都重建一个新 Runtime 替换 a.wx；替换前先 Stop 旧实例——长轮询 goroutine
// 挂在旧 Runtime 上，不 Stop 的话关闭总开关后旧实例仍会以真实 token 继续收发，
// 与「关闭即停轮询」意图相悖。总开关关闭时凭据一并清空（解绑），重新开启需重新扫码。
func (a *App) startWeixin() {
	cfg := a.snapshot().Config
	r := weixin.NewRuntime(newWXExecutor(a), normalizeLanguage(cfg.Language))
	r.SetPersist(func(p weixin.WeixinPersist) error {
		// 关闭态拒绝写回 token：扫码成功回调可能晚于解绑，不能把绑定复活。
		if p.Token != "" && !a.snapshot().Config.AgentHub.EnabledOn() {
			return errors.New("err.weixin.agenthub_disabled")
		}
		return a.saveConfig(func(c *config.Config) {
			c.Weixin = config.WeixinConfig{Enabled: p.Enabled, Token: p.Token, Wxid: p.Wxid}
		})
	})
	r.SetOnLearnWxid(func(wxid string) {
		if !a.snapshot().Config.AgentHub.EnabledOn() {
			return
		}
		_ = a.saveConfig(func(c *config.Config) {
			c.Weixin.Wxid = wxid
			c.Weixin.Enabled = true
		})
	})
	r.SetTranscriber(a.weixinTranscriber())
	// 智能路由与逃生门白名单：闭包每次实时读配置（照 transcriber 范式），
	// 改配置即时生效；Configure 重绑时 Runtime 会再次注入到新 bridge。
	r.SetRouter(a.weixinInputRouter())
	r.SetManagerRouter(a.weixinManagerRouter())
	// 智能链路门禁：总开关关闭时微信侧不进智能路由（按旧语义直投当前会话），
	// 与桌面端 AgentHub 的「未启用即不智能分发」口径一致。
	r.SetRouteGate(func() bool { return a.snapshot().Config.AgentHub.EnabledOn() })
	r.SetDirectPatterns(func() []string { return a.agenthubDirectPatterns() })
	r.SetToolName(a.toolDisplayName)
	// 共享聚合策略:桌面 Console(wxHubConsole)与微信 bridge Console 与出站
	// 镜像(mirrorAggregated)指到同一实例;Configure 创建 bridge 时会再注入。
	r.SetMirrorPolicy(a.mirrorHubPolicy())
	// 总开关关闭：不喂凭据（轮询不启动，绑定入口已被 WeixinBind 拒绝）；开启即恢复正常。
	if cfg.AgentHub.EnabledOn() {
		r.Configure(cfg.Weixin.Token, cfg.Weixin.Wxid)
	} else {
		r.Configure("", "")
		// 旧版本可能留下 token：关闭态下清空，避免重新开启时意外复活旧绑定。
		if cfg.Weixin.Token != "" || cfg.Weixin.Wxid != "" || cfg.Weixin.Enabled {
			if err := a.saveConfig(func(c *config.Config) {
				c.Weixin = config.WeixinConfig{}
			}); err != nil {
				log.Printf("[weixin] 清理关闭态遗留凭据失败: %v", err)
			}
		}
	}

	a.mu.Lock()
	old := a.wx
	a.wx = r
	a.mu.Unlock()
	if old != nil {
		old.Stop() // 开关翻转重建：停掉旧长轮询，防止关闭总开关后旧实例继续收发
	}
}

// weixinTranscriber 返回语音转写函数：每次调用实时读模型平台配置，改配置即时
// 生效。转写来源优先级链（2026-10-18 与用户确认）：默认平台 asr_model 已填走
// 专用 ASR 端点；留空则用默认对话模型（ActiveModel，AgentHub 指令环节同款
// chat 端点）经 input_audio 直接听音频转写——要求该模型支持音频输入，不支持
// 时由转写失败回执兜底。两者皆缺返回 weixin.ErrTranscriberNotConfigured 哨兵。
func (a *App) weixinTranscriber() weixin.Transcriber {
	return func(ctx context.Context, wav []byte) (string, error) {
		cfgSnapshot := a.snapshot().Config.Model
		plat, ok := cfgSnapshot.ActivePlatform()
		if !ok || strings.TrimSpace(plat.OpenAIBaseURL) == "" {
			return "", weixin.ErrTranscriberNotConfigured
		}
		cfg := hubtools.Config{BaseURL: plat.OpenAIBaseURL, APIKey: plat.APIKey}
		chatModel := strings.TrimSpace(cfgSnapshot.ActiveModel)
		// 对话模型三元组不齐（对齐 hubtools.Ready 口径：免费无 key 端点不尝试）；
		// BaseURL 已在上一步查过，这里只查 Model 与 APIKey。
		if strings.TrimSpace(plat.ASRModel) == "" && (chatModel == "" || strings.TrimSpace(plat.APIKey) == "") {
			// ASR 模型未填且对话模型三元组不齐：无可用的转写来源。
			return "", weixin.ErrTranscriberNotConfigured
		}
		text, err := hubtools.TranscribeAuto(ctx, cfg, plat.ASRModel, chatModel, wav)
		if err != nil {
			return "", mapVoiceErr(err)
		}
		return text, nil
	}
}

// weixinInputRouter 返回微信入站智能路由闭包：每次调用实时读配置，总开关关闭
// 或默认模型链未就绪时返回 (zero, false)，入站自由文本回落命令层原文投递。
// 会话快照编号与 bridge 侧 executeRouteText 同源（均为 ListSessions 索引+1），
// 保证决策里的编号在两侧指向同一会话。
func (a *App) weixinInputRouter() weixin.InputRouter {
	return func(ctx context.Context, input string) (hubtools.RouteDecision, bool) {
		cfg := a.snapshot().Config
		if !cfg.AgentHub.EnabledOn() {
			return hubtools.RouteDecision{}, false
		}
		hc, ok := hubtools.FromModelConfig(cfg.Model)
		if !ok {
			return hubtools.RouteDecision{}, false
		}
		ex := newWXExecutor(a)
		sessions := routeBriefs(ex.ListSessions(), ex.SessionStatuses())
		d, err := hubtools.RouteInput(ctx, hc, sessions, input)
		if err != nil {
			return hubtools.RouteDecision{}, false
		}
		return d, true
	}
}

// weixinManagerRouter 返回微信管理者循环路由闭包（逃生门之后、InputRouter
// 单发之前）：循环成功 → Replies 直接回显（正文已是自然语言结论/选项列表）；
// ErrLoopBusy/ErrLLMNotReady/其它错误一律 (zero,false) 回落 InputRouter
// （RouteInput 单发路径），但带失败原因 FailureKey——桥层据此把降级转成
// 待选 + 可读回执，不再静默直投当前会话（2026-11-18 真机事故）。
// Channel 固定 "wechat"，与桌面 Hub 的挂起态渠道隔离；反问采用挂起-续跑语义
// （agentapi Run 内部按渠道+SuspendTTL 对短输入自动续跑），desktop 不再维护
// 续轮标记与 TTL。
func (a *App) weixinManagerRouter() weixin.ManagerRouter {
	return func(ctx context.Context, input string) (weixin.ManagerOutcome, bool) {
		if !a.snapshot().Config.AgentHub.EnabledOn() {
			return weixin.ManagerOutcome{}, false
		}
		start := time.Now()
		progressed, pushed := 0, false
		res, err := a.runWXLoop(ctx, input, agentapi.RunOpts{
			Channel: "wechat",
			Progress: func(p agentapi.LoopProgress) {
				progressed = p.Round
				// 中途进度推送（单轮最多一次）：循环开始计时，超过阈值且尚未
				// 推送过才推一条「仍在处理」，经 bridge 出站 worker 异步投递。
				// 传的是循环轮次（LoopProgress 无累计工具步数字段，文案即
				// 「第 N 轮」语义）。
				if !pushed && time.Since(start) >= managerProgressInterval {
					pushed = true
					a.pushWXManagerProgress(progressed)
				}
			},
		})
		if err != nil {
			// 挂起态归 agentapi 所有：瞬时错误（如 ErrLLMNotReady）保留挂起，
			// 用户重试会自动续跑；desktop 无需也不得清理。
			// 失败原因带上 FailureKey：桥层据此把降级转成待选 + 可读回执，
			// 而不是静默直投当前会话（2026-11-18 真机事故）。
			return weixin.ManagerOutcome{FailureKey: wxManagerFailureKey(err)}, false
		}
		// 挂起反问（Suspended ⇔ Ask 非 nil）时 Replies 已是「问题+编号选项」
		// 文本，直接透传给 bridge 回显；挂起-续跑语境由 agentapi 自持
		//（同渠道短输入 Run 内部自动续跑），桥侧零特殊逻辑。
		return weixin.ManagerOutcome{Replies: res.Replies}, true
	}
}

// wxManagerFailureKey 把管理者循环错误分类成用户可读的稳定 key（桥层按语言
// 渲染）：模型未配置属用户可自助修复的环境问题，与网关/网络/解析失败分开，
// 让微信回执能给出「去设置换模型」这种可操作指引。
func wxManagerFailureKey(err error) string {
	if errors.Is(err, agentapi.ErrLLMNotReady) {
		return "err.router.llm_not_ready"
	}
	return "err.router.loop_failed"
}

// runWXLoop 取微信管理者循环闭包并执行一次：注入桩优先（测试），nil 时用
// AgentAPI().Run（LLM 配置每次访问实时刷新，与桌面 Hub 同口径）。
func (a *App) runWXLoop(ctx context.Context, input string, opts agentapi.RunOpts) (agentapi.RunResult, error) {
	a.mu.Lock()
	fn := a.wxRunner
	a.mu.Unlock()
	if fn != nil {
		return fn(ctx, input, opts)
	}
	return a.AgentAPI().Run(ctx, input, opts)
}

// pushWXManagerProgress 推送一条微信侧中途进度：wxNotify 注入点（测试观察）
// 优先，生产走 Runtime.NotifyProgress 经 bridge 既有出站 worker 异步投递
// （Runtime/bridge 未装配时安全跳过）。
func (a *App) pushWXManagerProgress(step int) {
	a.mu.Lock()
	fn := a.wxNotify
	a.mu.Unlock()
	if fn != nil {
		fn(step)
		return
	}
	if r := a.wxRuntime(); r != nil {
		r.NotifyProgress(step)
	}
}

// agenthubDirectPatterns 返回逃生门白名单：实时读配置，空/nil 时回落 hub
// 内置默认集（# 命令/@ 指名/continue）。
func (a *App) agenthubDirectPatterns() []string {
	pats := a.snapshot().Config.AgentHub.DirectPatterns
	if len(pats) == 0 {
		return hub.DefaultDirectPatterns()
	}
	return append([]string(nil), pats...)
}

// mapVoiceErr 把 hubtools「模型不支持音频」哨兵映射为 weixin.ErrVoiceUnsupported
// （%w 双层包裹，两层哨兵都在链上：桥层归因到 weixin 哨兵，日志仍可归因到
// hubtools 原始 key）。其余错误原样透传。
func mapVoiceErr(err error) error {
	if err != nil && errors.Is(err, hubtools.ErrChatAudioUnsupported) {
		return fmt.Errorf("%w: %w", weixin.ErrVoiceUnsupported, err)
	}
	return err
}

// wxRuntime 取当前微信运行时（未装配为 nil）。
func (a *App) wxRuntime() *weixin.Runtime {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.wx
}

// wxHubConsole 惰性创建桌面端 AgentHub 共用的命令层 Console（首次 HubCommand 调用装配）。
// 文案语言取当前配置语言（en / zh-CN）。
func (a *App) wxHubConsole() *hub.Console {
	a.mu.Lock()
	if a.hubConsole == nil {
		// 已持 a.mu：直接读 opts，不能再走 snapshot()（会重入 a.mu 死锁）。
		a.hubConsole = hub.NewConsoleLang(newWXExecutor(a), normalizeLanguage(a.opts.Config.Language))
		// 共享聚合策略：已持锁，直接判 nil 初始化（不能调 mirrorHubPolicy，
		// 重入 a.mu 死锁）。微信 bridge Console 同一实例（startWeixin 装配）。
		if a.mirrorPolicy == nil {
			a.mirrorPolicy = hub.NewMirrorPolicy()
		}
		policy := a.mirrorPolicy
		a.mu.Unlock()
		// SetMirrorPolicy 取 Console 内部锁（c.mu），必须移出 a.mu 之外调用：
		// 反向 Console.Handle 持 c.mu 后经 wxExecutor 同步路径取 a.mu，若这里
		// 持 a.mu 调 c.mu 会形成 a.mu→c.mu 与 c.mu→a.mu 两条边并存，理论上
		// ABBA 死锁。仅首次创建时调用一次，与原行为一致。
		a.hubConsole.SetMirrorPolicy(policy)
		return a.hubConsole
	}
	c := a.hubConsole
	a.mu.Unlock()
	return c
}

// WeixinBind 发起扫码绑定，返回二维码内容 URL（前端转二维码图）。
func (a *App) WeixinBind() (string, error) {
	// AgentHub 总开关关闭：绑定入口一并拒绝（轮询已在 startWeixin 停喂凭据）。
	if !a.snapshot().Config.AgentHub.EnabledOn() {
		return "", errors.New("err.agenthub.disabled|WeixinBind")
	}
	r := a.wxRuntime()
	if r == nil {
		return "", errNotReady
	}
	return r.Bind(context.Background())
}

// WeixinBindStatus 返回绑定状态：idle / pending / ok / expired。
func (a *App) WeixinBindStatus() string {
	r := a.wxRuntime()
	if r == nil {
		return "idle"
	}
	return r.BindStatus()
}

// WeixinUnbind 解绑并清空凭据。
func (a *App) WeixinUnbind() error {
	r := a.wxRuntime()
	if r == nil {
		return errNotReady
	}
	r.Unbind()
	return nil
}

// WeixinStatusView 是微信桥状态的绑定形态。
type WeixinStatusView struct {
	Bound       bool   `json:"bound"`
	Running     bool   `json:"running"`
	Wxid        string `json:"wxid"`
	Pending     int    `json:"pending"`
	Undelivered int    `json:"undelivered"`
	Binding     string `json:"binding"`
}

// WeixinStatus 返回微信桥状态快照。
func (a *App) WeixinStatus() WeixinStatusView {
	r := a.wxRuntime()
	if r == nil {
		return WeixinStatusView{Binding: "idle"}
	}
	st := r.Status()
	return WeixinStatusView{
		Bound:       st.Bound,
		Running:     st.Running,
		Wxid:        st.Wxid,
		Pending:     st.Pending,
		Undelivered: st.Undelivered,
		Binding:     r.BindStatus(),
	}
}

// HubCommand 是桌面端 AgentHub 共用的命令层入口：前端识别到 # 命令时调用，
// 普通文本不走这里（避免与 SendChatPrompt 双投）。返回命令层的回复文本列表。
func (a *App) HubCommand(text string) ([]hub.Reply, error) {
	if !a.snapshot().ready() {
		return nil, errNotReady
	}
	return a.wxHubConsole().Handle(text), nil
}

// HubPickView 是 HubPickTarget 的返回：Replies 按序回显；Pending 为真表示
// 已建立待选菜单（正文已暂存），前端可清空草稿；false 时（无会话/其他菜单
// 进行中）应保留草稿，避免正文丢失。
type HubPickView struct {
	Replies []hub.Reply
	Pending bool
	// Cancelled 表示本轮智能路由被用户中断：前端既不清草稿也不回落待选菜单。
	Cancelled bool
}

// HubPickTarget 是 AgentHub 无精确目标时的待选入口：共享 Console 输出分组
// 菜单并暂存正文；用户回数字由 HubCommand 完成激活与发送。用户原话的即时
// 可见性由前端本地渲染（瞬态，不落盘）——权威落盘仍由发送链 sendAndRecord
// 完成，避免同文重复入库。
func (a *App) HubPickTarget(text string) (HubPickView, error) {
	if !a.snapshot().ready() {
		return HubPickView{}, errNotReady
	}
	replies, pending := a.wxHubConsole().PickTarget(text)
	if replies == nil {
		replies = []hub.Reply{} // 统一成空数组，前端可直接遍历
	}
	return HubPickView{Replies: replies, Pending: pending}, nil
}

// clearHubPendingPick 让待选菜单失效（桌面 AgentHub + 微信桥两套 Console）。
// 任一侧成功投递后两侧旧暂存都不得再被数字误发。不惰性创建 hubConsole。
func (a *App) clearHubPendingPick() {
	// 先取指针再释放 a.mu：Console 执行后端会反向取 a.mu，避免锁序死锁。
	a.mu.Lock()
	c := a.hubConsole
	a.mu.Unlock()
	if c != nil {
		// TryLock 而非阻塞 Lock：Console.Handle 全程持 c.mu 回调 Executor
		// 发送，同步重入即死锁；此时 Handle 的 menu 必非 menuPick（见
		// TryCancelPick 注释），放弃取消无损语义。
		c.TryCancelPick()
	}
	// 微信桥独立 Console：同样 TryCancelPick，与桌面侧对称清理。
	if r := a.wxRuntime(); r != nil {
		r.TryCancelPick()
	}
}
