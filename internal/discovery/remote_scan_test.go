package discovery

import (
	"context"
	"fmt"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/remote"
	remotefs "github.com/yangk/kshell/internal/remote/fs"
)

// fakeClaude 最小文件型 provider：SessionRoots + ParseSession，供远端扫描 TDD。
type fakeClaude struct{}

func (fakeClaude) ID() string          { return "claude" }
func (fakeClaude) DisplayName() string { return "Claude" }
func (fakeClaude) DetectSpec(string) providers.DetectSpec {
	return providers.DetectSpec{BinName: "claude"}
}
func (fakeClaude) SessionRoots(home string) []string {
	return []string{path.Join(home, ".claude", "projects")}
}
func (fakeClaude) SessionFilePattern() string { return "*.jsonl" }
func (fakeClaude) MatchSessionRel(rel string) bool {
	rel = strings.TrimPrefix(rel, "./")
	return len(strings.Split(rel, "/")) == 2 && strings.HasSuffix(rel, ".jsonl")
}
func (fakeClaude) ParseSession(p string, head []byte) (*providers.Session, error) {
	cwd := ""
	id := ""
	for _, line := range strings.Split(string(head), "\n") {
		if strings.Contains(line, `"cwd"`) {
			// 极简提取：测试夹具固定格式
			if i := strings.Index(line, `"cwd":"`); i >= 0 {
				rest := line[i+7:]
				if j := strings.Index(rest, `"`); j >= 0 {
					cwd = rest[:j]
				}
			}
		}
		if strings.Contains(line, `"sessionId"`) {
			if i := strings.Index(line, `"sessionId":"`); i >= 0 {
				rest := line[i+13:]
				if j := strings.Index(rest, `"`); j >= 0 {
					id = rest[:j]
				}
			}
		}
	}
	if id == "" {
		return nil, fmt.Errorf("no id")
	}
	return &providers.Session{
		ID: id, ToolID: "claude", Workspace: cwd, Title: "t",
		Path: p, UpdatedAt: time.Unix(1700000000, 0),
	}, nil
}
func (fakeClaude) NewSessionCmd(string, string) providers.Launch { return providers.Launch{} }
func (fakeClaude) ResumeCmd(providers.Session, string) providers.Launch {
	return providers.Launch{}
}

func claudeHead(cwd, id string) string {
	return fmt.Sprintf(
		`{"type":"user","cwd":"%s","sessionId":"%s","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"hi"}}`+"\n",
		cwd, id,
	)
}

func TestScanRemoteSessionsFiltersByWorkspace(t *testing.T) {
	const remoteHome = "/home/u"
	const wantWS = "/home/u/proj"
	const otherWS = "/home/u/other"
	matchPath := "/home/u/.claude/projects/slug/match.jsonl"
	otherPath := "/home/u/.claude/projects/slug/other.jsonl"
	heads := map[string]string{
		matchPath: claudeHead(wantWS, "sess-match"),
		otherPath: claudeHead(otherWS, "sess-other"),
	}

	run := func(_ context.Context, _ remote.Connection, cmd string, _ []byte) ([]byte, []byte, error) {
		if strings.Contains(cmd, `echo -n "$HOME"`) {
			return []byte(remoteHome), nil, nil
		}
		if strings.Contains(cmd, "find") && strings.Contains(cmd, ".claude/projects") {
			var b strings.Builder
			for p := range heads {
				fmt.Fprintf(&b, "12\t1700000000.0\t%s\n", p)
			}
			return []byte(b.String()), nil, nil
		}
		if strings.Contains(cmd, "head -c") {
			for p, h := range heads {
				if strings.Contains(cmd, p) {
					return []byte(h), nil, nil
				}
			}
			return nil, nil, fmt.Errorf("missing head")
		}
		return nil, nil, fmt.Errorf("unexpected cmd: %s", cmd)
	}

	got, err := ScanRemoteSessions(
		context.Background(),
		remote.Connection{ID: "c1"},
		ScanOptions{},
		[]providers.Provider{fakeClaude{}},
		wantWS,
		run,
		"",
	)
	if err != nil {
		t.Fatalf("ScanRemoteSessions: %v", err)
	}
	if len(got.Sessions) != 1 || got.Sessions[0].ID != "sess-match" {
		t.Fatalf("got %+v, want only sess-match", got)
	}
	if NormalizeRemotePath(got.Sessions[0].Workspace) != NormalizeRemotePath(wantWS) {
		t.Fatalf("Workspace=%q", got.Sessions[0].Workspace)
	}
}

func TestScanRemoteSessionsNormalizesWorkspaceFilter(t *testing.T) {
	const remoteHome = "/home/u"
	filePath := "/home/u/.claude/projects/slug/a.jsonl"
	head := claudeHead("/home/u/proj/./", "s1")

	run := func(_ context.Context, _ remote.Connection, cmd string, _ []byte) ([]byte, []byte, error) {
		switch {
		case strings.Contains(cmd, `echo -n "$HOME"`):
			return []byte(remoteHome), nil, nil
		case strings.Contains(cmd, "find"):
			return []byte("10\t1700000000.0\t" + filePath + "\n"), nil, nil
		case strings.Contains(cmd, "head -c"):
			return []byte(head), nil, nil
		default:
			return nil, nil, fmt.Errorf("unexpected: %s", cmd)
		}
	}

	got, err := ScanRemoteSessions(
		context.Background(),
		remote.Connection{ID: "c1"},
		ScanOptions{},
		[]providers.Provider{fakeClaude{}},
		"/home/u/../u/proj//",
		run,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sessions) != 1 {
		t.Fatalf("got %d sessions", len(got.Sessions))
	}
}

func TestScanRemoteSessionsSkipsSubagentDepth(t *testing.T) {
	const remoteHome = "/home/u"
	good := "/home/u/.claude/projects/slug/good.jsonl"
	bad := "/home/u/.claude/projects/slug/sid/subagents/agent.jsonl"

	run := func(_ context.Context, _ remote.Connection, cmd string, _ []byte) ([]byte, []byte, error) {
		if strings.Contains(cmd, `echo -n "$HOME"`) {
			return []byte(remoteHome), nil, nil
		}
		if strings.Contains(cmd, "find") {
			return []byte(
				"10\t1700000000.0\t" + good + "\n" +
					"10\t1700000000.0\t" + bad + "\n",
			), nil, nil
		}
		if strings.Contains(cmd, "head -c") && strings.Contains(cmd, good) {
			return []byte(claudeHead("/home/u/proj", "good")), nil, nil
		}
		if strings.Contains(cmd, bad) {
			t.Fatal("不应读取子代理文件头")
		}
		return nil, nil, fmt.Errorf("unexpected: %s", cmd)
	}

	got, err := ScanRemoteSessions(
		context.Background(),
		remote.Connection{ID: "c1"},
		ScanOptions{},
		[]providers.Provider{fakeClaude{}},
		"/home/u/proj",
		run,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sessions) != 1 || got.Sessions[0].ID != "good" {
		t.Fatalf("got %+v", got)
	}
}

func TestScanRemoteSessionsHomeFailure(t *testing.T) {
	run := func(_ context.Context, _ remote.Connection, cmd string, _ []byte) ([]byte, []byte, error) {
		return nil, []byte("boom"), fmt.Errorf("err.ssh.exec_failed")
	}
	_, err := ScanRemoteSessions(
		context.Background(),
		remote.Connection{ID: "c1"},
		ScanOptions{},
		[]providers.Provider{fakeClaude{}},
		"/home/u/proj",
		run,
		"",
	)
	if err == nil {
		t.Fatal("期望错误")
	}
}

func TestScanRemoteSessionsOpencodeEnumerator(t *testing.T) {
	const remoteHome = "/home/u"
	const wantWS = "/home/u/proj"
	jsonOut := `[{"id":"oc1","cwd":"/home/u/proj","title":"远程 opencode","created":1700000000000,"updated":1700000001000}]`

	run := func(_ context.Context, _ remote.Connection, cmd string, _ []byte) ([]byte, []byte, error) {
		switch {
		case strings.Contains(cmd, `echo -n "$HOME"`):
			return []byte(remoteHome), nil, nil
		case strings.Contains(cmd, "command -v") && strings.Contains(cmd, "opencode"):
			return []byte("/usr/bin/opencode"), nil, nil
		case strings.Contains(cmd, "opencode") && strings.Contains(cmd, "--format json"):
			return []byte(jsonOut), nil, nil
		case strings.Contains(cmd, "find"):
			return nil, nil, nil // 无文件型会话
		default:
			return nil, nil, fmt.Errorf("unexpected: %s", cmd)
		}
	}

	got, err := ScanRemoteSessions(
		context.Background(),
		remote.Connection{ID: "c1"},
		ScanOptions{},
		[]providers.Provider{providers.Opencode{}},
		wantWS,
		run,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sessions) != 1 || got.Sessions[0].ID != "oc1" {
		t.Fatalf("got %+v", got)
	}
}

// 确认 Runner 类型与 remotefs 对齐，避免桌面层再包一层签名。
var _ remotefs.Runner = (RemoteRunner)(nil)

// TestRemoteListSessionFilesBSDFindFallback 覆盖 macOS/BSD 静默失败根因：
// find -printf 是 GNU 扩展，BSD find 会 stderr 报「unknown option」并空输出
//（`|| true` 又吞掉非零退出）。此时必须回退兼容方案拿到文件列表，而非返回空。
func TestRemoteListSessionFilesBSDFindFallback(t *testing.T) {
	const root = "/home/u/.claude/projects"
	run := func(_ context.Context, _ remote.Connection, cmd string, _ []byte) ([]byte, []byte, error) {
		if strings.Contains(cmd, "-printf") {
			// 模拟 BSD find：报错 + 空输出 + 退出码 1（被 || true 吞掉）
			return nil, []byte("find: -printf: unknown option"), nil
		}
		// 回退命令：`find ... -name ...`（无 -printf，POSIX 兼容）
		if strings.Contains(cmd, "find") {
			return []byte(root + "/slug/match.jsonl\n" + root + "/slug/other.jsonl\n"), nil, nil
		}
		return nil, nil, fmt.Errorf("unexpected cmd: %s", cmd)
	}
	got, err := remoteListSessionFiles(context.Background(), remote.Connection{ID: "c1"}, run, root, "*.jsonl")
	if err != nil {
		t.Fatalf("BSD find 应回退兼容方案, got err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v, want 2 files", got)
	}
	if got[0].path != root+"/slug/match.jsonl" {
		t.Fatalf("path = %q", got[0].path)
	}
}

// TestRemoteListSessionFilesGNUFindUnchanged 保证 GNU find 主路径不回归：
// -printf 输出（size/mtime/path 三段）照常解析。
func TestRemoteListSessionFilesGNUFindUnchanged(t *testing.T) {
	run := func(_ context.Context, _ remote.Connection, cmd string, _ []byte) ([]byte, []byte, error) {
		if strings.Contains(cmd, "-printf") {
			return []byte("12\t1700000000.0\t/home/u/.claude/projects/slug/a.jsonl\n"), nil, nil
		}
		return nil, nil, fmt.Errorf("unexpected cmd: %s", cmd)
	}
	got, err := remoteListSessionFiles(context.Background(), remote.Connection{ID: "c1"}, run, "/home/u/.claude/projects", "*.jsonl")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got[0].size != 12 || got[0].mtime == 0 {
		t.Fatalf("got %+v", got)
	}
}

// TestScanRemoteSessionsReturnsFailed 覆盖「失败不可见」：解析/列举失败必须
// 经返回值传给调用方（与本地 Scan 的 Result.Failed 口径对齐），不能整体丢弃。
func TestScanRemoteSessionsReturnsFailed(t *testing.T) {
	const remoteHome = "/home/u"
	filePath := "/home/u/.claude/projects/slug/a.jsonl"

	run := func(_ context.Context, _ remote.Connection, cmd string, _ []byte) ([]byte, []byte, error) {
		switch {
		case strings.Contains(cmd, `echo -n "$HOME"`):
			return []byte(remoteHome), nil, nil
		case strings.Contains(cmd, "find"):
			return []byte("10\t1700000000.0\t" + filePath + "\n"), nil, nil
		case strings.Contains(cmd, "head -c"):
			// 会话头损坏：ParseSession 失败 → 应计入 Failed
			return []byte("not json\n"), nil, nil
		default:
			return nil, nil, fmt.Errorf("unexpected: %s", cmd)
		}
	}

	res, err := ScanRemoteSessions(
		context.Background(),
		remote.Connection{ID: "c1"},
		ScanOptions{},
		[]providers.Provider{fakeClaude{}},
		"/home/u/proj",
		run,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Failed) == 0 {
		t.Fatalf("解析失败应出现在 Failed 里, got %+v", res)
	}
	if !strings.Contains(res.Failed[0], filePath) {
		t.Fatalf("Failed[0] = %q, 应含失败文件路径", res.Failed[0])
	}
}
