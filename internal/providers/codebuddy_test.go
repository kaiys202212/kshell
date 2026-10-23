package providers

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCodeBuddyIdentityAndDetect(t *testing.T) {
	p := CodeBuddy{}
	if p.ID() != "codebuddy" || p.DisplayName() != "CodeBuddy" {
		t.Fatalf("id/name = %s %s", p.ID(), p.DisplayName())
	}
	spec := p.DetectSpec("")
	if spec.BinName != "codebuddy" {
		t.Fatalf("bin = %q", spec.BinName)
	}
	if len(spec.AltBinNames) != 1 || spec.AltBinNames[0] != "cbc" {
		t.Fatalf("alt = %v", spec.AltBinNames)
	}
	if len(spec.ConfigDirs) != 1 || spec.ConfigDirs[0] != "~/.codebuddy" {
		t.Fatalf("dirs = %v", spec.ConfigDirs)
	}
}

func TestCodeBuddyResumeAndMatch(t *testing.T) {
	p := CodeBuddy{}
	launch := p.ResumeCmd(Session{ID: "cb-1", Workspace: `D:\ws`}, "codebuddy")
	if len(launch.Args) != 2 || launch.Args[0] != "--resume" || launch.Args[1] != "cb-1" {
		t.Fatalf("args = %v", launch.Args)
	}
	if !p.MatchSessionRel(`ws-1/01a0.jsonl`) {
		t.Fatal("session file must match")
	}
	if p.MatchSessionRel(`ws-1/01a0.jsonl/subagents/agent-1.jsonl`) {
		t.Fatal("subagent must not match")
	}
}

func TestCodeBuddyParsesSummaryTitle(t *testing.T) {
	content := `{"type":"session-meta","id":"m-1","sessionId":"cb-9","timestamp":1790242904766,"cwd":"D:\\ws\\demo"}` + "\n" +
		`{"id":"m-3","timestamp":1790243000000,"type":"summary","summary":"设计 RPC 任务架构","providerData":{"source":"initial-user-message"}}` + "\n"
	got, err := (CodeBuddy{}).ParseSession("cb.jsonl", []byte(content))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "cb-9" || got.Title != "设计 RPC 任务架构" {
		t.Fatalf("got %+v", got)
	}
}

func TestCodeBuddyRecipeNpm(t *testing.T) {
	r, ok := RecipeOf(CodeBuddy{})
	if !ok {
		t.Fatal("missing recipe")
	}
	if !strings.Contains(r.InstallCmd, "@tencent-ai/codebuddy-code") {
		t.Fatalf("install = %q", r.InstallCmd)
	}
	if !strings.Contains(r.UninstallCmd, "@tencent-ai/codebuddy-code") {
		t.Fatalf("uninstall = %q", r.UninstallCmd)
	}
	if len(r.PurgeDirs) != 1 || r.PurgeDirs[0] != "~/.codebuddy" {
		t.Fatalf("purge = %v", r.PurgeDirs)
	}
}

func TestDefaultYAMLOmitsCodeBuddyID(t *testing.T) {
	if strings.Contains(DefaultProvidersYAML(), "id: codebuddy") {
		t.Fatal("default yaml must not declare builtin codebuddy")
	}
	if !strings.Contains(DefaultProvidersYAML(), "id: cline") {
		t.Fatal("cline preset must remain")
	}
}

func TestMergeProvidersPrefersBuiltinCodeBuddy(t *testing.T) {
	specs := []GenericSpec{{ID: "codebuddy", Name: "YAML 覆盖名"}}
	ps := MergeProviders(Builtins(), specs, `C:\home`)
	var p Provider
	for _, x := range ps {
		if x.ID() == "codebuddy" {
			p = x
		}
	}
	if p == nil {
		t.Fatal("missing codebuddy")
	}
	if _, ok := p.(CodeBuddy); !ok {
		t.Fatalf("got %T", p)
	}
	if p.DisplayName() != "CodeBuddy" {
		t.Fatalf("name = %q", p.DisplayName())
	}
}

func TestCodeBuddyLoginCmd(t *testing.T) {
	// 裸 codebuddy：无官方 login 子命令，交互式 TUI 启动即触发登录引导。
	cmd := (CodeBuddy{}).LoginCmd()
	if len(cmd) != 1 || cmd[0] != "codebuddy" {
		t.Fatalf("LoginCmd = %v", cmd)
	}
}

// TestParseCodeBuddyAuthMethods 表驱动：initialize 响应行的解析。
// 实测锚点（2.172.1 未登录）：authMethods = iOA/external/internal/selfhosted。
func TestParseCodeBuddyAuthMethods(t *testing.T) {
	cases := []struct {
		name string
		line string
		wantN int
		wantOK bool
	}{
		{"未登录（实测形态）", `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"agentCapabilities":{},"authMethods":[{"id":"iOA","name":"Login with iOA"},{"id":"internal","name":"Login with WeChat"}]}}`, 2, true},
		{"已登录（不广播 authMethods）", `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}`, 0, true},
		{"authMethods 显式空数组", `{"jsonrpc":"2.0","id":1,"result":{"authMethods":[]}}`, 0, true},
		{"非 id=1 的其他响应", `{"jsonrpc":"2.0","id":2,"result":{}}`, 0, false},
		{"请求回调行（无 id 字段）", `{"jsonrpc":"2.0","method":"session/request_permission","params":{}}`, 0, false},
		{"坏 JSON", "not-json\n", 0, false},
		{"空行", "", 0, false},
	}
	for _, c := range cases {
		n, ok := parseCodeBuddyAuthMethods(c.line)
		if n != c.wantN || ok != c.wantOK {
			t.Fatalf("%s: parse = (%d,%v), want (%d,%v)", c.name, n, ok, c.wantN, c.wantOK)
		}
	}
}

// TestProbeCodeBuddyAuth 表驱动：run 的返回组合 → 三态结论。
func TestProbeCodeBuddyAuth(t *testing.T) {
	cases := []struct {
		name string
		line string
		err  error
		want AuthStatus
	}{
		{"未登录（authMethods 非空）", `{"jsonrpc":"2.0","id":1,"result":{"authMethods":[{"id":"iOA"}]}}`, nil, AuthOut},
		{"已登录（无 authMethods）", `{"jsonrpc":"2.0","id":1,"result":{"agentCapabilities":{}}}`, nil, AuthIn},
		{"找不到 bin", "", errNoCodeBuddyBin, AuthUnknown},
		{"子进程超时", "", errors.New("signal: killed"), AuthUnknown},
		{"无响应（EOF）", "", nil, AuthUnknown},
		{"响应形态异常（非 id=1）", `{"jsonrpc":"2.0","id":9,"result":{}}`, nil, AuthUnknown},
	}
	for _, c := range cases {
		got := probeCodeBuddyAuth(func() (string, error) { return c.line, c.err })
		if got != c.want {
			t.Fatalf("%s: probe = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestCodeBuddyAuthCacheTTL 验证 ProbeAuth 走 TTL 缓存：未过期不重复实探，
// 过期后重新实探并刷新缓存。缓存的过期时刻不可注入时钟，用「直接把缓存过期时刻
// 拨到过去/未来」的方式驱动两种分支。
func TestCodeBuddyAuthCacheTTL(t *testing.T) {
	calls := 0
	origRun := codeBuddyAuthRun
	t.Cleanup(func() {
		codeBuddyAuthRun = origRun
		codeBuddyAuthMu.Lock()
		codeBuddyAuthCache = codeBuddyAuthCacheEntry{}
		codeBuddyAuthMu.Unlock()
	})
	codeBuddyAuthRun = func() (string, error) {
		calls++
		return `{"jsonrpc":"2.0","id":1,"result":{"authMethods":[{"id":"iOA"}]}}`, nil
	}
	// 重置缓存后首探：实探 1 次，结论 out 并落缓存。
	codeBuddyAuthMu.Lock()
	codeBuddyAuthCache = codeBuddyAuthCacheEntry{}
	codeBuddyAuthMu.Unlock()
	if got := (CodeBuddy{}).ProbeAuth(); got != AuthOut {
		t.Fatalf("首探 = %q, want out", got)
	}
	if calls != 1 {
		t.Fatalf("首探后实探次数 = %d, want 1", calls)
	}
	// 缓存命中：不再实探，结论仍是 out。
	if got := (CodeBuddy{}).ProbeAuth(); got != AuthOut {
		t.Fatalf("缓存内 = %q, want out", got)
	}
	if calls != 1 {
		t.Fatalf("缓存命中后实探次数 = %d, want 1", calls)
	}
	// 把缓存拨过期：重新实探，结论翻转 in（换一次桩输出）并落新缓存。
	codeBuddyAuthRun = func() (string, error) {
		calls++
		return `{"jsonrpc":"2.0","id":1,"result":{}}`, nil
	}
	codeBuddyAuthMu.Lock()
	codeBuddyAuthCache.expires = time.Now().Add(-time.Second)
	codeBuddyAuthMu.Unlock()
	if got := (CodeBuddy{}).ProbeAuth(); got != AuthIn {
		t.Fatalf("过期重探 = %q, want in", got)
	}
	if calls != 2 {
		t.Fatalf("过期重探次数 = %d, want 2", calls)
	}
	// 探测失败（unknown）也落缓存：unknown 同样吃 TTL，避免弹窗轮询空转打桩失败。
	codeBuddyAuthRun = func() (string, error) { return "", errors.New("boom") }
	codeBuddyAuthMu.Lock()
	codeBuddyAuthCache.expires = time.Now().Add(-time.Second)
	codeBuddyAuthMu.Unlock()
	if got := (CodeBuddy{}).ProbeAuth(); got != AuthUnknown {
		t.Fatalf("失败重探 = %q, want unknown", got)
	}
}
