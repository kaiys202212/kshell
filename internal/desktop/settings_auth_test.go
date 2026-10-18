package desktop

import (
	"testing"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

// fakeAuthProvider 在 fakeProvider 上叠加可控的 ProbeAuth 返回值与调用计数，
// 验证 CheckToolAuth 走实时探测而非缓存结论。
type fakeAuthProvider struct {
	fakeProvider
	status providers.AuthStatus
	calls  int
}

func (f *fakeAuthProvider) ProbeAuth() providers.AuthStatus {
	f.calls++
	return f.status
}

// newAuthApp 组装带指定 provider 表的 App（toolsReady 直接落位，不触发真实扫描）。
func newAuthApp(t *testing.T, ps []providers.Provider) *App {
	t.Helper()
	return newAgentSetupApp(t, false, []discovery.Tool{
		{ID: "cursor", Name: "Cursor", Installed: true, BinPath: "cursor-agent"},
	}, ps)
}

func TestCheckToolAuthProbesLive(t *testing.T) {
	p := &fakeAuthProvider{id: "cursor", status: providers.AuthOut}
	app := newAuthApp(t, []providers.Provider{p})

	got, err := app.CheckToolAuth("cursor")
	if err != nil {
		t.Fatal(err)
	}
	if got != string(providers.AuthOut) || p.calls != 1 {
		t.Fatalf("CheckToolAuth = %q calls = %d, want out/1", got, p.calls)
	}

	// 登录完成后的状态翻转必须立刻反映：轮询拿到的是实时结论而非缓存
	p.status = providers.AuthIn
	got, err = app.CheckToolAuth("cursor")
	if err != nil {
		t.Fatal(err)
	}
	if got != string(providers.AuthIn) || p.calls != 2 {
		t.Fatalf("再次探测应为实时结果: got %q calls = %d, want in/2", got, p.calls)
	}
}

// TestCheckToolAuthUnknownToolID 未知 toolID 返回 unknown 且不报错：
// 前端轮询逻辑据此继续等待，不能当作异常打断弹窗流程。
func TestCheckToolAuthUnknownToolID(t *testing.T) {
	app := newAuthApp(t, []providers.Provider{&fakeAuthProvider{id: "cursor"}})

	got, err := app.CheckToolAuth("nope")
	if err != nil {
		t.Fatalf("未知 toolID 不应报错: %v", err)
	}
	if got != string(providers.AuthUnknown) {
		t.Fatalf("CheckToolAuth = %q, want %q", got, providers.AuthUnknown)
	}
}

// TestCheckToolAuthWithoutProber 未实现 AuthProber 的 provider 返回 unknown 且不报错。
func TestCheckToolAuthWithoutProber(t *testing.T) {
	app := newAuthApp(t, []providers.Provider{fakeProvider{id: "claude"}})

	got, err := app.CheckToolAuth("claude")
	if err != nil {
		t.Fatalf("无 Prober 不应报错: %v", err)
	}
	if got != string(providers.AuthUnknown) {
		t.Fatalf("CheckToolAuth = %q, want %q", got, providers.AuthUnknown)
	}
}
