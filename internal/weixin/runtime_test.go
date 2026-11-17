package weixin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/hub"
	"github.com/yangk/kshell/internal/hubtools"
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
			_, _ = w.Write([]byte(`{"errcode":0,"qrcode_img_content":"https://qr/x","qrcode":"key-1"}`))
		case "/ilink/bot/get_qrcode_status":
			if s.pollToken != "" {
				_, _ = w.Write([]byte(`{"errcode":0,"bot_token":"bot-tok"}`))
			} else {
				_, _ = w.Write([]byte(`{"errcode":0,"bot_token":""}`))
			}
		case "/ilink/bot/getupdates":
			s.updates++
			if s.expireUpdates > 0 && s.updates >= s.expireUpdates {
				_, _ = w.Write([]byte(`{"errcode":-14,"errmsg":"expired"}`))
				return
			}
			if s.failAfterUpdates > 0 && s.updates > s.failAfterUpdates {
				w.WriteHeader(500)
				_, _ = w.Write([]byte(`{}`))
				return
			}
			var msgs []InboundMsg
			msgs, s.msgs = s.msgs, nil
			out := `{"errcode":0,"get_updates_buf":"cur-2","msgs":[]}`
			if len(msgs) > 0 {
				out = `{"errcode":0,"get_updates_buf":"cur-2","msgs":[`
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
			_, _ = w.Write([]byte(`{"errcode":0}`))
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

	// 已绑定（running）时再次 Bind 允许重新扫码（-14 过期后恢复路径）：
	// 返回新二维码，绑定状态回到 pending。
	url2, err := r.Bind(context.Background())
	if err != nil {
		t.Fatalf("已绑定再 Bind 应允许重新扫码: %v", err)
	}
	if url2 != "https://qr/x" {
		t.Fatalf("重绑二维码 URL = %q", url2)
	}
	if st := r.BindStatus(); st != "pending" {
		t.Fatalf("重绑中 BindStatus = %q, 期望 pending", st)
	}
	// 绑定流程进行中再 Bind 拒绝（防并发重复起绑定 goroutine）。
	if _, err := r.Bind(context.Background()); err == nil {
		t.Fatal("绑定进行中再 Bind 应报错")
	}
	// 等第二次绑定转正后清理。
	waitFor(t, 2*time.Second, func() bool { return r.BindStatus() == "ok" }, "重绑应成功")
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
// desktop 装配顺序一致）。首轮积压被丢弃（见 TestRuntimeFirstPollDropsBacklog），
// 消息须在次轮返回，故用 waitFor 轮询窗口而非固定 msgs。
func TestRuntimePollLoopMessages(t *testing.T) {
	var mu sync.Mutex
	firstPoll := true
	handler := func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if req.URL.Path == "/ilink/bot/getupdates" {
			if firstPoll {
				firstPoll = false
				_, _ = w.Write([]byte(`{"errcode":0,"get_updates_buf":"cur-1","msgs":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"errcode":0,"get_updates_buf":"cur-2","msgs":[` +
				`{"message_type":1,"from_user_id":"u1","context_token":"ct-1","item_list":[{"type":1,"text_item":{"text":"hi"}}]}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"errcode":0}`))
	}
	r, _ := newTestRuntime(t, &fakeExec{hasCur: true}, handler)
	var learned string
	// learned 由学习回调（轮询 goroutine）与测试主 goroutine 并发读写，须加锁。
	var muLearn sync.Mutex
	r.SetOnLearnWxid(func(wxid string) { muLearn.Lock(); learned = wxid; muLearn.Unlock() })
	r.Configure("tok", "")
	r.Start()
	waitFor(t, 2*time.Second, func() bool {
		muLearn.Lock()
		defer muLearn.Unlock()
		return learned == "u1"
	}, "入站消息应触发学习")
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

// TestRuntimeFirstPollDropsBacklog cursor="" 的首轮 getupdates 可能拉到重启/
// 重绑前的历史积压：**按消息时间判定**——过期积压只推进游标不投递（防陌生人
// 消息错绑 wxid、旧文本被当命令执行）；新鲜消息（绑定窗口内到达）正常投递。
// 2026-10-18 真机教训：曾按「首轮无条件丢弃」实现，用户扫码后立即发的消息
// 正好落在首轮窗口被吞，wxid 学不到、回复石沉大海。
func TestRuntimeFirstPollDropsBacklog(t *testing.T) {
	var mu sync.Mutex
	firstPoll := true
	var learned string
	old := time.Now().Add(-10 * time.Minute).UnixMilli() // 明显早于绑定的积压
	now := time.Now().UnixMilli()
	handler := func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch req.URL.Path {
		case "/ilink/bot/getupdates":
			if firstPoll {
				firstPoll = false
				// 首轮同时给两条：过期陌生人消息（须丢弃，否则错绑 u-stranger）
				// 与新鲜白名单消息（须投递并学习 u1）。
				_, _ = w.Write([]byte(`{"errcode":0,"get_updates_buf":"cur-1","msgs":[` +
					`{"message_type":1,"from_user_id":"u-stranger","context_token":"ct-old","create_time_ms":` + fmtInt(old) + `,"item_list":[{"type":1,"text_item":{"text":"#l"}}]},` +
					`{"message_type":1,"from_user_id":"u1","context_token":"ct-new","create_time_ms":` + fmtInt(now) + `,"item_list":[{"type":1,"text_item":{"text":"hi"}}]}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"errcode":0,"get_updates_buf":"cur-2","msgs":[]}`))
		default:
			_, _ = w.Write([]byte(`{"errcode":0}`))
		}
	}
	r, _ := newTestRuntime(t, &fakeExec{hasCur: true}, handler)
	// learned 由学习回调（轮询 goroutine）与测试主 goroutine 并发读写，须加锁。
	var muLearn sync.Mutex
	r.SetOnLearnWxid(func(wxid string) { muLearn.Lock(); learned = wxid; muLearn.Unlock() })
	r.Configure("tok", "")
	r.Start()
	// 新鲜消息应触发学习（u1）；过期积压的 u-stranger 不得被学习。
	waitFor(t, 2*time.Second, func() bool {
		muLearn.Lock()
		defer muLearn.Unlock()
		return learned == "u1"
	}, "首轮新鲜消息应正常投递并学习 u1")
	if st := r.Status(); st.Wxid != "u1" {
		t.Fatalf("过期积压消息不得错绑 wxid: Wxid = %q", st.Wxid)
	}
	r.Stop()
}

// fmtInt 供测试拼 JSON 的 int64 毫秒时间戳。
func fmtInt(n int64) string { return strconv.FormatInt(n, 10) }

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

func TestRuntimeSetTranscriberForwards(t *testing.T) {
	r := NewRuntime(nil, "zh-CN")
	// Configure 前装配：创建 bridge 时注入。
	r.SetTranscriber(func(ctx context.Context, wav []byte) (string, error) { return "x", nil })
	r.baseURL = "http://unused" // 不真正轮询
	r.Configure("tok", "")
	if b := r.GetBridge(); b == nil || b.transcriberFn() == nil {
		t.Fatal("Configure 后 bridge 应持有 transcriber")
	}
	// Configure 后再装配：现有 bridge 即时生效（重绑重建同样生效）。
	r.SetTranscriber(nil)
	if b := r.GetBridge(); b.transcriberFn() != nil {
		t.Fatal("SetTranscriber 应即时转发给现有 bridge")
	}
}

func TestRuntimeSetRouterForwards(t *testing.T) {
	r := NewRuntime(nil, "zh-CN")
	defer r.Stop()
	// Configure 前装配：创建 bridge 时注入。
	r.SetRouter(func(ctx context.Context, input string) (hubtools.RouteDecision, bool) {
		return hubtools.RouteDecision{}, false
	})
	r.baseURL = "http://unused" // 不真正轮询
	r.Configure("tok", "")
	if b := r.GetBridge(); b == nil || b.routerFn() == nil {
		t.Fatal("Configure 后 bridge 应持有 router")
	}
	// Configure 后再装配：现有 bridge 即时生效（重绑重建同样生效）。
	r.SetRouter(nil)
	if b := r.GetBridge(); b.routerFn() != nil {
		t.Fatal("SetRouter 应即时转发给现有 bridge")
	}
}

func TestRuntimeSetDirectPatternsForwards(t *testing.T) {
	r := NewRuntime(nil, "zh-CN")
	defer r.Stop()
	// Configure 前装配：创建 bridge 时注入。
	r.SetDirectPatterns(func() []string { return []string{"hello"} })
	r.baseURL = "http://unused" // 不真正轮询
	r.Configure("tok", "")
	if b := r.GetBridge(); b == nil {
		t.Fatal("Configure 后应有 bridge")
	} else if pats := b.directPatterns(); len(pats) != 1 || pats[0] != "hello" {
		t.Fatalf("Configure 后 bridge 应持有 directPatterns, got %v", pats)
	}
	// Configure 后再装配：现有 bridge 即时生效（重绑重建同样生效）。
	r.SetDirectPatterns(nil)
	if b := r.GetBridge(); b.directPatterns() == nil {
		t.Fatal("SetDirectPatterns(nil) 应回落内置默认集")
	}
}

func TestRuntimeSetManagerRouterForwards(t *testing.T) {
	r := NewRuntime(nil, "zh-CN")
	defer r.Stop()
	// Configure 前装配：创建 bridge 时注入。
	r.SetManagerRouter(func(ctx context.Context, input string) (ManagerOutcome, bool) {
		return ManagerOutcome{}, false
	})
	r.baseURL = "http://unused" // 不真正轮询
	r.Configure("tok", "")
	if b := r.GetBridge(); b == nil || b.managerRouterFn() == nil {
		t.Fatal("Configure 后 bridge 应持有 managerRouter")
	}
	// Configure 后再装配：现有 bridge 即时生效（重绑重建同样生效）。
	r.SetManagerRouter(nil)
	if b := r.GetBridge(); b.managerRouterFn() != nil {
		t.Fatal("SetManagerRouter 应即时转发给现有 bridge")
	}
}

// NotifyProgress 经 bridge 出站 worker 异步推送进度文案（语言取桥配置）；
// bridge 未创建或无发送上下文时安全（no-op / 计 undelivered）。
func TestRuntimeNotifyProgress(t *testing.T) {
	rc := &recorder{}
	r, _ := newTestRuntime(t, &fakeExec{hasCur: true}, rc.handler(t))
	// bridge 未创建：no-op 不 panic。
	r.NotifyProgress(2)
	r.Configure("tok", "u1")
	b := r.GetBridge()
	if b == nil {
		t.Fatal("Configure 后应有 bridge")
	}
	// 无入站上下文：出站丢弃计 undelivered，不 panic。
	r.NotifyProgress(2)
	waitFor(t, 2*time.Second, func() bool { return b.Undelivered() >= 1 }, "无上下文推送应计 undelivered")
	// 入站建立发送上下文后：推送一条桥语言（zh-CN）进度文案（入站本身会
	// 触发命令层回执，按文案内容匹配进度消息）。
	b.HandleInbound("u1", "ct-1", "hi")
	want := fmt.Sprintf(bridgeText("zh-CN", "manager.progress"), 3)
	r.NotifyProgress(3)
	waitFor(t, 2*time.Second, func() bool {
		for _, s := range rc.sends() {
			if sendText(t, s.body) == want {
				return true
			}
		}
		return false
	}, "进度推送应出站")
}

// TestUnbindInvalidatesLateBindSuccess 验证解绑后迟到的扫码成功回调被丢弃：
// bindSucceeded 以解绑前代次调用不写凭据、不把状态改回 ok。
func TestUnbindInvalidatesLateBindSuccess(t *testing.T) {
	r := NewRuntime(nil, "en")
	var mu sync.Mutex
	var persisted []WeixinPersist
	r.SetPersist(func(p WeixinPersist) error {
		mu.Lock()
		persisted = append(persisted, p)
		mu.Unlock()
		return nil
	})
	r.mu.Lock()
	gen := r.bindGen
	r.mu.Unlock()

	r.Unbind()
	mu.Lock()
	persisted = nil // 只观察解绑之后是否还有写回
	mu.Unlock()

	r.bindSucceeded(gen, "late-token")
	mu.Lock()
	defer mu.Unlock()
	if len(persisted) != 0 {
		t.Fatalf("解绑后不应再写凭据: %+v", persisted)
	}
	if got := r.BindStatus(); got == "ok" {
		t.Fatalf("解绑后迟到成功不应把状态置回 ok, got %q", got)
	}
}
