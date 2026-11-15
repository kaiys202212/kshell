package clisync_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/agentcli"
	"github.com/yangk/kshell/internal/clisync"
	"github.com/yangk/kshell/internal/skills"
)

func fakeBinary() []byte { return []byte("MZ-fake-kshell-cli-binary") }

// ensureRecorder 记录 EnsurePath 调用并返回预设结果。
type ensureRecorder struct {
	calls []string
	state string
}

func (r *ensureRecorder) ensure(binDir string) (string, error) {
	r.calls = append(r.calls, binDir)
	return r.state, nil
}

func TestSyncReleasesBinaryAndSkill(t *testing.T) {
	home := t.TempDir()
	rec := &ensureRecorder{state: "added"}
	res, err := clisync.Sync(clisync.Options{
		Home:          home,
		Binary:        fakeBinary(),
		Version:       "v1.0.0",
		ConfigurePath: true,
		EnsurePath:    rec.ensure,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.BinaryState != "released" {
		t.Fatalf("BinaryState = %q (%s), want released", res.BinaryState, res.BinaryErr)
	}
	binPath := filepath.Join(clisync.BinDir(home), clisync.BinaryName())
	b, err := os.ReadFile(binPath)
	if err != nil || string(b) != string(fakeBinary()) {
		t.Fatalf("释放的二进制内容不符: %v %q", err, b)
	}
	if res.BinaryPath != binPath {
		t.Fatalf("BinaryPath = %q, want %q", res.BinaryPath, binPath)
	}
	if res.PathState != "added" {
		t.Fatalf("PathState = %q (%s), want added", res.PathState, res.PathErr)
	}
	if len(rec.calls) != 1 || rec.calls[0] != clisync.BinDir(home) {
		t.Fatalf("EnsurePath 调用不符: %v", rec.calls)
	}
	// Skill：frontmatter 可解析、含完整用法文本与版本行。
	skillPath := filepath.Join(home, ".codebuddy", "skills", "kshell", "SKILL.md")
	raw, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("SKILL.md 未写入: %v", err)
	}
	name, _, err := skills.ParseSkillMD(raw)
	if err != nil || name != "kshell" {
		t.Fatalf("frontmatter 解析: name=%q err=%v", name, err)
	}
	if !strings.Contains(string(raw), agentcli.UsageText()) {
		t.Fatal("SKILL.md 应包含 UsageText 全文")
	}
	if !strings.Contains(string(raw), "v1.0.0") {
		t.Fatal("SKILL.md 应包含版本行")
	}
	if res.SkillState != "installed" {
		t.Fatalf("SkillState = %q (%s), want installed", res.SkillState, res.SkillErr)
	}
}

// TestSyncIdempotent：内容不变时二进制与技能都跳过重写（current）。
func TestSyncIdempotent(t *testing.T) {
	home := t.TempDir()
	opts := clisync.Options{Home: home, Binary: fakeBinary(), Version: "v1.0.0"}
	if _, err := clisync.Sync(opts); err != nil {
		t.Fatal(err)
	}
	res, err := clisync.Sync(opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.BinaryState != "current" || res.SkillState != "current" {
		t.Fatalf("幂等同步: Binary=%q Skill=%q, want current/current", res.BinaryState, res.SkillState)
	}
}

// TestSyncRewritesOnUpgrade：二进制内容变化（升级）时重新释放；版本变化时技能重写。
func TestSyncRewritesOnUpgrade(t *testing.T) {
	home := t.TempDir()
	if _, err := clisync.Sync(clisync.Options{Home: home, Binary: fakeBinary(), Version: "v1.0.0"}); err != nil {
		t.Fatal(err)
	}
	res, err := clisync.Sync(clisync.Options{Home: home, Binary: []byte("MZ-new"), Version: "v2.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if res.BinaryState != "released" || res.SkillState != "installed" {
		t.Fatalf("升级后: Binary=%q Skill=%q, want released/installed", res.BinaryState, res.SkillState)
	}
	raw, _ := os.ReadFile(filepath.Join(home, ".codebuddy", "skills", "kshell", "SKILL.md"))
	if !strings.Contains(string(raw), "v2.0.0") {
		t.Fatal("升级后 SKILL.md 应更新版本行")
	}
}

// TestSyncNotEmbedded：dev 构建未嵌 CLI 时二进制跳过，其余步骤照常。
func TestSyncNotEmbedded(t *testing.T) {
	home := t.TempDir()
	res, err := clisync.Sync(clisync.Options{Home: home, Version: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	if res.BinaryState != "not_embedded" {
		t.Fatalf("BinaryState = %q, want not_embedded", res.BinaryState)
	}
	if res.SkillState != "installed" {
		t.Fatalf("SkillState = %q, want installed", res.SkillState)
	}
}

// TestSyncNotEmbeddedSkipsPath：dev 构建没有 CLI，即使显式请求也不配 PATH
//（指向空目录毫无意义）。
func TestSyncNotEmbeddedSkipsPath(t *testing.T) {
	rec := &ensureRecorder{state: "ready"}
	res, err := clisync.Sync(clisync.Options{
		Home:          t.TempDir(),
		Version:       "dev",
		ConfigurePath: true,
		EnsurePath:    rec.ensure,
		PathLookup:    func() []string { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.BinaryState != "not_embedded" || res.PathState != "skipped" || len(rec.calls) != 0 {
		t.Fatalf("res = %+v calls = %v", res, rec.calls)
	}
}

// TestSyncStaleCopyWarningWireKey：PATH 体检警告走 err.wbsync.* wire key，
// 由前端 translateBackend 渲染，Go 侧不输出自然语言。
func TestSyncStaleCopyWarningWireKey(t *testing.T) {
	home := t.TempDir()
	binDir := clisync.BinDir(home)
	stale := filepath.Join(home, "stale")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, clisync.BinaryName()), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := clisync.Sync(clisync.Options{
		Home:          home,
		Binary:        fakeBinary(),
		Version:       "v1.0.0",
		ConfigurePath: true,
		EnsurePath:    func(string) (string, error) { return "ready", nil },
		PathLookup:    func() []string { return []string{stale, binDir} },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 1 || !strings.HasPrefix(res.Warnings[0], "err.wbsync.stale_copy|") {
		t.Fatalf("Warnings = %v, want err.wbsync.stale_copy|<dir>", res.Warnings)
	}
}

// TestSyncConfigurePathOff：不配 PATH 时该步 skipped，EnsurePath 不被调用。
func TestSyncConfigurePathOff(t *testing.T) {
	home := t.TempDir()
	rec := &ensureRecorder{state: "ready"}
	res, err := clisync.Sync(clisync.Options{Home: home, Binary: fakeBinary(), Version: "v1.0.0", EnsurePath: rec.ensure})
	if err != nil {
		t.Fatal(err)
	}
	if res.PathState != "skipped" || len(rec.calls) != 0 {
		t.Fatalf("PathState=%q calls=%v, want skipped/未调用", res.PathState, rec.calls)
	}
}

// TestStatus：只读状态查询，不触发任何写入。
func TestStatus(t *testing.T) {
	home := t.TempDir()
	st := clisync.Status(clisync.Options{Home: home, Binary: fakeBinary()})
	if st.BinaryState != "absent" || st.SkillState != "absent" || st.PathState != "missing" {
		t.Fatalf("空环境状态: %+v", st)
	}
	if _, err := clisync.Sync(clisync.Options{Home: home, Binary: fakeBinary(), Version: "v1.0.0"}); err != nil {
		t.Fatal(err)
	}
	st = clisync.Status(clisync.Options{Home: home, Binary: fakeBinary()})
	if st.BinaryState != "present" || st.SkillState != "present" {
		t.Fatalf("同步后状态: %+v", st)
	}
}
