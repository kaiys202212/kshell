package desktop

import (
	"context"
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

// weixinTranscriber 返回语音转写函数：每次调用实时读模型平台配置（默认平台的
// ASR 模型），改配置即时生效。未配置（无默认平台/ASR 模型/OpenAI 端点）时返回
// weixin.ErrTranscriberNotConfigured 哨兵，桥据此回执「未配置」。
func (a *App) weixinTranscriber() weixin.Transcriber {
	return func(ctx context.Context, wav []byte) (string, error) {
		plat, ok := a.snapshot().Config.Model.ActivePlatform()
		if !ok || strings.TrimSpace(plat.ASRModel) == "" || strings.TrimSpace(plat.OpenAIBaseURL) == "" {
			return "", weixin.ErrTranscriberNotConfigured
		}
		return hubtools.TranscribeAudio(ctx,
			hubtools.Config{BaseURL: plat.OpenAIBaseURL, APIKey: plat.APIKey},
			plat.ASRModel, wav)
	}
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
