package hubtools

import (
	"strings"
	"testing"
)

// TestBuildSystemPromptContainsTools 验证提示词含全部工具名与路由规则：
// routeAware=true 含路由 JSON 约定，false 为纯工具人设。
func TestBuildSystemPromptContainsTools(t *testing.T) {
	reg := NewRegistry(&fakeBridge{})
	tools := reg.Tools()

	p := BuildSystemPrompt(tools, true)
	for _, td := range tools {
		if !strings.Contains(p, td.Name) {
			t.Errorf("提示词应含工具名 %s", td.Name)
		}
	}
	if !strings.Contains(p, RouteAgentJSON) {
		t.Error("auto 模式提示词应含 route=agent 路由约定")
	}

	plain := BuildSystemPrompt(tools, false)
	if strings.Contains(plain, RouteAgentJSON) {
		t.Error("force_auto 提示词不应含路由约定")
	}
}

// TestParseRouteAgent 验证 route=agent 判定：纯 JSON、混在文本中的 JSON、
// 非法 JSON、route 非 agent、无 JSON。
func TestParseRouteAgent(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{`{"route":"agent"}`, true},
		{"好的，这个交给编程工具处理。{\"route\":\"agent\"}", true},
		{`{"route":"agent"}多余文本`, true},
		{`{"route":"tools"}`, false},
		{"这不是 JSON", false},
		{`{"broken`, false},
		{"", false},
	}
	for _, c := range cases {
		if got := ParseRouteAgent(c.text); got != c.want {
			t.Errorf("ParseRouteAgent(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}
