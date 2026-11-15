package desktop

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/clisync"
)

// clisyncApp 构造带注入 PATH 实现与假二进制的 App。
func clisyncApp(t *testing.T) (*App, *[]string) {
	t.Helper()
	calls := &[]string{}
	home := t.TempDir()
	app := NewAppWith(Options{
		Home:      home,
		CLIBinary: []byte("MZ-fake-cli"),
		ClisyncEnsurePath: func(binDir string) (string, error) {
			*calls = append(*calls, binDir)
			return "added", nil
		},
		ClisyncPathEntries: func() []string {
			return []string{clisync.BinDir(home)}
		},
	})
	return app, calls
}

// skillStep 按宿主取状态，找不到返回零值。
func skillStep(res clisync.Result, toolID string) clisync.SkillStep {
	for _, s := range res.Skills {
		if s.ToolID == toolID {
			return s
		}
	}
	return clisync.SkillStep{}
}

// SyncHosts 全量同步：释放二进制 + PATH + 指定宿主技能，单宿主错误进结果不中断。
func TestSyncHosts(t *testing.T) {
	app, calls := clisyncApp(t)
	res, err := app.SyncHosts([]string{"codebuddy", "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if res.BinaryState != "released" {
		t.Fatalf("BinaryState = %q (%s)", res.BinaryState, res.BinaryErr)
	}
	if _, err := os.Stat(res.BinaryPath); err != nil {
		t.Fatalf("二进制未落盘: %v", err)
	}
	if res.PathState != "added" || len(*calls) != 1 {
		t.Fatalf("PathState = %q calls = %v", res.PathState, *calls)
	}
	// 两个宿主各自落盘 SKILL.md。
	for _, id := range []string{"codebuddy", "claude"} {
		step := skillStep(res, id)
		if step.State != "installed" {
			t.Fatalf("%s State = %q (%s)", id, step.State, step.Err)
		}
		raw, err := os.ReadFile(step.Path)
		if err != nil || len(raw) == 0 {
			t.Fatalf("%s SKILL.md 未写入: %v", id, err)
		}
	}
	if skillStep(res, "codebuddy").Path != filepath.Join(app.opts.Home, ".codebuddy", "skills", "kshell", "SKILL.md") {
		t.Fatalf("codebuddy 路径口径不符: %s", skillStep(res, "codebuddy").Path)
	}
}

// GetHostSyncStatuses 只读，覆盖全部已知宿主，不写盘、不触发 PATH 配置。
func TestGetHostSyncStatuses(t *testing.T) {
	app, calls := clisyncApp(t)
	st := app.GetHostSyncStatuses()
	if st.BinaryState != "absent" || len(st.Skills) == 0 {
		t.Fatalf("初始状态: %+v", st)
	}
	if len(*calls) != 0 {
		t.Fatal("Status 不得触发 PATH 配置")
	}
	if _, err := app.SyncHosts([]string{"codebuddy"}); err != nil {
		t.Fatal(err)
	}
	st = app.GetHostSyncStatuses()
	if st.BinaryState != "present" || st.PathState != "ready" {
		t.Fatalf("同步后状态: %+v", st)
	}
	if skillStep(st, "codebuddy").State != "present" || skillStep(st, "claude").State != "absent" {
		t.Fatalf("同步后按宿主状态不符: %+v", st.Skills)
	}
}

// SyncHosts 在未内嵌二进制（dev 构建）时二进制步跳过，指定宿主的技能照常。
func TestSyncHostsNotEmbedded(t *testing.T) {
	app := NewAppWith(Options{
		Home: t.TempDir(),
		ClisyncEnsurePath: func(string) (string, error) {
			return "ready", nil
		},
	})
	res, err := app.SyncHosts([]string{"codebuddy"})
	if err != nil {
		t.Fatal(err)
	}
	if res.BinaryState != "not_embedded" || skillStep(res, "codebuddy").State != "installed" {
		t.Fatalf("res = %+v", res)
	}
}

// SyncHosts 未知宿主走 wire key 错误，不影响其它宿主。
func TestSyncHostsUnknownHost(t *testing.T) {
	app := NewAppWith(Options{
		Home:      t.TempDir(),
		CLIBinary: []byte("MZ-fake-cli"),
	})
	res, err := app.SyncHosts([]string{"no-such-host", "codebuddy"})
	if err != nil {
		t.Fatal(err)
	}
	if skillStep(res, "no-such-host").State != "error" ||
		skillStep(res, "no-such-host").Err != "err.hostsync.unknown_host|no-such-host" {
		t.Fatalf("未知宿主状态不符: %+v", res.Skills)
	}
	if skillStep(res, "codebuddy").State != "installed" {
		t.Fatalf("未知宿主不应拖垮其余: %+v", res.Skills)
	}
}
