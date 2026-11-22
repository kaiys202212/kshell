package desktop

import (
	"testing"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

// agenthub 默认设置解析：工具与模型都只认 ⚙ 默认设置（config.yaml 是唯一权威），
// 显式值优先；默认设置未配置/不可用时回落首个已安装 ACP 工具。
func TestResolveCreateToolID(t *testing.T) {
	app := wxApp(t, nil)
	app.mu.Lock()
	app.tools = []discovery.Tool{
		{ID: "claude", Installed: true, ACP: &providers.ACPDetection{Available: true}},
		{ID: "codex", Installed: true}, // 已安装但无 ACP
		{ID: "trae", Installed: true, ACP: &providers.ACPDetection{Available: true}},
	}
	app.mu.Unlock()

	// 显式指定：原样返回，不做可用性替换（保持既有报错语义）。
	if got := app.resolveCreateToolID("codex", true); got != "codex" {
		t.Fatalf("显式工具应原样返回, got %q", got)
	}

	// 未配置：回落首个已安装 ACP 可用工具。
	if got := app.resolveCreateToolID("", true); got != "claude" {
		t.Fatalf("未配置应回落 claude, got %q", got)
	}

	// 配置了 ACP 可用的默认工具：优先。
	app.mu.Lock()
	app.opts.Config.AgentHub.DefaultTool = "trae"
	app.mu.Unlock()
	if got := app.resolveCreateToolID("", true); got != "trae" {
		t.Fatalf("默认工具应生效, got %q", got)
	}

	// 默认工具不支持 ACP：回落首个 ACP 可用（不能选中 codex）。
	app.mu.Lock()
	app.opts.Config.AgentHub.DefaultTool = "codex"
	app.mu.Unlock()
	if got := app.resolveCreateToolID("", true); got != "claude" {
		t.Fatalf("非 ACP 默认工具应回落 claude, got %q", got)
	}

	// 默认工具未安装：回落。
	app.mu.Lock()
	app.opts.Config.AgentHub.DefaultTool = "gone"
	app.mu.Unlock()
	if got := app.resolveCreateToolID("", true); got != "claude" {
		t.Fatalf("失效默认工具应回落 claude, got %q", got)
	}

	// 终端路径（requireACP=false）：已安装即可用。
	app.mu.Lock()
	app.opts.Config.AgentHub.DefaultTool = "codex"
	app.mu.Unlock()
	if got := app.resolveCreateToolID("", false); got != "codex" {
		t.Fatalf("终端路径应接受已安装的非 ACP 默认工具, got %q", got)
	}

	// 无任何可用工具：返回空（调用方报错）。
	app.mu.Lock()
	app.opts.Config.AgentHub.DefaultTool = ""
	app.tools = []discovery.Tool{{ID: "codex", Installed: false}}
	app.mu.Unlock()
	if got := app.resolveCreateToolID("", true); got != "" {
		t.Fatalf("无 ACP 可用工具应返回空, got %q", got)
	}
}

// 模型默认值：显式优先；未显式且工具与默认工具一致才用默认模型；
// claude 的 default 伪模型按「未指定」处理，不能下发非法模型 ID。
func TestResolveCreateModelID(t *testing.T) {
	app := wxApp(t, nil)
	app.mu.Lock()
	app.opts.Config.AgentHub.DefaultTool = "claude"
	app.opts.Config.AgentHub.DefaultModel = "m-default"
	app.mu.Unlock()

	if got := app.resolveCreateModelID("claude", "m-explicit"); got != "m-explicit" {
		t.Fatalf("显式模型应优先, got %q", got)
	}
	if got := app.resolveCreateModelID("claude", ""); got != "m-default" {
		t.Fatalf("默认工具未显式模型应取默认模型, got %q", got)
	}
	if got := app.resolveCreateModelID("trae", ""); got != "" {
		t.Fatalf("非默认工具不得套用默认模型, got %q", got)
	}
	if got := app.resolveCreateModelID("claude", "DEFAULT"); got != "" {
		t.Fatalf("claude default 伪模型应归一为空, got %q", got)
	}

	app.mu.Lock()
	app.opts.Config.AgentHub.DefaultTool = "claude"
	app.opts.Config.AgentHub.DefaultModel = "default"
	app.mu.Unlock()
	if got := app.resolveCreateModelID("claude", ""); got != "" {
		t.Fatalf("默认模型为 claude default 伪值时应归一为空, got %q", got)
	}

	// 非 claude 工具的同名字面值不是伪模型，保留。
	app.mu.Lock()
	app.opts.Config.AgentHub.DefaultTool = "trae"
	app.opts.Config.AgentHub.DefaultModel = "default"
	app.mu.Unlock()
	if got := app.resolveCreateModelID("trae", ""); got != "default" {
		t.Fatalf("非 claude 的 default 字面值应保留, got %q", got)
	}
}

// 现场事故：AgentHub 新建 claude 会话时 modelId 传入字面 "claude"，
// 旧实现只拦 "default"，于是被当合法模型名下发，网关 400。
func TestNormalizeCreateModelIDClaudeAliases(t *testing.T) {
	cases := []struct {
		toolID, in, want string
	}{
		{"claude", "default", ""},
		{"claude", "claude", ""},
		{"claude", " Claude ", ""},
		{"claude", "sonnet", ""},
		{"claude", "opus[1m]", ""},
		{"claude", "haiku-1m", ""},
		// 具体平台模型名保留
		{"claude", "glm-5.3-flash", "glm-5.3-flash"},
		{"claude", "claude-sonnet-4-6", "claude-sonnet-4-6"},
		// 非 claude 工具不拦（可能真有同名模型）
		{"cursor", "default", "default"},
		{"cursor", "claude", "claude"},
		{"opencode", "sonnet", "sonnet"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := normalizeCreateModelID(c.toolID, c.in); got != c.want {
			t.Errorf("normalizeCreateModelID(%q, %q) = %q, want %q", c.toolID, c.in, got, c.want)
		}
	}
}
