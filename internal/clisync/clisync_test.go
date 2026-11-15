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

func TestSyncReleasesBinaryAndSkills(t *testing.T) {
	home := t.TempDir()
	rec := &ensureRecorder{state: "added"}
	res, err := clisync.Sync(clisync.Options{
		Home:          home,
		Binary:        fakeBinary(),
		Version:       "v1.0.0",
		Targets:       []string{"codebuddy", "claude"},
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
	// Skills：每宿主一条，顺序与 Targets 一致，路径经 skills.TargetRoot。
	if len(res.Skills) != 2 {
		t.Fatalf("Skills 应 2 条: %+v", res.Skills)
	}
	wantRoots := map[string]string{
		"codebuddy": mustTargetRoot(t, "codebuddy", home),
		"claude":    mustTargetRoot(t, "claude", home),
	}
	for i, wantID := range []string{"codebuddy", "claude"} {
		step := res.Skills[i]
		if step.ToolID != wantID || step.State != "installed" {
			t.Fatalf("Skills[%d] = %+v, want %s/installed", i, step, wantID)
		}
		if step.Path != filepath.Join(wantRoots[wantID], "kshell", "SKILL.md") {
			t.Fatalf("Skills[%d].Path = %q, want under %s", i, step.Path, wantRoots[wantID])
		}
		raw, err := os.ReadFile(step.Path)
		if err != nil {
			t.Fatalf("SKILL.md 未写入 %s: %v", step.Path, err)
		}
		name, _, err := skills.ParseSkillMD(raw)
		if err != nil || name != "kshell" {
			t.Fatalf("frontmatter 解析: name=%q err=%v", name, err)
		}
		if !strings.Contains(string(raw), agentcli.UsageText()) {
			t.Fatalf("%s 的 SKILL.md 应包含 UsageText 全文", wantID)
		}
		if !strings.Contains(string(raw), "v1.0.0") {
			t.Fatalf("%s 的 SKILL.md 应包含版本行", wantID)
		}
	}
}

func mustTargetRoot(t *testing.T, toolID, home string) string {
	t.Helper()
	root, ok := skills.TargetRoot(toolID, home)
	if !ok {
		t.Fatalf("TargetRoot(%s) 不存在", toolID)
	}
	return root
}

// TestSyncEmptyTargetsSkipsSkill：未指定宿主时技能步整体跳过（写入哪个宿主
// 由用户显式决定）。
func TestSyncEmptyTargetsSkipsSkill(t *testing.T) {
	res, err := clisync.Sync(clisync.Options{Home: t.TempDir(), Binary: fakeBinary(), Version: "v1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Skills) != 0 {
		t.Fatalf("Skills 应为空: %+v", res.Skills)
	}
}

// TestSyncIdempotent：内容不变时二进制与各宿主技能都跳过重写（current）。
func TestSyncIdempotent(t *testing.T) {
	home := t.TempDir()
	opts := clisync.Options{Home: home, Binary: fakeBinary(), Version: "v1.0.0", Targets: []string{"codebuddy"}}
	if _, err := clisync.Sync(opts); err != nil {
		t.Fatal(err)
	}
	res, err := clisync.Sync(opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.BinaryState != "current" || len(res.Skills) != 1 || res.Skills[0].State != "current" {
		t.Fatalf("幂等同步: Binary=%q Skills=%+v", res.BinaryState, res.Skills)
	}
}

// TestSyncRewritesOnUpgrade：二进制内容变化（升级）时重新释放；版本变化时技能重写。
func TestSyncRewritesOnUpgrade(t *testing.T) {
	home := t.TempDir()
	if _, err := clisync.Sync(clisync.Options{Home: home, Binary: fakeBinary(), Version: "v1.0.0", Targets: []string{"codebuddy"}}); err != nil {
		t.Fatal(err)
	}
	res, err := clisync.Sync(clisync.Options{Home: home, Binary: []byte("MZ-new"), Version: "v2.0.0", Targets: []string{"codebuddy"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.BinaryState != "released" || len(res.Skills) != 1 || res.Skills[0].State != "installed" {
		t.Fatalf("升级后: Binary=%q Skills=%+v", res.BinaryState, res.Skills)
	}
	raw, _ := os.ReadFile(res.Skills[0].Path)
	if !strings.Contains(string(raw), "v2.0.0") {
		t.Fatal("升级后 SKILL.md 应更新版本行")
	}
}

// TestSyncNotEmbedded：dev 构建未嵌 CLI 时二进制跳过，技能照常。
func TestSyncNotEmbedded(t *testing.T) {
	home := t.TempDir()
	res, err := clisync.Sync(clisync.Options{Home: home, Version: "dev", Targets: []string{"codebuddy"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.BinaryState != "not_embedded" {
		t.Fatalf("BinaryState = %q, want not_embedded", res.BinaryState)
	}
	if len(res.Skills) != 1 || res.Skills[0].State != "installed" {
		t.Fatalf("Skills = %+v, want installed", res.Skills)
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

// TestSyncStaleCopyWarningWireKey：PATH 体检警告走 err.hostsync.* wire key，
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
	if len(res.Warnings) != 1 || !strings.HasPrefix(res.Warnings[0], "err.hostsync.stale_copy|") {
		t.Fatalf("Warnings = %v, want err.hostsync.stale_copy|<dir>", res.Warnings)
	}
}

// TestSyncWritesGuide：同步时生成自助安装说明书；dev 构建（未嵌 CLI）跳过。
func TestSyncWritesGuide(t *testing.T) {
	home := t.TempDir()
	guide := filepath.Join(home, ".kshell", "host-install.md")
	if _, err := clisync.Sync(clisync.Options{Home: home, Version: "v1.0.0", Targets: []string{"codebuddy"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(guide); !os.IsNotExist(err) {
		t.Fatal("dev 构建不应写说明书")
	}
	if _, err := clisync.Sync(clisync.Options{Home: home, Binary: fakeBinary(), Version: "v1.0.0", Targets: []string{"codebuddy"}}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(guide)
	if err != nil {
		t.Fatalf("说明书未写入: %v", err)
	}
	if !strings.Contains(string(raw), "name: kshell") {
		t.Fatal("说明书应内嵌 SKILL.md 全文")
	}
}

// TestSyncGuideFailureWarning：说明书写失败以 wire key 进 Warnings，不阻断同步。
func TestSyncGuideFailureWarning(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".kshell", "host-install.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := clisync.Sync(clisync.Options{Home: home, Binary: fakeBinary(), Version: "v1.0.0", Targets: []string{"codebuddy"}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range res.Warnings {
		if strings.HasPrefix(w, "err.hostsync.guide_write|") {
			found = true
		}
	}
	if !found {
		t.Fatalf("Warnings 应含 guide_write: %v", res.Warnings)
	}
	// 同步本身不受影响。
	if res.BinaryState != "released" || len(res.Skills) != 1 {
		t.Fatalf("说明书失败不应影响其它步骤: %+v", res)
	}
}

// TestStatus：只读状态查询，不触发任何写入；Skills 覆盖全部已知宿主。
func TestStatus(t *testing.T) {
	home := t.TempDir()
	st := clisync.Status(clisync.Options{Home: home, Binary: fakeBinary()})
	if st.BinaryState != "absent" || st.PathState != "missing" {
		t.Fatalf("空环境状态: %+v", st)
	}
	if len(st.Skills) == 0 {
		t.Fatal("Status 的 Skills 应覆盖全部已知宿主")
	}
	for _, s := range st.Skills {
		if s.State != "absent" {
			t.Fatalf("空环境 %s 应 absent: %+v", s.ToolID, s)
		}
	}
	if _, err := clisync.Sync(clisync.Options{Home: home, Binary: fakeBinary(), Version: "v1.0.0", Targets: []string{"codebuddy"}}); err != nil {
		t.Fatal(err)
	}
	st = clisync.Status(clisync.Options{Home: home, Binary: fakeBinary()})
	if st.BinaryState != "present" {
		t.Fatalf("同步后 BinaryState: %+v", st)
	}
	byID := map[string]clisync.SkillStep{}
	for _, s := range st.Skills {
		byID[s.ToolID] = s
	}
	if byID["codebuddy"].State != "present" || byID["claude"].State != "absent" {
		t.Fatalf("同步后按宿主状态不符: %+v", st.Skills)
	}
}

// TestExistingSkillTargets：启动自愈只认已装过技能的宿主，不扩散新目标。
func TestExistingSkillTargets(t *testing.T) {
	home := t.TempDir()
	if got := clisync.ExistingSkillTargets(home); len(got) != 0 {
		t.Fatalf("空环境 ExistingSkillTargets = %v, want 空", got)
	}
	if _, err := clisync.Sync(clisync.Options{Home: home, Binary: fakeBinary(), Version: "v1.0.0", Targets: []string{"codebuddy", "claude"}}); err != nil {
		t.Fatal(err)
	}
	got := clisync.ExistingSkillTargets(home)
	if len(got) != 2 {
		t.Fatalf("ExistingSkillTargets = %v, want 2 项", got)
	}
}
