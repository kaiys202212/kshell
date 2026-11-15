package agentcli_test

import (
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/agentapi"
	"github.com/yangk/kshell/internal/agentcli"
	"github.com/yangk/kshell/internal/version"
)

// UsageText 导出供 SKILL.md 复用，必须覆盖全部子命令。
func TestUsageTextCoversAllCommands(t *testing.T) {
	text := agentcli.UsageText()
	for _, cmd := range []string{
		"list", "search", "info", "output", "watch", "send", "stop", "recall",
		"models", "model", "perms", "perm", "create", "open", "new", "route",
	} {
		if !strings.Contains(text, cmd) {
			t.Fatalf("UsageText 缺少命令 %q\n%s", cmd, text)
		}
	}
	if !strings.Contains(text, "kshell agent") {
		t.Fatalf("UsageText 缺少程序名前缀:\n%s", text)
	}
}

// TestRunWarnsOnVersionMismatch：endpoint 携带的桌面端版本与 CLI 自身不一致时，
// 执行任何需连接的命令都应向 stderr 打一行提醒，但不阻断执行。
func TestRunWarnsOnVersionMismatch(t *testing.T) {
	h := newCliHarness(t)
	h.fake.Active = []agentapi.Session{{ID: "c1", Title: "t", Tool: "codebuddy"}}
	h.client.App = "v9.9.9"
	code := h.run("list")
	if code != 0 {
		t.Fatalf("list 退出码 = %d, want 0（提醒不得阻断执行）", code)
	}
	if !strings.Contains(h.errb.String(), "v9.9.9") {
		t.Fatalf("stderr 应包含桌面端版本提醒, got %q", h.errb.String())
	}
}

// TestRunNoWarnWhenVersionMatches：版本一致（dev == dev 或显式相等）不打提醒。
func TestRunNoWarnWhenVersionMatches(t *testing.T) {
	orig := version.Version
	defer func() { version.Version = orig }()

	cases := []struct{ name, app, self string }{
		{"双方 dev", "", "dev"},
		{"显式相等", "v1.2.3", "v1.2.3"},
		{"endpoint 缺 app 字段（旧桌面端）", "", "v1.2.3"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			version.Version = c.self
			h := newCliHarness(t)
			h.fake.Active = []agentapi.Session{{ID: "c1", Title: "t", Tool: "codebuddy"}}
			h.client.App = c.app
			_ = h.run("list")
			if strings.Contains(h.errb.String(), "不一致") {
				t.Fatalf("版本一致不应提醒, got %q", h.errb.String())
			}
		})
	}
}
