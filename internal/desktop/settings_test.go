package desktop

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/weixin"
)

// TestAgentHubSettingsRoundTrip 验证 AgentHub 设置绑定：缺省视图、保存归一
// （心跳钳默认、白名单 TrimSpace 去空白项）、总开关翻转落盘。
func TestAgentHubSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	layout := config.Layout{
		Root:   dir,
		Config: filepath.Join(dir, "config.yaml"),
		Cache:  filepath.Join(dir, "cache"),
	}
	a := NewAppWith(Options{Config: config.Default(), Layout: layout})

	// 缺省视图： EnabledOn(nil)=true，心跳=5（config.agenthubHeartbeatDefault 未导出，取字面值）
	v := a.GetAgentHubSettings()
	if !v.Enabled {
		t.Fatalf("缺省 Enabled=false, want true")
	}
	if v.HeartbeatMinutes != 5 {
		t.Fatalf("缺省 HeartbeatMinutes=%d, want 5", v.HeartbeatMinutes)
	}

	// 保存逃生门与心跳（含待 TrimSpace 项）
	if err := a.SaveAgentHubSettings(AgentHubSettingsInput{HeartbeatMinutes: 10, DirectPatterns: []string{"#", "@", " continue "}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	v = a.GetAgentHubSettings()
	if v.HeartbeatMinutes != 10 || len(v.DirectPatterns) != 3 || v.DirectPatterns[2] != "continue" {
		t.Fatalf("roundtrip 不符: %+v", v)
	}

	// 非法心跳钳默认而非报错
	if err := a.SaveAgentHubSettings(AgentHubSettingsInput{HeartbeatMinutes: -1}); err != nil {
		t.Fatalf("Save(负值): %v", err)
	}
	if v := a.GetAgentHubSettings(); v.HeartbeatMinutes != 5 {
		t.Fatalf("负值未钳默认: %d", v.HeartbeatMinutes)
	}

	// 总开关翻转落盘
	if err := a.SetAgentHubEnabled(false); err != nil {
		t.Fatalf("off: %v", err)
	}
	if v := a.GetAgentHubSettings(); v.Enabled {
		t.Fatalf("关闭失败")
	}
	if err := a.SetAgentHubEnabled(true); err != nil {
		t.Fatalf("on: %v", err)
	}
	if v := a.GetAgentHubSettings(); !v.Enabled {
		t.Fatalf("开启失败")
	}

	// 文件级读回：显式 true 应写进 yaml（Enabled 非 nil 且为 true）
	loaded, err := config.Load(layout)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.AgentHub.Enabled == nil || !*loaded.AgentHub.Enabled {
		t.Fatalf("落盘读回 Enabled=%v, want 非 nil 且 true", loaded.AgentHub.Enabled)
	}
}

// TestSetAgentHubEnabledOffUnlinksWeixin 验证关闭总开关会解绑微信：
// 清空 token/wxid 并广播 agenthub:changed；重新开启不会复活旧凭据。
func TestSetAgentHubEnabledOffUnlinksWeixin(t *testing.T) {
	dir := t.TempDir()
	layout := config.Layout{
		Root:   dir,
		Config: filepath.Join(dir, "config.yaml"),
		Cache:  filepath.Join(dir, "cache"),
	}
	cfg := config.Default()
	cfg.Weixin = config.WeixinConfig{Enabled: true, Token: "tok", Wxid: "wxid"}
	var events []string
	var payloads []map[string]any
	a := NewAppWith(Options{
		Config: cfg,
		Layout: layout,
		Emit: func(name string, data ...any) {
			events = append(events, name)
			if name == "agenthub:changed" && len(data) == 1 {
				if m, ok := data[0].(map[string]any); ok {
					payloads = append(payloads, m)
				}
			}
		},
	})
	// 注入真实 Runtime 并记录 Unbind 落盘：NewAppWith 不跑 Startup，a.wx 需手工装配，
	// 否则 SetAgentHubEnabled 里的取消绑定/清凭据分支在测试中不会被执行（假绿）。
	wx := weixin.NewRuntime(nil, "en")
	unbound := false
	wx.SetPersist(func(p weixin.WeixinPersist) error {
		if p.Token == "" {
			unbound = true
		}
		return nil
	})
	a.mu.Lock()
	a.wx = wx
	a.mu.Unlock()

	if err := a.SetAgentHubEnabled(false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	loaded, err := config.Load(layout)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Weixin.Token != "" || loaded.Weixin.Wxid != "" || loaded.Weixin.Enabled {
		t.Fatalf("关闭后微信凭据未清空: %+v", loaded.Weixin)
	}
	if !unbound {
		t.Fatal("关闭时未调用 Runtime.Unbind 清空凭据")
	}
	if len(payloads) == 0 || payloads[len(payloads)-1]["enabled"] != false {
		t.Fatalf("事件载荷应为 enabled=false, payloads=%v", payloads)
	}

	// SaveAgentHubSettings 同样广播变更（首页无需区分来源）。
	events = events[:0]
	if err := a.SaveAgentHubSettings(AgentHubSettingsInput{HeartbeatMinutes: 7}); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	found := false
	for _, e := range events {
		if e == "agenthub:changed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("SaveAgentHubSettings 未广播 agenthub:changed, events=%v", events)
	}

	// 重新开启不恢复旧凭据：必须重新扫码。
	if err := a.SetAgentHubEnabled(true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	loaded2, err := config.Load(layout)
	if err != nil {
		t.Fatalf("Load after enable: %v", err)
	}
	if loaded2.Weixin.Token != "" || loaded2.Weixin.Wxid != "" {
		t.Fatalf("开启后凭据被复活: %+v", loaded2.Weixin)
	}
}

// TestStartWeixinClearsLegacyCredsWhenDisabled 验证旧版本遗留 token 在总开关
// 关闭态启动时被清空：重新开启不会意外恢复旧绑定。
func TestStartWeixinClearsLegacyCredsWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	layout := config.Layout{
		Root:   dir,
		Config: filepath.Join(dir, "config.yaml"),
		Cache:  filepath.Join(dir, "cache"),
	}
	cfg := config.Default()
	off := false
	cfg.AgentHub.Enabled = &off
	cfg.Weixin = config.WeixinConfig{Enabled: true, Token: "legacy", Wxid: "wxid"}
	a := NewAppWith(Options{Config: cfg, Layout: layout})

	a.startWeixin()
	loaded, err := config.Load(layout)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Weixin.Token != "" || loaded.Weixin.Wxid != "" || loaded.Weixin.Enabled {
		t.Fatalf("关闭态启动未清理遗留凭据: %+v", loaded.Weixin)
	}
}

// agentHubTestApp 构造带落盘 Layout 的 App（saveConfig 需要 Layout.Config，
// 套路同 TestAgentHubSettingsRoundTrip；无扫描数据）。
func agentHubTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	layout := config.Layout{
		Root:   dir,
		Config: filepath.Join(dir, "config.yaml"),
		Cache:  filepath.Join(dir, "cache"),
	}
	return NewAppWith(Options{Config: config.Default(), Layout: layout})
}

// TestAgentHubGateBlocksWeixinBind 验证 AgentHub 总开关关闭时 WeixinBind 被
// 门控拒绝（错误含 agenthub）；重新开启后放行（未配凭据时返回非门控错误）。
func TestAgentHubGateBlocksWeixinBind(t *testing.T) {
	a := agentHubTestApp(t)
	if err := a.SetAgentHubEnabled(false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := a.WeixinBind(); err == nil || !strings.Contains(err.Error(), "agenthub") {
		t.Fatalf("关闭时应被门控拒绝, got %v", err)
	}
	if err := a.SetAgentHubEnabled(true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	// 恢复后放行：未配微信凭据时应返回 errNotReady 类错误而非门控错误。
	// 测试气味说明：SetAgentHubEnabled(true) 会经 applyAgentHubGates 走
	// startWeixin 内部 new 出的 Runtime，其 iLink baseURL 无注入点
	//（Runtime.SetBaseURL 只能打在测试自建的 Runtime 上），因此这里
	// Bind 放行分支会向真实默认端点发一次取二维码请求；不为此新建注入
	// 基建，仅以断言只关注「非门控错误」来限定观察面。
	if _, err := a.WeixinBind(); err != nil && strings.Contains(err.Error(), "agenthub") {
		t.Fatalf("开启后仍被门控拦截: %v", err)
	}
}
