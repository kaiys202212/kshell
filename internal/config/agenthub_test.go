package config

import "testing"

// RouteModeNormalize 验证 AgentHub 路由模式归一：空/非法值回落 auto。
func TestAgentHubRouteModeNormalize(t *testing.T) {
	cases := map[string]string{
		"":           RouteModeAuto,
		"  ":         RouteModeAuto,
		"bogus":      RouteModeAuto,
		"agent":      RouteModeAgent,
		"force_auto": RouteModeForceAuto,
		" auto ":     RouteModeAuto,
	}
	for in, want := range cases {
		c := Default()
		c.AgentHub.RouteMode = in
		if got := c.normalized().AgentHub.RouteMode; got != want {
			t.Fatalf("RouteMode %q 归一为 %q, want %q", in, got, want)
		}
	}
}

// EnabledOn 验证 enabled 指针语义:未写(nil)=启用,显式 false=关闭,显式 true=启用。
func TestAgentHubEnabledOn(t *testing.T) {
	on := true
	off := false
	cases := []struct {
		name string
		in   *bool
		want bool
	}{
		{"nil 默认启用", nil, true},
		{"显式 true", &on, true},
		{"显式 false", &off, false},
	}
	for _, tc := range cases {
		c := Default()
		c.AgentHub.Enabled = tc.in
		if got := c.AgentHub.EnabledOn(); got != tc.want {
			t.Fatalf("%s: EnabledOn()=%v, want %v", tc.name, got, tc.want)
		}
	}
}

// AgentHubOrchestrationNormalize 验证心跳阈值钳制与逃生门白名单清洗:
// heartbeat<=0 → 5;>9999 钳到 120;direct_patterns 去空白项、全空回落 nil;
// patterns=nil 保持 nil(空=默认集语义,默认集由消费方 hub 包定义)。
func TestAgentHubOrchestrationNormalize(t *testing.T) {
	c := Default()
	c.AgentHub.HeartbeatMinutes = 0
	c.AgentHub.DirectPatterns = []string{"", "  @  ", "@tool", ""}
	got := c.normalized().AgentHub
	if got.HeartbeatMinutes != 5 {
		t.Fatalf("HeartbeatMinutes=%d, want 5(钳默认)", got.HeartbeatMinutes)
	}
	want := []string{"@", "@tool"}
	if len(got.DirectPatterns) != len(want) {
		t.Fatalf("DirectPatterns=%v, want %v", got.DirectPatterns, want)
	}
	for i := range want {
		if got.DirectPatterns[i] != want[i] {
			t.Fatalf("DirectPatterns[%d]=%q, want %q", i, got.DirectPatterns[i], want[i])
		}
	}
	c2 := Default()
	if c2.normalized().AgentHub.DirectPatterns != nil {
		t.Fatalf("nil patterns 被改写: %v", c2.AgentHub.DirectPatterns)
	}
	c3 := Default()
	c3.AgentHub.HeartbeatMinutes = 9999
	if got3 := c3.normalized().AgentHub; got3.HeartbeatMinutes != 120 {
		t.Fatalf("HeartbeatMinutes=%d, want 120(上限)", got3.HeartbeatMinutes)
	}
}
