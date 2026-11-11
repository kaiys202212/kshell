package desktop

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/config"
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
	if _, err := a.WeixinBind(); err != nil && strings.Contains(err.Error(), "agenthub") {
		t.Fatalf("开启后仍被门控拦截: %v", err)
	}
}
