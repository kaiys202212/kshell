package desktop

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/hubtools"
	"github.com/yangk/kshell/internal/weixin"
)

// startWeixin 装配微信运行时（Startup 末尾调用）：读配置、注入回调，凭据齐全则启动长轮询。
// 幂等：重复调用只重建 Runtime 的回调装配，不重复 Start（Start 自身幂等）。
func (a *App) startWeixin() {
	cfg := a.snapshot().Config
	r := weixin.NewRuntime(newWXExecutor(a), normalizeLanguage(cfg.Language))
	r.SetPersist(func(p weixin.WeixinPersist) error {
		return a.saveConfig(func(c *config.Config) {
			c.Weixin = config.WeixinConfig{Enabled: p.Enabled, Token: p.Token, Wxid: p.Wxid}
		})
	})
	r.SetOnLearnWxid(func(wxid string) {
		_ = a.saveConfig(func(c *config.Config) {
			c.Weixin.Wxid = wxid
			c.Weixin.Enabled = true
		})
	})
	r.SetTranscriber(a.weixinTranscriber())
	r.Configure(cfg.Weixin.Token, cfg.Weixin.Wxid)

	a.mu.Lock()
	a.wx = r
	a.mu.Unlock()
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

// mapVoiceErr 把 hubtools「模型不支持音频」哨兵映射为 weixin.ErrVoiceUnsupported
//（%w 双层包裹，两层哨兵都在链上：桥层归因到 weixin 哨兵，日志仍可归因到
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
	defer a.mu.Unlock()
	if a.hubConsole == nil {
		// 已持 a.mu：直接读 opts，不能再走 snapshot()（会重入 a.mu 死锁）。
		a.hubConsole = hub.NewConsoleLang(newWXExecutor(a), normalizeLanguage(a.opts.Config.Language))
	}
	return a.hubConsole
}

// WeixinBind 发起扫码绑定，返回二维码内容 URL（前端转二维码图）。
func (a *App) WeixinBind() (string, error) {
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
}

// HubPickTarget 是 AgentHub 无精确目标时的待选入口：共享 Console 输出编号
// 会话列表并暂存正文；用户回数字由 HubCommand 完成激活与发送。
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

// clearHubPendingPick 让桌面端 AgentHub 的待选菜单失效（不惰性创建 Console）。
// 用户另行投递后，旧暂存正文不得再被后续纯数字选中发送。
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
}
