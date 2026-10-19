package desktop

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/hubtools"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/weixin"
)

// wxStatusApp 构造带最小依赖的 App（Windows/Scan 注入满足 ready()，无扫描数据）。
func wxStatusApp(t *testing.T) *App {
	t.Helper()
	return NewAppWith(Options{
		Home:    t.TempDir(),
		Windows: NewWindowManager(&stubLauncher{}, nil),
		Scan: func(string, []providers.Provider, string, discovery.ScanOptions) (*discovery.Result, error) {
			return &discovery.Result{}, nil
		},
		Emit: func(string, ...any) {},
	})
}

// HubCommand("#?") 返回含命令表的非空回复（wxExecutor 对 nil manager 安全）。
func TestWeixinHubCommandHelp(t *testing.T) {
	app := wxStatusApp(t)
	replies, err := app.HubCommand("#?")
	if err != nil {
		t.Fatalf("HubCommand: %v", err)
	}
	if len(replies) == 0 || replies[0].Text == "" {
		t.Fatalf("#? 应返回非空命令表: %+v", replies)
	}
	if !strings.Contains(replies[0].Text, "#l") {
		t.Errorf("命令表应含 #l: %q", replies[0].Text)
	}
}

// HubCommand("#l") 空会话回「暂无会话」（wxStatusApp 未设置语言，默认英文文案；
// 命令层文案随语言配置走，见 internal/hub/lang.go）。
func TestWeixinHubCommandListEmpty(t *testing.T) {
	app := wxStatusApp(t)
	replies, err := app.HubCommand("#l")
	if err != nil {
		t.Fatalf("HubCommand: %v", err)
	}
	if len(replies) != 1 || replies[0].Text != "No sessions yet" {
		t.Fatalf("#l 空会话应回 No sessions yet: %+v", replies)
	}
}

// recordHubMessage 出站钩子：agent 消息经 Runtime.OnHubMessage 异步镜像到
// Bridge 并真正 sendmessage（httptest 服务端作为 iLink client 的 baseURL）。
func TestWeixinRecordHubMessageMirror(t *testing.T) {
	var mu chan struct{} = make(chan struct{}, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ilink/bot/sendmessage" {
			mu <- struct{}{}
		}
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	t.Cleanup(srv.Close)

	app := wxStatusApp(t)
	r := weixin.NewRuntime(newWXExecutor(app), "zh-CN")
	r.SetPersist(func(weixin.WeixinPersist) error { return nil })
	r.SetBaseURL(srv.URL)
	r.Configure("tok", "u1")
	bridge := r.GetBridge()
	if bridge == nil {
		t.Fatal("Configure 后应有 bridge")
	}
	// 入站建立发送上下文（白名单 u1）。
	bridge.HandleInbound("u1", "ct-1", "hi")
	app.mu.Lock()
	app.wx = r
	app.mu.Unlock()

	// agent 消息出站镜像：异步 → 等服务端收到 sendmessage。
	app.recordHubMessage(hub.Message{Kind: "agent", Text: "hello bridge"})
	select {
	case <-mu: // 收到 sendmessage 即通过
	case <-time.After(2 * time.Second):
		t.Fatal("出站镜像未触发 sendmessage")
	}
}

// WeixinStatus 各字段映射（Bound/Running/Wxid/Pending/Undelivered/Binding）。
func TestWeixinStatusView(t *testing.T) {
	app := wxStatusApp(t)
	// 未装配：idle。
	st := app.WeixinStatus()
	if st.Bound || st.Running || st.Binding != "idle" {
		t.Fatalf("未装配 Status = %+v", st)
	}

	r := weixin.NewRuntime(newWXExecutor(app), "zh-CN")
	r.SetPersist(func(weixin.WeixinPersist) error { return nil })
	r.Configure("tok", "u-me")
	app.mu.Lock()
	app.wx = r
	app.mu.Unlock()

	st = app.WeixinStatus()
	if !st.Bound || !st.Running || st.Wxid != "u-me" || st.Binding != "idle" {
		t.Fatalf("Status = %+v", st)
	}

	// Unbind 后归零。
	if err := app.WeixinUnbind(); err != nil {
		t.Fatalf("WeixinUnbind: %v", err)
	}
	st = app.WeixinStatus()
	if st.Bound || st.Running || st.Wxid != "" {
		t.Fatalf("Unbind 后 Status = %+v", st)
	}
}

// 绑定状态机：未绑定时 BindStatus 为 idle；已绑定时 Bind 允许重扫（-14 过期
// 恢复路径）；绑定进行中再 Bind 报错（不测真实网络）。
func TestWeixinBindGuards(t *testing.T) {
	app := wxStatusApp(t)
	if got := app.WeixinBindStatus(); got != "idle" {
		t.Fatalf("初始 BindStatus = %q", got)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ilink/bot/get_bot_qrcode" {
			_, _ = w.Write([]byte(`{"code":0,"qrcode_img_content":"https://qr/x","qrcode":"key-1"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	t.Cleanup(srv.Close)
	r := weixin.NewRuntime(newWXExecutor(app), "zh-CN")
	r.SetPersist(func(weixin.WeixinPersist) error { return nil })
	r.SetBaseURL(srv.URL)
	r.Configure("tok", "u1")
	app.mu.Lock()
	app.wx = r
	app.mu.Unlock()
	// 已绑定允许重扫：返回二维码 URL，状态进入 pending。
	url, err := app.WeixinBind()
	if err != nil || url == "" {
		t.Fatalf("已绑定 WeixinBind 应允许重扫: url=%q err=%v", url, err)
	}
	// 绑定进行中拒绝。
	if _, err := app.WeixinBind(); err == nil {
		t.Fatal("绑定进行中再 Bind 应报错")
	}
	r.Stop()
}

// startWeixin 装配：配置有 token → Runtime 就绪并运行；配置为空 → Runtime 就绪但未运行。
func TestWeixinStartWeixinAssembly(t *testing.T) {
	app := wxStatusApp(t)
	cfg := config.Default()
	cfg.Weixin = config.WeixinConfig{Enabled: true, Token: "tok", Wxid: "u1"}
	app.mu.Lock()
	app.opts.Config = cfg
	app.mu.Unlock()

	app.startWeixin()
	r := app.wxRuntime()
	if r == nil {
		t.Fatal("startWeixin 应装配 Runtime")
	}
	st := app.WeixinStatus()
	if !st.Bound || !st.Running || st.Wxid != "u1" {
		t.Fatalf("有凭据 Status = %+v", st)
	}
	r.Stop()

	// 空配置：Runtime 存在但未启动（等扫码）。
	app2 := wxStatusApp(t)
	app2.mu.Lock()
	app2.opts.Config = config.Default()
	app2.mu.Unlock()
	app2.startWeixin()
	st2 := app2.WeixinStatus()
	if st2.Bound || st2.Running || st2.Binding != "idle" {
		t.Fatalf("空配置 Status = %+v", st2)
	}
}

// weixinTranscriber 映射：hubtools「模型不支持音频」哨兵应映射为
// weixin.ErrVoiceUnsupported（%w 包裹保留原 key 供日志归因），其余错误原样透传。
func TestWeixinTranscriberMapsAudioUnsupported(t *testing.T) {
	app := wxStatusApp(t)
	orig := app.snapshot().Config.Model
	app.mu.Lock()
	app.opts.Config.Model.Platforms = []config.ModelPlatform{{
		ID: "p1", Name: "p", OpenAIBaseURL: "http://unused", APIKey: "k", Models: []string{"m1"},
	}}
	app.opts.Config.Model.DefaultPlatform = "p1"
	app.mu.Unlock()
	defer func() {
		app.mu.Lock()
		app.opts.Config.Model = orig
		app.mu.Unlock()
	}()

	tr := app.weixinTranscriber()
	if tr == nil {
		t.Fatal("weixinTranscriber 应非 nil")
	}

	// 平台配置了专用 ASR 模型：转写路径由 hubtools 决定，这里不真正发请求，
	// 只验证映射逻辑 —— 用无网络 BaseURL 会以连接错误失败，属普通错误透传。
	_, err := tr(context.Background(), []byte("x"))
	if err != nil && errors.Is(err, weixin.ErrVoiceUnsupported) {
		t.Fatalf("连接错误不应被判为不支持音频: %v", err)
	}

	// 哨兵映射：直接构造 hubtools 哨兵经映射函数（与闭包内同款逻辑）。
	wrapped := fmt.Errorf("%w: 状态码 400", hubtools.ErrChatAudioUnsupported)
	mapped := mapVoiceErr(wrapped)
	if !errors.Is(mapped, weixin.ErrVoiceUnsupported) {
		t.Fatalf("hubtools 哨兵应映射为 weixin.ErrVoiceUnsupported: %v", mapped)
	}
	if !errors.Is(mapped, hubtools.ErrChatAudioUnsupported) {
		t.Fatalf("映射应保留原哨兵链供日志: %v", mapped)
	}
	other := context.DeadlineExceeded
	if mapVoiceErr(other) != other {
		t.Fatalf("普通错误应原样透传: %v", mapVoiceErr(other))
	}
}

// HubPickTarget 是 AgentHub 无精确目标时的待选入口：返回编号列表、暂存正文，
// 不写 hub 历史、不投递。
func TestWeixinHubPickTarget(t *testing.T) {
	app := wxApp(t, []providers.Session{
		{ID: "s1", ToolID: "claude", Workspace: `D:\ws\a`, Title: "会话A"},
	})
	app.mu.Lock()
	app.opts.Config.Language = "zh-CN"
	app.mu.Unlock()
	store := app.snapshot().HubStore
	if store == nil {
		t.Fatal("wxApp 未装配 HubStore")
	}
	before := len(store.List())

	replies, err := app.HubPickTarget("  随便看看  ")
	if err != nil {
		t.Fatalf("HubPickTarget: %v", err)
	}
	if len(replies) != 1 {
		t.Fatalf("应返回一条待选回复: %+v", replies)
	}
	text := replies[0].Text
	if !strings.Contains(text, "未精确匹配到会话") || !strings.Contains(text, "1. [a] claude 会话A") {
		t.Fatalf("待选回复应含提示与编号列表: %q", text)
	}
	if got := len(store.List()); got != before {
		t.Fatalf("HubPickTarget 不得写 hub 历史: before=%d after=%d", before, got)
	}
}
