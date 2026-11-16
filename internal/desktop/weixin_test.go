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

	"github.com/yangk/kshell/internal/agentapi"
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
	app.opts.Config.Model.ActivePlatformID = "p1"
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
// 不写 hub 历史（原话可见性由前端本地渲染，权威落盘在发送链）、不投递。
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

	view, err := app.HubPickTarget("  随便看看  ")
	if err != nil {
		t.Fatalf("HubPickTarget: %v", err)
	}
	if !view.Pending {
		t.Fatal("有会话时应报告进入待选菜单（前端据此清草稿）")
	}
	if len(view.Replies) != 1 {
		t.Fatalf("应返回一条待选回复: %+v", view.Replies)
	}
	text := view.Replies[0].Text
	if !strings.Contains(text, "未精确匹配到会话") ||
		!strings.Contains(text, "1. 新建 session") ||
		!strings.Contains(text, "2. [a] claude 会话A") {
		t.Fatalf("待选回复应含分组菜单: %q", text)
	}
	// 不落盘：原话可见性由前端本地渲染，权威落盘在发送链。
	for _, msg := range store.List() {
		if msg.Kind == "user" && msg.Text == "随便看看" {
			t.Fatalf("HubPickTarget 不得写 hub 历史（前端本地渲染负责可见性）: %+v", msg)
		}
	}

	// 无会话：仍进菜单（仅新建入口，Pending=true），前端可清草稿。
	empty := wxApp(t, nil)
	empty.mu.Lock()
	empty.opts.Config.Language = "zh-CN"
	empty.mu.Unlock()
	view, err = empty.HubPickTarget("随便看看")
	if err != nil {
		t.Fatalf("HubPickTarget(empty): %v", err)
	}
	if !view.Pending {
		t.Fatal("无会话仅新建入口时应报告进入待选菜单")
	}
	if len(view.Replies) != 1 || !strings.Contains(view.Replies[0].Text, "1. 新建 session") {
		t.Fatalf("无会话应回仅新建入口待选: %+v", view.Replies)
	}
}

// injectWXRunner 注入微信管理者循环桩并返回调用计数指针。
func injectWXRunner(app *App, fn func(ctx context.Context, input string, opts agentapi.RunOpts) (agentapi.RunResult, error)) *int {
	calls := 0
	app.mu.Lock()
	app.wxRunner = func(ctx context.Context, input string, opts agentapi.RunOpts) (agentapi.RunResult, error) {
		calls++
		return fn(ctx, input, opts)
	}
	app.mu.Unlock()
	return &calls
}

// TestWeixinManagerRouter 覆盖微信管理者路由闭包：回显/挂起透传/降级/中途
// 进度推送。续轮衔接已改挂起-续跑语义（agentapi Run 内部按渠道+SuspendTTL
// 自动续跑，见 loop_test），desktop 不再维护续轮标记。bridge 侧挂钩（逃生门/
// 优先级/回显）由 weixin 包测试覆盖。
func TestWeixinManagerRouter(t *testing.T) {
	newApp := func(t *testing.T) *App {
		app := wxStatusApp(t)
		app.mu.Lock()
		app.opts.Config = config.Default()
		app.mu.Unlock()
		return app
	}

	t.Run("循环成功回显 Replies，挂起反问透传 AskPending", func(t *testing.T) {
		app := newApp(t)
		calls := injectWXRunner(app, func(_ context.Context, _ string, opts agentapi.RunOpts) (agentapi.RunResult, error) {
			if opts.Channel != "wechat" {
				t.Errorf("Channel = %q, 期望 wechat", opts.Channel)
			}
			return agentapi.RunResult{Replies: []string{"要发给哪个会话？\n1. 甲"}, Suspended: true}, nil
		})
		out, handled := app.weixinManagerRouter()(context.Background(), "帮我把任务分给甲")
		if !handled {
			t.Fatal("循环成功应接管")
		}
		if len(out.Replies) != 1 || out.Replies[0] != "要发给哪个会话？\n1. 甲" {
			t.Fatalf("Replies 未透传: %+v", out)
		}
		if !out.AskPending {
			t.Fatal("挂起反问应透传 AskPending（bridge 据此标记待答语境）")
		}
		if *calls != 1 {
			t.Fatalf("Run 调用次数 = %d, 期望 1", *calls)
		}
	})

	t.Run("ErrLLMNotReady 回落", func(t *testing.T) {
		app := newApp(t)
		injectWXRunner(app, func(context.Context, string, agentapi.RunOpts) (agentapi.RunResult, error) {
			return agentapi.RunResult{}, agentapi.ErrLLMNotReady
		})
		out, handled := app.weixinManagerRouter()(context.Background(), "随便看看")
		if handled {
			t.Fatal("ErrLLMNotReady 应回落 InputRouter")
		}
		if out.Replies != nil {
			t.Fatalf("回落应返回零值: %+v", out)
		}
	})

	t.Run("其它错误回落", func(t *testing.T) {
		app := newApp(t)
		injectWXRunner(app, func(context.Context, string, agentapi.RunOpts) (agentapi.RunResult, error) {
			return agentapi.RunResult{}, errors.New("boom")
		})
		if _, handled := app.weixinManagerRouter()(context.Background(), "随便看看"); handled {
			t.Fatal("其它错误应回落 InputRouter")
		}
	})

	t.Run("中途进度推送单轮一次", func(t *testing.T) {
		app := newApp(t)
		var pushes []int
		app.mu.Lock()
		app.wxNotify = func(step int) { pushes = append(pushes, step) }
		app.mu.Unlock()
		old := managerProgressInterval
		managerProgressInterval = 0 // 注入极短阈值：首个 Progress 即触发
		defer func() { managerProgressInterval = old }()
		injectWXRunner(app, func(_ context.Context, _ string, opts agentapi.RunOpts) (agentapi.RunResult, error) {
			if opts.Progress != nil {
				opts.Progress(agentapi.LoopProgress{Tool: "list_sessions", Round: 1})
				opts.Progress(agentapi.LoopProgress{Tool: "send", Round: 2})
			}
			return agentapi.RunResult{Replies: []string{"完成"}}, nil
		})
		mgr := app.weixinManagerRouter()
		mgr(context.Background(), "第一轮")
		if len(pushes) != 1 {
			t.Fatalf("单轮应最多推送一次, 实际 %d 次 (%v)", len(pushes), pushes)
		}
		if pushes[0] != 1 {
			t.Fatalf("推送轮次 = %d, 期望触发时刻的最新轮次 1", pushes[0])
		}
		// 下一轮：pushed 重置，仍只推一次。
		mgr(context.Background(), "第二轮")
		if len(pushes) != 2 {
			t.Fatalf("第二轮应再推一次, 累计 %d 次 (%v)", len(pushes), pushes)
		}
	})

	t.Run("总开关关闭不接管", func(t *testing.T) {
		app := newApp(t)
		off := false
		app.mu.Lock()
		app.opts.Config.AgentHub.Enabled = &off
		app.mu.Unlock()
		injectWXRunner(app, func(context.Context, string, agentapi.RunOpts) (agentapi.RunResult, error) {
			return agentapi.RunResult{Replies: []string{"不应到达"}}, nil
		})
		if _, handled := app.weixinManagerRouter()(context.Background(), "随便看看"); handled {
			t.Fatal("AgentHub 关闭时管理者路由不应接管")
		}
	})
}
