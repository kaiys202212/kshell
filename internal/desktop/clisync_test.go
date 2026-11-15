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

// SyncWorkBuddy 全量同步：释放二进制 + PATH + 技能，单步错误进结果不中断。
func TestSyncWorkBuddy(t *testing.T) {
	app, calls := clisyncApp(t)
	res, err := app.SyncWorkBuddy()
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
	raw, err := os.ReadFile(filepath.Join(app.opts.Home, ".codebuddy", "skills", "kshell", "SKILL.md"))
	if err != nil {
		t.Fatalf("SKILL.md 未写入: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("SKILL.md 不应为空")
	}
}

// GetWorkBuddySyncStatus 只读，不写盘、不触发 PATH 配置。
func TestGetWorkBuddySyncStatus(t *testing.T) {
	app, calls := clisyncApp(t)
	st := app.GetWorkBuddySyncStatus()
	if st.BinaryState != "absent" || st.SkillState != "absent" {
		t.Fatalf("初始状态: %+v", st)
	}
	if len(*calls) != 0 {
		t.Fatal("Status 不得触发 PATH 配置")
	}
	if _, err := app.SyncWorkBuddy(); err != nil {
		t.Fatal(err)
	}
	st = app.GetWorkBuddySyncStatus()
	if st.BinaryState != "present" || st.SkillState != "present" || st.PathState != "ready" {
		t.Fatalf("同步后状态: %+v", st)
	}
}

// SyncWorkBuddy 在未内嵌二进制（dev 构建）时二进制步跳过，其余照常。
func TestSyncWorkBuddyNotEmbedded(t *testing.T) {
	app := NewAppWith(Options{
		Home: t.TempDir(),
		ClisyncEnsurePath: func(string) (string, error) {
			return "ready", nil
		},
	})
	res, err := app.SyncWorkBuddy()
	if err != nil {
		t.Fatal(err)
	}
	if res.BinaryState != "not_embedded" || res.SkillState != "installed" {
		t.Fatalf("res = %+v", res)
	}
}
