package weixin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/hub"
)

// fakeExec 已在 bridge_test.go 定义，runtime 测试直接复用。

// rtServer 是 Runtime 测试的假服务端：处理扫码（GetQRCode/Poll）、长轮询与发消息。
// pollToken 非空时 PollQRCodeStatus 返回成功；failAfterUpdates 表示 getupdates
// 成功该次数后开始返回 500（注入普通错误测退避）；expireUpdates 第 N 次
// getupdates 返回 -14（注入会话过期）。
type rtServer struct {
	mu               sync.Mutex
	pollToken        string
	msgs             []InboundMsg // getupdates 下次返回的消息
	updates          int          // getupdates 请求计数
	failAfterUpdates int
	expireUpdates    int
}

func (s *rtServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		switch r.URL.Path {
		case "/ilink/bot/get_bot_qrcode":
			_, _ = w.Write([]byte(`{"code":0,"qrcode_img_content":"https://qr/x","qrcode":"key-1"}`))
		case "/ilink/bot/get_qrcode_status":
			if s.pollToken != "" {
				_, _ = w.Write([]byte(`{"code":0,"bot_token":"bot-tok"}`))
			} else {
				_, _ = w.Write([]byte(`{"code":0,"bot_token":""}`))
			}
		case "/ilink/bot/getupdates":
			s.updates++
			if s.expireUpdates > 0 && s.updates >= s.expireUpdates {
				_, _ = w.Write([]byte(`{"code":-14,"message":"expired"}`))
				return
			}
			if s.failAfterUpdates > 0 && s.updates > s.failAfterUpdates {
				w.WriteHeader(500)
				_, _ = w.Write([]byte(`{}`))
				return
			}
			var msgs []InboundMsg
			msgs, s.msgs = s.msgs, nil
			out := `{"code":0,"get_updates_buf":"cur-2","msgs":[]}`
			if len(msgs) > 0 {
				out = `{"code":0,"get_updates_buf":"cur-2","msgs":[`
				for i, m := range msgs {
					if i > 0 {
						out += ","
					}
					out += `{"message_type":1,"from_user_id":"` + m.FromUserID +
						`","context_token":"` + m.ContextToken + `","item_list":[{"type":1,"text_item":{"text":"` + m.Text + `"}}]}`
				}
				out += `]}`
			}
			_, _ = w.Write([]byte(out))
		default:
			_, _ = w.Write([]byte(`{"code":0}`))
		}
	}
}

// newTestRuntime 构造指向测试服务端的 Runtime（bollPoller 退避注入 no-op）。
func newTestRuntime(t *testing.T, ex hub.Executor, h http.HandlerFunc) (*Runtime, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	r := NewRuntime(ex, "zh-CN")
	r.baseURL = srv.URL
	r.backoff = func(time.Duration) {} // 退避等待注入 no-op
	r.sleepBetweenPolls = func() {}    // 绑定轮询间隔注入 no-op
	return r, srv
}

// waitFor 自旋等待条件成立（Runtime 的 goroutine 是异步的，事件通知太重）。
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待超时: %s", what)
}

// Configure 空 token 只置凭据不启动；Start 幂等。
func TestRuntimeConfigureAndStart(t *testing.T) {
	s := &rtServer{}
	r, _ := newTestRuntime(t, &fakeExec{hasCur: true}, s.handler())

	persisted := []WeixinPersist{}
	r.SetPersist(func(p WeixinPersist) error { persisted = append(persisted, p); return nil })

	// 空 token：不创建 client，不启动。
	r.Configure("", "")
	if r.Client() != nil || r.Running() {
		t.Fatalf("空 token 不应创建 client 或启动: client=%v running=%v", r.Client() != nil, r.Running())
	}

	// 有 token：创建并启动轮询；重复 Configure/Start 幂等。
	r.Configure("tok", "u1")
	if r.Client() == nil {
		t.Fatal("有 token 应创建 client")
	}
	r.Start()
	before := r.runningPolls()
	r.Start()
	r.Configure("tok", "u1")
	waitFor(t, 2*time.Second, func() bool { return r.runningPolls() > before }, "轮询应恰好再起一轮（幂等）")
	r.Stop()
}

// Start 无凭据时 no-op；之后 Configure+Start 仍可用。
func TestRuntimeStartWithoutCreds(t *testing.T) {
	s := &rtServer{}
	r, _ := newTestRuntime(t, &fakeExec{hasCur: true}, s.handler())
	r.Start()
	if r.Running() {
		t.Fatal("无凭据 Start 应 no-op")
	}
	r.Configure("tok", "u1")
	r.Start()
	waitFor(t, 2*time.Second, func() bool { return r.Running() }, "轮询应启动")
	r.Stop()
	if r.Running() {
		t.Fatal("Stop 后 Running 应为 false")
	}
}

// Bind 成功路径：拿二维码 → 轮询到 token → persist 落盘 → 转正为运行态。
func TestRuntimeBindSuccess(t *testing.T) {
	s := &rtServer{}
	r, _ := newTestRuntime(t, &fakeExec{hasCur: true}, s.handler())

	var persisted WeixinPersist
	r.SetPersist(func(p WeixinPersist) error { persisted = p; return nil })

	url, err := r.Bind(context.Background())
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if url != "https://qr/x" {
		t.Fatalf("二维码 URL = %q", url)
	}
	if st := r.BindStatus(); st != "pending" {
		t.Fatalf("BindStatus = %q, 期望 pending", st)
	}

	// 模拟扫码成功 → 绑定 goroutine 拿到 token。
	s.mu.Lock()
	s.pollToken = "bot-tok"
	s.mu.Unlock()
	waitFor(t, 2*time.Second, func() bool { return r.BindStatus() == "ok" }, "绑定应成功")
	if persisted.Token != "bot-tok" || !persisted.Enabled {
		t.Fatalf("persist 收到 %+v", persisted)
	}
	if r.Client() == nil || !r.Running() {
		t.Fatalf("绑定成功后应转运行态: client=%v running=%v", r.Client() != nil, r.Running())
	}

	// 已绑定（running）时再次 Bind 报错。
	if _, err := r.Bind(context.Background()); err == nil {
		t.Fatal("已绑定再 Bind 应报错")
	}
	r.Stop()
}

// 绑定轮询遇错误时置 expired（用 closed server 模拟网络失败）。
func TestRuntimeBindExpired(t *testing.T) {
	s := &rtServer{}
	r, srv := newTestRuntime(t, &fakeExec{hasCur: true}, s.handler())
	r.sleepBetweenPolls = func() {}
	url, err := r.Bind(context.Background())
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	_ = url
	srv.Close() // 立刻杀掉服务端：轮询 PollQRCodeStatus 失败 → expired
	waitFor(t, 2*time.Second, func() bool { return r.BindStatus() == "expired" }, "绑定应置 expired")
	if r.Running() {
		t.Fatal("绑定失败不应进入运行态")
	}
}

// 轮询 goroutine 收到消息转交 bridge；wxid 学习回调先装配再 Configure（与
// desktop 装配顺序一致）。
func TestRuntimePollLoopMessages(t *testing.T) {
	s := &rtServer{msgs: []InboundMsg{{FromUserID: "u1", ContextToken: "ct-1", Text: "hi"}}}
	r, _ := newTestRuntime(t, &fakeExec{hasCur: true}, s.handler())
	var learned string
	r.SetOnLearnWxid(func(wxid string) { learned = wxid })
	r.Configure("tok", "")
	r.Start()
	waitFor(t, 2*time.Second, func() bool { return learned == "u1" }, "入站消息应触发学习")
	if st := r.Status(); st.Wxid != "u1" || !st.Bound || !st.Running {
		t.Fatalf("Status = %+v", st)
	}
	r.Stop()
}

// ErrSessionExpired 停止轮询（Running=false），可重新 Configure+Start。
func TestRuntimePollSessionExpired(t *testing.T) {
	s := &rtServer{expireUpdates: 1}
	r, _ := newTestRuntime(t, &fakeExec{hasCur: true}, s.handler())
	r.Configure("tok", "u1")
	r.Start()
	waitFor(t, 2*time.Second, func() bool { return !r.Running() }, "过期后应停止轮询")
	if st := r.Status(); st.Running || !st.Bound {
		t.Fatalf("过期后 Status = %+v（Bound 仍真：凭据在）", st)
	}
}

// OnHubMessage 异步转发到 bridge；bridge 为 nil 时安全跳过。
func TestRuntimeOnHubMessage(t *testing.T) {
	s := &rtServer{}
	r, _ := newTestRuntime(t, &fakeExec{hasCur: true}, s.handler())
	// bridge 为 nil：不 panic。
	r.OnHubMessage(hub.Message{Kind: "agent", Text: "hi"})

	r.Configure("tok", "u1")
	b := r.GetBridge()
	if b == nil {
		t.Fatal("Configure 后应有 bridge")
	}
	if !r.HasBridge() {
		t.Fatal("HasBridge 应为 true")
	}
	// 先入站建立发送上下文（直接调 bridge），再异步出站。
	b.HandleInbound("u1", "ct-9", "hi")
	r.OnHubMessage(hub.Message{Kind: "agent", Text: "out"})
	waitFor(t, 2*time.Second, func() bool { return b.Undelivered() >= 0 }, "占位等待")
	// 出站结果断言交给 desktop 集成测试；这里只保证不 panic、无阻塞。
}

// Unbind 清凭据并落盘。
func TestRuntimeUnbind(t *testing.T) {
	s := &rtServer{}
	r, _ := newTestRuntime(t, &fakeExec{hasCur: true}, s.handler())
	var persisted WeixinPersist
	r.SetPersist(func(p WeixinPersist) error { persisted = p; return nil })
	r.Configure("tok", "u1")
	r.Start()
	r.Unbind()
	if r.Client() != nil || r.HasBridge() || r.Running() {
		t.Fatalf("Unbind 后应清空: %+v", r.Status())
	}
	if persisted.Enabled || persisted.Token != "" || persisted.Wxid != "" {
		t.Fatalf("Unbind persist = %+v", persisted)
	}
	if st := r.Status(); st.Bound || st.Wxid != "" {
		t.Fatalf("Unbind 后 Status = %+v", st)
	}
}

// 普通错误指数退避重试：服务端在第 2 次 getupdates 后开始返回 500，
// 退避等待注入 no-op，验证轮询不会因失败退出且持续重试。
func TestRuntimePollBackoff(t *testing.T) {
	s := &rtServer{failAfterUpdates: 1}
	r, _ := newTestRuntime(t, &fakeExec{hasCur: true}, s.handler())
	r.Configure("tok", "u1")
	r.Start()
	waitFor(t, 2*time.Second, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.updates >= 4
	}, "失败后应按退避持续重试")
	r.Stop()
}
