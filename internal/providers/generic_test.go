package providers

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const genericYAML = `
providers:
  - id: codebuddy
    name: CodeBuddy
    detect:
      command: codebuddy
      dirs:
        - ~/.codebuddy
    sessions:
      glob: ~/.codebuddy/projects/*/*.jsonl
      format: jsonl
    fields:
      cwd: cwd
      id: sessionId
      timestamp: timestamp
      title: message.content
    resume:
      args: ["--resume", "{id}"]
    verified: false
`

// codebuddyYAML 与 DefaultProvidersYAML 里的 codebuddy 预置保持同构（实测字段）。
const codebuddyYAML = `
providers:
  - id: codebuddy
    name: CodeBuddy
    sessions:
      glob: ~/.codebuddy/projects/*/*.jsonl
      format: jsonl
    fields:
      cwd: cwd
      id: sessionId
      timestamp: timestamp
      title: summary
      titleFallbacks: ["aiTitle"]
    resume:
      args: ["--resume", "{id}"]
    verified: true
`

func TestLoadGenericSpecs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.yaml")
	if err := os.WriteFile(path, []byte(genericYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	specs, err := LoadGenericSpecs(path)
	if err != nil {
		t.Fatalf("LoadGenericSpecs error: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("specs = %d, want 1", len(specs))
	}
	if specs[0].ID != "codebuddy" || specs[0].Sessions.Glob != "~/.codebuddy/projects/*/*.jsonl" {
		t.Fatalf("spec = %+v", specs[0])
	}
	if specs[0].Verified {
		t.Fatal("preset must be marked unverified")
	}
}

func TestGenericProviderRootsAndPattern(t *testing.T) {
	home := t.TempDir()
	var spec GenericSpec
	if err := yamlUnmarshalHelper(genericYAML, &spec); err != nil {
		t.Fatal(err)
	}
	g := Generic{Spec: spec, Home: home}

	roots := g.SessionRoots(home)
	want := filepath.Join(home, ".codebuddy", "projects")
	if len(roots) != 1 || roots[0] != want {
		t.Fatalf("roots = %v, want [%s]", roots, want)
	}
	if got := g.SessionFilePattern(); got != "*.jsonl" {
		t.Fatalf("pattern = %q", got)
	}
}

func TestGenericProviderParsesFixture(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codebuddy", "projects", "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "sess.jsonl")
	content := `{"type":"user","cwd":"D:\\data\\workspace\\demo","sessionId":"cb-1","timestamp":"2026-09-20T10:00:00Z","message":{"content":"看一下这个工程"}}` + "\n" +
		`{"type":"assistant","cwd":"D:\\data\\workspace\\demo","sessionId":"cb-1","timestamp":"2026-09-20T10:05:00Z","message":{"content":"好的"}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	var spec GenericSpec
	if err := yamlUnmarshalHelper(genericYAML, &spec); err != nil {
		t.Fatal(err)
	}
	g := Generic{Spec: spec, Home: home}

	head, err := ReadHead(path, 64*1024)
	if err != nil {
		t.Fatal(err)
	}
	got, err := g.ParseSession(path, head)
	if err != nil {
		t.Fatalf("ParseSession error: %v", err)
	}
	if got.ID != "cb-1" {
		t.Fatalf("id = %q", got.ID)
	}
	if got.Workspace != `D:\data\workspace\demo` {
		t.Fatalf("workspace = %q", got.Workspace)
	}
	if got.Title != "看一下这个工程" {
		t.Fatalf("title = %q", got.Title)
	}
	if got.Messages != 2 {
		t.Fatalf("messages = %d, want 2", got.Messages)
	}
	if !got.UpdatedAt.After(got.CreatedAt) {
		t.Fatalf("timestamps: %v ~ %v", got.CreatedAt, got.UpdatedAt)
	}
}

func TestGenericResumeCmdSubstitutesID(t *testing.T) {
	var spec GenericSpec
	if err := yamlUnmarshalHelper(genericYAML, &spec); err != nil {
		t.Fatal(err)
	}
	g := Generic{Spec: spec}

	launch := g.ResumeCmd(Session{ID: "cb-1", Workspace: "D:\\ws"}, "codebuddy")
	if len(launch.Args) != 2 || launch.Args[0] != "--resume" || launch.Args[1] != "cb-1" {
		t.Fatalf("args = %v, want [--resume cb-1]", launch.Args)
	}
	if launch.Dir != "D:\\ws" {
		t.Fatalf("dir = %q", launch.Dir)
	}
}

func TestDefaultProvidersYAMLCoversPresetTools(t *testing.T) {
	content := DefaultProvidersYAML()
	for _, want := range []string{"opencode", "cline", "verified: false"} {
		if !strings.Contains(content, want) {
			t.Fatalf("default providers.yaml missing %q", want)
		}
	}
}

func TestEnsureProvidersFileWritesTemplate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "providers.yaml")
	if err := EnsureProvidersFile(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "providers:") {
		t.Fatalf("template not written: %s", data)
	}

	// 已存在时不应覆盖
	if err := os.WriteFile(path, []byte("custom"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureProvidersFile(path); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if string(data) != "custom" {
		t.Fatalf("existing file must not be overwritten, got %s", data)
	}
}

func TestRootFromGlob(t *testing.T) {
	if got := rootFromGlob("~/.a/*/x.jsonl"); got != "~/.a" {
		t.Fatalf("got %q", got)
	}
	if got := rootFromGlob("~/.a/x.jsonl"); got != "~/.a/x.jsonl" {
		t.Fatalf("got %q", got)
	}
}

func TestGenericMatchesGlobDepth(t *testing.T) {
	var spec GenericSpec
	if err := yamlUnmarshalHelper(codebuddyYAML, &spec); err != nil {
		t.Fatal(err)
	}
	g := Generic{Spec: spec, Home: `C:\Users\demo`}

	if !g.MatchSessionRel(`ws-1/01a0.jsonl`) {
		t.Fatal("one-level session file must match */*.jsonl")
	}
	if g.MatchSessionRel(`ws-1/01a0.jsonl/subagents/agent-1.jsonl`) {
		t.Fatal("deeper subagent file must not match */*.jsonl")
	}
	if g.MatchSessionRel(`01a0.jsonl`) {
		t.Fatal("root-level file must not match */*.jsonl")
	}
}

func TestGenericParsesCodeBuddyShape(t *testing.T) {
	// 与真实 CodeBuddy 会话文件同构：毫秒时间戳、summary/ai-title 记录、content 块数组、包装文本
	var spec GenericSpec
	if err := yamlUnmarshalHelper(codebuddyYAML, &spec); err != nil {
		t.Fatal(err)
	}
	g := Generic{Spec: spec, Home: t.TempDir()}

	content := `{"type":"session-meta","id":"m-1","sessionId":"cb-9","timestamp":1790242904766,"cwd":"D:\\ws\\demo"}` + "\n" +
		`{"id":"m-2","timestamp":1790242904766,"type":"message","role":"user","content":[{"type":"input_text","text":"<system-reminder>Caveat</system-reminder>"}],"sessionId":"cb-9","cwd":"D:\\ws\\demo"}` + "\n" +
		`{"id":"m-3","timestamp":1790243000000,"type":"summary","summary":"设计 RPC 任务架构","providerData":{"source":"initial-user-message"}}` + "\n" +
		`{"id":"x-1","timestamp":1790243100000,"type":"ai-title","aiTitle":"选择方案A"}` + "\n"
	got, err := g.ParseSession("cb.jsonl", []byte(content))
	if err != nil {
		t.Fatalf("ParseSession error: %v", err)
	}
	if got.ID != "cb-9" {
		t.Fatalf("id = %q", got.ID)
	}
	if got.Workspace != `D:\ws\demo` {
		t.Fatalf("workspace = %q", got.Workspace)
	}
	if got.Title != "设计 RPC 任务架构" {
		t.Fatalf("title = %q, want summary field", got.Title)
	}
	want := time.UnixMilli(1790242904766)
	if !got.CreatedAt.Equal(want) || !got.UpdatedAt.After(want) {
		t.Fatalf("timestamps = %v ~ %v, want epoch-millis parsed", got.CreatedAt, got.UpdatedAt)
	}
}

func TestGenericTitleFallsBackToUserText(t *testing.T) {
	var spec GenericSpec
	if err := yamlUnmarshalHelper(codebuddyYAML, &spec); err != nil {
		t.Fatal(err)
	}
	g := Generic{Spec: spec, Home: t.TempDir()}

	content := `{"type":"session-meta","sessionId":"cb-8","timestamp":1790242904766,"cwd":"D:\\ws"}` + "\n" +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"<command-name>/clear</command-name>"}]}` + "\n" +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"帮我检查会话识别"},{"type":"input_text","text":"第二段"}]}` + "\n"
	got, err := g.ParseSession("cb.jsonl", []byte(content))
	if err != nil {
		t.Fatalf("ParseSession error: %v", err)
	}
	if got.Title != "帮我检查会话识别 第二段" {
		t.Fatalf("title = %q, want first real user message", got.Title)
	}
}

func TestGenericTitleFallsBackToAssistantText(t *testing.T) {
	var spec GenericSpec
	if err := yamlUnmarshalHelper(codebuddyYAML, &spec); err != nil {
		t.Fatal(err)
	}
	g := Generic{Spec: spec, Home: t.TempDir()}

	// 用户消息全是包装记录，但有 assistant 回复：用首条 assistant 文本当标题
	content := `{"type":"session-meta","sessionId":"cb-7","timestamp":1790242904766,"cwd":"D:\\ws"}` + "\n" +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"<external_links>web results"}]}` + "\n" +
		`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"先读 Task 10 简报和相关代码"}]}` + "\n"
	got, err := g.ParseSession("cb.jsonl", []byte(content))
	if err != nil {
		t.Fatalf("ParseSession error: %v", err)
	}
	if got.Title != "先读 Task 10 简报和相关代码" {
		t.Fatalf("title = %q, want first assistant text", got.Title)
	}
}

// CodeBuddy/Codex 的导入会话把用户问题包在 <user_query> 里：
// 剥掉标记后剩下的才是真标题，不该因为「以 < 开头」被整条跳过。
func TestGenericTitleUnwrapsUserQuery(t *testing.T) {
	var spec GenericSpec
	if err := yamlUnmarshalHelper(codebuddyYAML, &spec); err != nil {
		t.Fatal(err)
	}
	g := Generic{Spec: spec, Home: t.TempDir()}

	content := `{"type":"session-meta","sessionId":"cb-9","timestamp":1790242904766,"cwd":"D:\\ws"}` + "\n" +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"<user_info>ctx</user_info><user_query>继续对界面进行美化</user_query>"}]}` + "\n"
	got, err := g.ParseSession("cb.jsonl", []byte(content))
	if err != nil {
		t.Fatalf("ParseSession error: %v", err)
	}
	if got.Title != "继续对界面进行美化" {
		t.Fatalf("title = %q, want unwrapped user query", got.Title)
	}
}

func TestGenericDropsCommandOnlyStub(t *testing.T) {
	var spec GenericSpec
	if err := yamlUnmarshalHelper(codebuddyYAML, &spec); err != nil {
		t.Fatal(err)
	}
	g := Generic{Spec: spec, Home: t.TempDir()}

	// 只有 /clear、change session 等命令记录的空壳：应整体剔除
	content := `{"type":"session-meta","sessionId":"cb-6","timestamp":1790242904766,"cwd":"D:\\ws"}` + "\n" +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"<command-name>/clear</command-name>"}]}` + "\n" +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"<local-command-stdout>change session x"}]}` + "\n"
	if _, err := g.ParseSession("cb.jsonl", []byte(content)); err != errGenericEmpty {
		t.Fatalf("err = %v, want errGenericEmpty", err)
	}
}

func TestFormatProvidersYAMLRoundTrip(t *testing.T) {
	in := []GenericSpec{{ID: "demo", Name: "Demo"}}
	in[0].Detect.Command = "demo"
	raw, err := FormatProvidersYAML(in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseProvidersYAML([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "demo" || got[0].Name != "Demo" || got[0].Detect.Command != "demo" {
		t.Fatalf("got %+v yaml=%s", got, raw)
	}
}

func TestGenericBypassArgs(t *testing.T) {
	specs, err := ParseProvidersYAML([]byte(`providers:
  - id: mytool
    name: MyTool
    permission:
      bypassArgs: ["--trust-all"]
`))
	if err != nil || len(specs) != 1 {
		t.Fatalf("parse: %v %v", specs, err)
	}
	g := Generic{Spec: specs[0]}
	args, env := g.InjectPermission(true)
	if !reflect.DeepEqual(args, []string{"--trust-all"}) || len(env) != 0 {
		t.Fatalf("generic bypass = %v %v", args, env)
	}
	// 未声明时保持 no-op
	if a, e := (Generic{}).InjectPermission(true); len(a) != 0 || len(e) != 0 {
		t.Fatalf("未声明应 no-op，got %v %v", a, e)
	}
	// 默认（非 bypass）模式不注入
	if a, e := g.InjectPermission(false); len(a) != 0 || len(e) != 0 {
		t.Fatalf("default 不应注入，got %v %v", a, e)
	}
}

func TestGenericBypassArgsFormatRoundtrip(t *testing.T) {
	// 未声明 permission 的项不应输出空占位块
	plain, err := FormatProvidersYAML([]GenericSpec{{ID: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain, "permission") {
		t.Fatalf("空 permission 不应输出：\n%s", plain)
	}
	specs := []GenericSpec{{ID: "mytool"}}
	specs[0].Permission.BypassArgs = []string{"--trust-all"}
	out, err := FormatProvidersYAML(specs)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "permission:") && strings.Contains(out, "bypassArgs: []") {
		t.Fatalf("空 permission 不应输出占位：\n%s", out)
	}
	back, err := ParseProvidersYAML([]byte(out))
	if err != nil || len(back) != 1 || !reflect.DeepEqual(back[0].Permission.BypassArgs, []string{"--trust-all"}) {
		t.Fatalf("roundtrip = %+v %v yaml=%s", back, err, out)
	}
}
