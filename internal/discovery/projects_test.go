package discovery

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func newProjectStore(t *testing.T) (*ProjectStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "projects.yaml")
	return NewProjectStore(path), path
}

func TestProjectStoreMissingFileIsEmpty(t *testing.T) {
	st, path := newProjectStore(t)

	if err := st.Load(); err != nil {
		t.Fatalf("文件缺失应视为空表: %v", err)
	}
	if len(st.Manual()) != 0 || len(st.Deleted()) != 0 {
		t.Fatalf("空表被污染: %v / %v", st.Manual(), st.Deleted())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("只读加载不应创建文件")
	}
}

func TestProjectStoreCorruptFileReturnsEmptyAndError(t *testing.T) {
	st, path := newProjectStore(t)
	if err := os.WriteFile(path, []byte("manual: [unclosed"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := st.Load(); err == nil {
		t.Fatal("损坏文件应返回错误")
	}
	if len(st.Manual()) != 0 {
		t.Fatalf("损坏后应为空表: %v", st.Manual())
	}
}

func TestProjectStoreAddHideRestore(t *testing.T) {
	st, path := newProjectStore(t)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "demo")

	if err := st.Add(dir); err != nil {
		t.Fatalf("Add error: %v", err)
	}
	if got := st.Manual(); len(got) != 1 || got[0] != dir {
		t.Fatalf("Manual = %v, want [%s]", got, dir)
	}
	st.Add(dir) // 幂等
	if got := st.Manual(); len(got) != 1 {
		t.Fatalf("重复 Add 不应产生副本: %v", got)
	}

	if err := st.Hide(dir); err != nil {
		t.Fatalf("Hide error: %v", err)
	}
	if !st.IsDeleted(dir) {
		t.Fatal("Hide 后 IsDeleted 应为 true")
	}
	if got := st.Deleted(); len(got) != 1 || got[0].Path != dir || got[0].At.IsZero() {
		t.Fatalf("回收站记录不符: %+v", got)
	}

	// 落盘内容：manual 保留（还原时无需重新登记），deleted 带路径
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "manual:") || !strings.Contains(string(raw), "deleted:") {
		t.Fatalf("落盘内容缺少 manual/deleted 段:\n%s", raw)
	}

	if err := st.Restore(dir); err != nil {
		t.Fatalf("Restore error: %v", err)
	}
	if st.IsDeleted(dir) {
		t.Fatal("Restore 后 IsDeleted 应为 false")
	}
	if got := st.Manual(); len(got) != 1 || got[0] != dir {
		t.Fatalf("Restore 后 manual 应仍在: %v", got)
	}
	// 幂等：再还原一次不报错
	if err := st.Restore(dir); err != nil {
		t.Fatalf("重复 Restore 应幂等: %v", err)
	}
}

func TestProjectStoreRedoOnDisk(t *testing.T) {
	st, path := newProjectStore(t)
	dir := filepath.Join(t.TempDir(), "again")
	if err := st.Add(dir); err != nil {
		t.Fatal(err)
	}
	if err := st.Hide(dir); err != nil {
		t.Fatal(err)
	}

	// 新实例从磁盘恢复同一状态
	other := NewProjectStore(path)
	if err := other.Load(); err != nil {
		t.Fatal(err)
	}
	if len(other.Manual()) != 1 || !other.IsDeleted(dir) {
		t.Fatalf("重载后状态丢失: manual=%v deleted=%v", other.Manual(), other.Deleted())
	}
}

func TestProjectStoreAddClearsDeleted(t *testing.T) {
	st, _ := newProjectStore(t)
	dir := filepath.Join(t.TempDir(), "revive")

	if err := st.Hide(dir); err != nil {
		t.Fatal(err)
	}
	if err := st.Add(dir); err != nil {
		t.Fatal(err)
	}
	if st.IsDeleted(dir) {
		t.Fatal("重新添加应从回收站移出")
	}
	if got := st.Manual(); len(got) != 1 {
		t.Fatalf("Manual = %v", got)
	}
}

func TestProjectStorePathNormalization(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("大小写不敏感只存在于 Windows")
	}
	st, _ := newProjectStore(t)
	upper := `D:\data\workspace\Demo`
	lower := `d:/data/workspace/demo`

	if err := st.Hide(upper); err != nil {
		t.Fatal(err)
	}
	if !st.IsDeleted(lower) {
		t.Fatal("同一路径的不同写法应被识别为已隐藏")
	}
	if err := st.Add(lower); err != nil {
		t.Fatal(err)
	}
	// 落盘保留用户写法，只做 filepath.Clean（分隔符归一），不改大小写
	if got := st.Manual(); len(got) != 1 || got[0] != filepath.Clean(lower) {
		t.Fatalf("应记录为第二次传入的写法: %v", got)
	}
}

func TestApplyProjectsNilStore(t *testing.T) {
	in := []Workspace{{Path: `D:\a`, Source: "sessions"}}
	out := ApplyProjects(in, nil, nil)
	if len(out) != 1 || out[0].Path != in[0].Path {
		t.Fatalf("st 为 nil 应原样返回: %+v", out)
	}
}

func TestApplyProjectsFiltersDeletedAndMissing(t *testing.T) {
	st, _ := newProjectStore(t)
	gone := filepath.Join(t.TempDir(), "gone")
	if err := st.Hide(gone); err != nil {
		t.Fatal(err)
	}

	in := []Workspace{
		{Path: gone, Source: "sessions"},         // 已隐藏
		{Path: `D:\deleted-repo`, Source: "git"}, // 目录不存在
		{Path: `D:\alive`, Source: "sessions"},   // 保留
	}
	exists := func(p string) bool { return p == `D:\alive` }

	out := ApplyProjects(in, st, exists)
	if len(out) != 1 || out[0].Path != `D:\alive` {
		t.Fatalf("过滤结果不符: %+v", out)
	}
}

func TestApplyProjectsAppendsManualIdempotently(t *testing.T) {
	st, _ := newProjectStore(t)
	manual := filepath.Join(t.TempDir(), "my-proj")
	if err := st.Add(manual); err != nil {
		t.Fatal(err)
	}

	in := []Workspace{{Path: `D:\scanned`, Source: "sessions"}}
	exists := func(string) bool { return true }

	out := ApplyProjects(in, st, exists)
	if len(out) != 2 {
		t.Fatalf("应追加手动项目: %+v", out)
	}
	last := out[len(out)-1]
	if last.Path != manual || last.Source != "manual" || last.Name != "my-proj" {
		t.Fatalf("手动项字段不符: %+v", last)
	}

	// 幂等：再叠加一次不会重复追加
	if again := ApplyProjects(out, st, exists); len(again) != 2 {
		t.Fatalf("重复叠加不应重复追加: %+v", again)
	}
}

func TestApplyProjectsKeepsScannedSource(t *testing.T) {
	st, _ := newProjectStore(t)
	path := `D:\both`
	if err := st.Add(path); err != nil {
		t.Fatal(err)
	}

	in := []Workspace{{Path: path, Name: "both", Source: "sessions", SessionCount: 3}}
	out := ApplyProjects(in, st, func(string) bool { return true })

	if len(out) != 1 {
		t.Fatalf("已扫描到的手动项目不应重复: %+v", out)
	}
	if out[0].Source != "sessions" || out[0].SessionCount != 3 {
		t.Fatalf("应保留扫描来源与统计: %+v", out[0])
	}
}

func TestApplyProjectsSkipsDeletedManualAndMissingManual(t *testing.T) {
	st, _ := newProjectStore(t)
	hidden := filepath.Join(t.TempDir(), "hidden-proj")
	gone := filepath.Join(t.TempDir(), "gone-proj")
	if err := st.Add(hidden); err != nil {
		t.Fatal(err)
	}
	if err := st.Hide(hidden); err != nil {
		t.Fatal(err)
	}
	if err := st.Add(gone); err != nil {
		t.Fatal(err)
	}

	exists := func(p string) bool { return p != gone }
	if out := ApplyProjects(nil, st, exists); len(out) != 0 {
		t.Fatalf("回收站项与目录已消失的手动项都不应出现: %+v", out)
	}

	// 还原后回来
	if err := st.Restore(hidden); err != nil {
		t.Fatal(err)
	}
	out := ApplyProjects(nil, st, exists)
	if len(out) != 1 || out[0].Path != hidden {
		t.Fatalf("还原后应回到列表: %+v", out)
	}
}

func TestDirExists(t *testing.T) {
	dir := t.TempDir()
	if !DirExists(dir) {
		t.Fatal("临时目录应存在")
	}
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if DirExists(file) {
		t.Fatal("文件不是目录")
	}
	if DirExists(filepath.Join(dir, "nope")) || DirExists("  ") {
		t.Fatal("不存在或空路径应为 false")
	}
}

// 项目存储对外错误是 wire key，供前端翻译。
func TestProjectStoreErrorKeys(t *testing.T) {
	st, _ := newProjectStore(t)
	if err := st.Add("  "); err == nil || err.Error() != "err.projects.empty_path" {
		t.Fatalf("空路径 Add 错误 = %v, want err.projects.empty_path", err)
	}
	if err := st.Hide(""); err == nil || err.Error() != "err.projects.empty_path" {
		t.Fatalf("空路径 Hide 错误 = %v, want err.projects.empty_path", err)
	}
	if err := st.Restore(""); err == nil || err.Error() != "err.projects.empty_path" {
		t.Fatalf("空路径 Restore 错误 = %v, want err.projects.empty_path", err)
	}
	if err := NewProjectStore("").Save(); err == nil || err.Error() != "err.projects.empty_file" {
		t.Fatalf("空文件路径 Save 错误 = %v, want err.projects.empty_file", err)
	}
}

func TestProjectStoreDeletedNewestFirst(t *testing.T) {
	st, _ := newProjectStore(t)
	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")
	if err := st.Hide(first); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := st.Hide(second); err != nil {
		t.Fatal(err)
	}

	got := st.Deleted()
	if len(got) != 2 || got[0].Path != second {
		t.Fatalf("最近删除的应排最前: %+v", got)
	}
}

func TestProjectStoreSSHRoundTrip(t *testing.T) {
	st, path := newProjectStore(t)
	if err := st.AddEntry(ProjectEntry{Kind: KindSSH, ConnID: "c1", Path: "/home/u/p"}); err != nil {
		t.Fatal(err)
	}
	other := NewProjectStore(path)
	if err := other.Load(); err != nil {
		t.Fatal(err)
	}
	entries := other.ManualEntries()
	if len(entries) != 1 || entries[0].Kind != KindSSH || entries[0].ConnID != "c1" || entries[0].Path != "/home/u/p" {
		t.Fatalf("%+v", entries)
	}
}

func TestApplyProjectsAppendsSSHWithoutExists(t *testing.T) {
	st, _ := newProjectStore(t)
	if err := st.AddEntry(ProjectEntry{Kind: KindSSH, ConnID: "c1", Path: "/r"}); err != nil {
		t.Fatal(err)
	}
	out := ApplyProjects(nil, st, func(string) bool { return false })
	if len(out) != 1 || out[0].Kind != KindSSH || out[0].Path != "/r" || out[0].ConnID != "c1" || out[0].Source != "manual" {
		t.Fatalf("%+v", out)
	}
}

func TestProjectStoreLoadsLegacyManualStrings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.yaml")
	legacy := "manual:\n  - D:\\foo\n  - D:\\bar\n"
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	st := NewProjectStore(path)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	entries := st.ManualEntries()
	if len(entries) != 2 {
		t.Fatalf("应读入两条本地项: %+v", entries)
	}
	for _, e := range entries {
		if e.Kind != KindLocal && e.Kind != "" {
			t.Fatalf("旧字符串项应为 local: %+v", e)
		}
		if e.Path == "" {
			t.Fatalf("path 为空: %+v", e)
		}
	}
	if got := st.Manual(); len(got) != 2 {
		t.Fatalf("Manual 兼容: %v", got)
	}
}

func TestProjectStoreSSHHideRestoreByKey(t *testing.T) {
	st, path := newProjectStore(t)
	e := ProjectEntry{Kind: KindSSH, ConnID: "c1", Path: "/home/u/../u/proj//"}
	if err := st.AddEntry(e); err != nil {
		t.Fatal(err)
	}
	ref := FormatSSHRef("c1", "/home/u/proj")
	if err := st.Hide(ref); err != nil {
		t.Fatal(err)
	}
	if !st.IsDeleted(ref) {
		t.Fatal("ssh Hide 后应按归一化 key 判定已删除")
	}
	// 不同写法同一远端路径
	if !st.IsDeleted(FormatSSHRef("c1", "/home/u/proj/")) {
		t.Fatal("远端路径归一化后应匹配")
	}
	if st.IsDeleted(FormatSSHRef("c2", "/home/u/proj")) {
		t.Fatal("不同 conn 不应匹配")
	}

	other := NewProjectStore(path)
	if err := other.Load(); err != nil {
		t.Fatal(err)
	}
	if !other.IsDeleted(ref) {
		t.Fatal("deleted ssh 条目应可从 YAML 重载")
	}
	if err := other.Restore(ref); err != nil {
		t.Fatal(err)
	}
	if other.IsDeleted(ref) {
		t.Fatal("Restore 后应不在回收站")
	}
	out := ApplyProjects(nil, other, func(string) bool { return false })
	if len(out) != 1 || out[0].Kind != KindSSH || out[0].Path != "/home/u/proj" {
		t.Fatalf("还原后 ssh 项应回到列表: %+v", out)
	}
}

func TestProjectStoreLoadsLegacyDeleted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projects.yaml")
	legacy := "deleted:\n  - path: D:\\old\n    at: 2026-01-01T00:00:00Z\n"
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	st := NewProjectStore(path)
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	if !st.IsDeleted(`D:\old`) {
		t.Fatal("旧 deleted 字符串 path 应可识别")
	}
}
