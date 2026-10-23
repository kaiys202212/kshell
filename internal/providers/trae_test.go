package providers

import (
	"path/filepath"
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
	// CLI 自带 acp 子命令（stdio），适配器与 CLI 同二进制，无 npx 兜底。
	a := (Trae{}).ACPAdapter()
	if len(a.BinNames) != 2 || a.BinNames[0] != "traecli" || a.BinNames[1] != "traex" {
		t.Fatalf("bin names = %v", a.BinNames)
	}
	if len(a.ExtraArgs) != 1 || a.ExtraArgs[0] != "acp" {
		t.Fatalf("extra args = %v", a.ExtraArgs)
	}
	if a.NPMPackage != "" {
		t.Fatalf("want no npm package, got %q", a.NPMPackage)
	}
}
