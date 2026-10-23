package providers

import (
	"errors"
	"path/filepath"
	"runtime"
	"testing"
)

// TestTraeParseRollout：Trae 会话是 Codex rollout 同构格式，首行 session_meta，
// 后随 event_msg / TRAE 独有的 history_mutation（解析器须跳过未知类型）。
func TestTraeParseRollout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout-tr-1.jsonl")
	content := `{"timestamp":"2026-10-20T09:00:00.000Z","type":"session_meta","payload":{"session_id":"tr-1","cwd":"d:\\ws\\demo"}}` + "\n" +
		`{"timestamp":"2026-10-20T09:00:01.000Z","type":"history_mutation","payload":{"kind":"tool_call","tool":"Read"}}` + "\n" +
		`{"timestamp":"2026-10-20T09:00:02.000Z","type":"event_msg","payload":{"type":"user_message","message":"帮我修复登录超时"}}` + "\n"
	writeFile(t, path, content)

	head, err := ReadHead(path, 64*1024)
	if err != nil {
		t.Fatal(err)
	}
	got, err := (Trae{}).ParseSession(path, head)
	if err != nil {
		t.Fatalf("ParseSession error: %v", err)
	}
	if got.ID != "tr-1" {
		t.Fatalf("id = %q", got.ID)
	}
	if got.Workspace != `d:\ws\demo` {
		t.Fatalf("workspace = %q", got.Workspace)
	}
	if got.Title != "帮我修复登录超时" {
		t.Fatalf("title = %q", got.Title)
	}
	if got.ToolID != "trae" {
		t.Fatalf("tool = %q", got.ToolID)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatalf("timestamp not parsed")
	}
}

func TestTraeParseWithoutMeta(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no-meta.jsonl")
	writeFile(t, path, `{"timestamp":"2026-10-20T09:00:00.000Z","type":"event_msg","payload":{"type":"task_started"}}`+"\n")

	head, err := ReadHead(path, 64*1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (Trae{}).ParseSession(path, head); err == nil {
		t.Fatal("session_meta missing should be an error")
	}
}

func TestTraeSessionRoots(t *testing.T) {
	home := t.TempDir()
	roots := (Trae{}).SessionRoots(home)
	want := filepath.Join(home, ".trae", "cli", "sessions")
	if len(roots) != 1 || roots[0] != want {
		t.Fatalf("roots = %v, want [%s]", roots, want)
	}
}

func TestTraeMatchSessionRel(t *testing.T) {
	cases := []struct {
		rel  string
		want bool
	}{
		{"2026/10/20/rollout-tr-1.jsonl", true},
		{"2026/10/20/rollout-tr-1.jsonl.zst", false}, // 压缩产物
		{"2026/rollout-tr-1.jsonl", false},           // 层次不对
		{"2026/10/rollout-tr-1.jsonl", false},
		{"2026/10/20/extra/rollout-x.jsonl", false},
	}
	for _, tc := range cases {
		if got := (Trae{}).MatchSessionRel(tc.rel); got != tc.want {
			t.Fatalf("MatchSessionRel(%q) = %v, want %v", tc.rel, got, tc.want)
		}
	}
}

func TestTraeResumeCmd(t *testing.T) {
	launch := (Trae{}).ResumeCmd(Session{ID: "tr-9", Workspace: `d:\ws`}, "traecli")
	if len(launch.Args) != 2 || launch.Args[0] != "resume" || launch.Args[1] != "tr-9" {
		t.Fatalf("args = %v, want [resume tr-9]", launch.Args)
	}
	if launch.Dir != `d:\ws` {
		t.Fatalf("dir = %q", launch.Dir)
	}
}

func TestTraeNewSessionCmd(t *testing.T) {
	launch := (Trae{}).NewSessionCmd(`d:\ws`, "traecli")
	if launch.Path != "traecli" || launch.Dir != `d:\ws` || len(launch.Args) != 0 {
		t.Fatalf("launch = %+v", launch)
	}
}

func TestTraeACPAdapter(t *testing.T) {
	// stdio server 是 acp serve（两层子命令，`traecli acp` 只打 usage），
	// 0.211.1 实测握手通过；适配器与 CLI 同二进制，无 npx 兜底。
	a := (Trae{}).ACPAdapter()
	if len(a.BinNames) != 2 || a.BinNames[0] != "traecli" || a.BinNames[1] != "traex" {
		t.Fatalf("bin names = %v", a.BinNames)
	}
	if len(a.ExtraArgs) != 2 || a.ExtraArgs[0] != "acp" || a.ExtraArgs[1] != "serve" {
		t.Fatalf("extra args = %v, want [acp serve]", a.ExtraArgs)
	}
	if a.NPMPackage != "" {
		t.Fatalf("want no npm package, got %q", a.NPMPackage)
	}
}

func TestTraeLoginCmd(t *testing.T) {
	cmd := (Trae{}).LoginCmd()
	if len(cmd) != 2 || cmd[0] != "traecli" || cmd[1] != "login" {
		t.Fatalf("LoginCmd = %v", cmd)
	}
}

// TestTraeProbeAuth 表驱动：login status 的输出/退出码组合 → 三态结论。
// 实测锚点（0.211.1 未登录）：exit 非 0 + "Not logged in"。
func TestTraeProbeAuth(t *testing.T) {
	cases := []struct {
		name string
		out  string
		err  error
		want AuthStatus
	}{
		{"已登录", "Logged in as user@corp (SSO)\n", nil, AuthIn},
		{"未登录（实测形态）", "Not logged in\n", errors.New("exit 1"), AuthOut},
		{"未登录输出但大小写不同", "NOT LOGGED IN", errors.New("exit 1"), AuthOut},
		{"找不到 bin", "", errNoTraeBin, AuthUnknown},
		{"超时（无标识输出）", "", errors.New("signal: killed"), AuthUnknown},
		{"exit 0 却说未登录（形态异常）", "not logged in", nil, AuthUnknown},
		{"崩溃输出", "panic: boom", errors.New("exit 2"), AuthUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := probeTraeAuth(func() (string, error) { return tc.out, tc.err })
			if got != tc.want {
				t.Fatalf("probeTraeAuth = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTraeDetectSpecInstallDirs(t *testing.T) {
	spec := (Trae{}).DetectSpec(`C:\home`)
	if spec.BinName != "traecli" {
		t.Fatalf("BinName = %q", spec.BinName)
	}
	if len(spec.AltBinNames) != 1 || spec.AltBinNames[0] != "traex" {
		t.Fatalf("AltBinNames = %v", spec.AltBinNames)
	}
	if runtime.GOOS == "windows" {
		if len(spec.InstallDirs) != 1 || spec.InstallDirs[0] != `%LOCALAPPDATA%\Programs\TraeCLI\bin` {
			t.Fatalf("InstallDirs = %v", spec.InstallDirs)
		}
	}
}
