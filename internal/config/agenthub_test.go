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
