package desktop

import (
	"path/filepath"
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

	// 缺省视图： EnabledOn(nil)=true，心跳=5
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
	_ = a.SaveAgentHubSettings(AgentHubSettingsInput{HeartbeatMinutes: -1})
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
}
