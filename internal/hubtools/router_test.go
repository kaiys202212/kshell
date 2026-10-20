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

	p := BuildSystemPrompt(tools, true, nil)
	for _, td := range tools {
		if !strings.Contains(p, td.Name) {
			t.Errorf("提示词应含工具名 %s", td.Name)
		}
	}
	if !strings.Contains(p, RouteAgentJSON) {
		t.Error("auto 模式提示词应含 route=agent 路由约定")
	}

	plain := BuildSystemPrompt(tools, false, nil)
	if strings.Contains(plain, RouteAgentJSON) {
		t.Error("force_auto 提示词不应含路由约定")
	}
}

// TestBuildSystemPromptWithRecent 验证聊天记录注入：auto 模式渲染编号记录 +
// 派发规则；force_auto 带记录但不含路由/派发规则段。
func TestBuildSystemPromptWithRecent(t *testing.T) {
	recent := []ChatLogEntry{
		{Agent: "Cursor「修复登录」", Sender: "用户", Text: "帮我修下登录页报错"},
		{Agent: "Cursor「修复登录」", Sender: "助手", Text: "已定位到空指针"},
	}
	p := BuildSystemPrompt(nil, true, recent)
	if !strings.Contains(p, "[1] 会话=Cursor「修复登录」 方向=用户 内容=帮我修下登录页报错") {
		t.Errorf("记录未按编号渲染进提示词:\n%s", p)
	}
	if !strings.Contains(p, "[2] 会话=Cursor「修复登录」 方向=助手") {
		t.Errorf("第二条记录未渲染:\n%s", p)
	}
	if !strings.Contains(p, "dispatch_task") {
		t.Errorf("auto 模式缺派发规则:\n%s", p)
	}
	if !strings.Contains(p, RouteAgentJSON) {
		t.Errorf("auto 模式缺转投兜底约定:\n%s", p)
	}

	pf := BuildSystemPrompt(nil, false, recent)
	if strings.Contains(pf, RouteAgentJSON) || strings.Contains(pf, "路由规则") {
		t.Errorf("force_auto 不应含路由/派发规则:\n%s", pf)
	}
	if !strings.Contains(pf, "[1] 会话=Cursor「修复登录」") {
		t.Errorf("force_auto 也应带聊天记录:\n%s", pf)
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
