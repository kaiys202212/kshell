package desktop

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/hub"
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

// HubCommand("#l") 空会话回「暂无会话」。
func TestWeixinHubCommandListEmpty(t *testing.T) {
	app := wxStatusApp(t)
	replies, err := app.HubCommand("#l")
	if err != nil {
		t.Fatalf("HubCommand: %v", err)
	}
	if len(replies) != 1 || replies[0].Text != "暂无会话" {
		t.Fatalf("#l 空会话应回「暂无会话」: %+v", replies)
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

// 绑定状态机：未绑定时 BindStatus 为 idle；已绑定再 Bind 报错（不测真实网络）。
func TestWeixinBindGuards(t *testing.T) {
	app := wxStatusApp(t)
	if got := app.WeixinBindStatus(); got != "idle" {
		t.Fatalf("初始 BindStatus = %q", got)
	}
	r := weixin.NewRuntime(newWXExecutor(app), "zh-CN")
	r.SetPersist(func(weixin.WeixinPersist) error { return nil })
	r.Configure("tok", "u1")
	app.mu.Lock()
	app.wx = r
	app.mu.Unlock()
	if _, err := app.WeixinBind(); err == nil {
		t.Fatal("已绑定再 Bind 应报错")
	}
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
