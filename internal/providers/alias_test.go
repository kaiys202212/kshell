package providers

import "testing"

// IsClaudeBuiltinAlias 只认 Claude CLI 能自行解析的内置别名（含 1m 变体）。
// 语义来源：CLI 对 /model opus 会回显 "Set model to opus (claude-opus-4-6)"
// 这种带括号的已解析形态，而对 /model claude 只回显原文。
func TestIsClaudeBuiltinAlias(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"default", true},
		{" Default ", true},
		{"sonnet", true},
		{"Sonnet", true},
		{"opus", true},
		{"haiku", true},
		{"sonnet[1m]", true},
		{"opus[1m]", true},
		{"haiku[1m]", true},
		{"sonnet-1m", true},
		{"opus-1m", true},
		{"haiku-1m", true},
		{"sonnet[1M]", true},
		// 非内置别名
		{"claude", false},
		{"claude-sonnet-4-6", false},
		{"glm-5.3-flash", false},
		{"deepseek-v4.1-flash", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsClaudeBuiltinAlias(c.in); got != c.want {
			t.Errorf("IsClaudeBuiltinAlias(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// IsClaudeUnpinnableModel 表示「不可写进 ANTHROPIC_MODEL」：内置别名 + "claude"。
// 依据实测：ANTHROPIC_MODEL 不经别名解析，写别名会被字面发给网关（400）。
func TestIsClaudeUnpinnableModel(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"default", true},
		{"claude", true},
		{" Claude ", true},
		{"sonnet", true},
		{"opus[1m]", true},
		{"haiku-1m", true},
		// 具体平台模型名可钉死
		{"glm-5.3-flash", false},
		{"deepseek-v4.1-flash-1017", false},
		{"claude-sonnet-4-6", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsClaudeUnpinnableModel(c.in); got != c.want {
			t.Errorf("IsClaudeUnpinnableModel(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
