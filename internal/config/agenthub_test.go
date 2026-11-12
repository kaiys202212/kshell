package config

import "testing"

// RouteModeNormalize 验证 AgentHub 路由模式归一：空/非法值回落 auto；
// 已下线的 force_auto 也按 auto 处理（旧配置自动兼容）。
func TestAgentHubRouteModeNormalize(t *testing.T) {
	cases := map[string]string{
		"":           RouteModeAuto,
		"  ":         RouteModeAuto,
		"bogus":      RouteModeAuto,
		"agent":      RouteModeAgent,
		"force_auto": RouteModeAuto, // 工具循环已下线：退化为默认智能路由
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
// heartbeat<=0 → agenthubHeartbeatDefault;超上限钳到 agenthubHeartbeatMax;
// direct_patterns 去空白项、去重、全空回落 nil;
// patterns=nil 保持 nil(空=默认集语义,默认集由消费方 hub 包定义)。
func TestAgentHubOrchestrationNormalize(t *testing.T) {
	c := Default()
	c.AgentHub.HeartbeatMinutes = 0
	c.AgentHub.DirectPatterns = []string{"", "  @  ", "@tool", " @ ", ""}
	got := c.normalized().AgentHub
	if got.HeartbeatMinutes != agenthubHeartbeatDefault {
		t.Fatalf("HeartbeatMinutes=%d, want %d(钳默认)", got.HeartbeatMinutes, agenthubHeartbeatDefault)
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
	if got3 := c3.normalized().AgentHub; got3.HeartbeatMinutes != agenthubHeartbeatMax {
		t.Fatalf("HeartbeatMinutes=%d, want %d(上限)", got3.HeartbeatMinutes, agenthubHeartbeatMax)
	}
}

// TestAgentHubEnabledFalseRoundTrip 覆盖 yaml 往返对显式 false 的保留:
// enabled 指针 Save 落盘后 Load 必须仍是非 nil 的 false,不能退化回 nil(默认启用)。
func TestAgentHubEnabledFalseRoundTrip(t *testing.T) {
	p := tempPaths(t)
	off := false
	want := Default()
	want.AgentHub.Enabled = &off
	if err := Save(p, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.AgentHub.Enabled == nil || *got.AgentHub.Enabled != false {
		t.Fatalf("显式 false 往返丢失: %+v", got.AgentHub.Enabled)
	}
	if got.AgentHub.EnabledOn() {
		t.Fatal("EnabledOn 应为 false")
	}
	// 未写 enabled 的默认配置往返后保持 nil(默认启用)。
	p2 := tempPaths(t)
	base := Default()
	if err := Save(p2, base); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got2, err := Load(p2)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got2.AgentHub.Enabled != nil {
		t.Fatalf("默认 enabled 应保持 nil, got %v", *got2.AgentHub.Enabled)
	}
}
