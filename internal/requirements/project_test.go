package requirements

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeLegacyFile 在临时目录写入指定内容的 requirements.json（模拟旧版本落盘）。
func writeLegacyFile(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "requirements.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestMigrateProjectFromDispatchedTo 验证存量迁移：resolver 就绪时首次 List 触发，
// 把 DispatchedTo 与工作区列表归一化匹配后回填 Project（原值），并落盘 version=2。
func TestMigrateProjectFromDispatchedTo(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"version":1,"items":[` +
		`{"id":"m1","text":"命中","priority":"low","dispatchedTo":"D:/work/proj","status":"active","ts":1},` +
		`{"id":"m2","text":"未命中","priority":"low","dispatchedTo":"D:/work/other","status":"active","ts":2},` +
		`{"id":"m3","text":"已有项目","priority":"low","project":"D:/work/kept","dispatchedTo":"D:/work/proj","status":"active","ts":3},` +
		`{"id":"m4","text":"未派发","priority":"low","status":"pending","ts":4}` +
		`]}`
	writeLegacyFile(t, dir, legacy)

	s := New(dir)
	_ = s.Load()
	// resolver 故意返回大小写、斜杠方向、尾分隔符都不同的写法，验证归一化匹配
	s.SetWorkspaceResolver(func() []string { return []string{`d:\WORK\proj\`, "C:/other"} })

	items := s.List() // List 触发惰性迁移
	byID := map[string]Item{}
	for _, it := range items {
		byID[it.ID] = it
	}
	if got := byID["m1"].Project; got != "D:/work/proj" {
		t.Errorf("命中条目 Project = %q, want 原值 D:/work/proj", got)
	}
	if got := byID["m2"].Project; got != "" {
		t.Errorf("resolver 不含该路径时应保持空, got %q", got)
	}
	if got := byID["m3"].Project; got != "D:/work/kept" {
		t.Errorf("已有 Project 的条目不应被覆盖, got %q", got)
	}
	if got := byID["m4"].Project; got != "" {
		t.Errorf("未派发条目不应回填, got %q", got)
	}

	// 迁移后落盘：version=2 且 project 持久化
	s2 := New(dir)
	_ = s2.Load()
	again := s2.List()
	byID = map[string]Item{}
	for _, it := range again {
		byID[it.ID] = it
	}
	if got := byID["m1"].Project; got != "D:/work/proj" {
		t.Errorf("重启后 Project 未持久化, got %q", got)
	}
}

// TestMigrateDeferredWhenResolverEmpty 验证 resolver 返回空列表（扫描未就绪）时
// 迁移推迟，不置完成标记；resolver 就绪后下次 List 再执行。
func TestMigrateDeferredWhenResolverEmpty(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"version":1,"items":[{"id":"d1","text":"待迁移","priority":"low","dispatchedTo":"D:/work/proj","status":"active","ts":1}]}`
	writeLegacyFile(t, dir, legacy)

	s := New(dir)
	_ = s.Load()
	s.SetWorkspaceResolver(func() []string { return nil }) // 扫描未完成

	if got := s.List()[0].Project; got != "" {
		t.Fatalf("resolver 未就绪不应回填, got %q", got)
	}

	// 换成就绪的 resolver，下次 List 再迁移
	s.SetWorkspaceResolver(func() []string { return []string{"D:/work/proj"} })
	if got := s.List()[0].Project; got != "D:/work/proj" {
		t.Fatalf("resolver 就绪后 List 应完成迁移, got %q", got)
	}
}

// TestLoadAcceptsOlderVersion 验证低版本文件（v1）不因 fileVersion 升到 2 被拒读。
func TestLoadAcceptsOlderVersion(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"version":1,"items":[{"id":"o1","text":"旧数据","priority":"low","status":"pending","ts":1}]}`
	writeLegacyFile(t, dir, legacy)
	s := New(dir)
	_ = s.Load()
	items := s.List()
	if len(items) != 1 || items[0].Text != "旧数据" {
		t.Fatalf("v1 文件应兼容读取, got %+v", items)
	}
}

// TestAddRequiresProject 验证空 project 被 wire key 错误拒绝。
func TestAddRequiresProject(t *testing.T) {
	s, _ := newTestStore(t)
	for _, p := range []string{"", "   "} {
		_, err := s.Add(p, "文本", "low", nil)
		if !errors.Is(err, ErrProjectRequired) {
			t.Errorf("Add 空 project(%q) 应返回 ErrProjectRequired, got %v", p, err)
		}
		if err != nil && err.Error() != "err.requirements.project_required" {
			t.Errorf("错误文案应为稳定 wire key, got %q", err.Error())
		}
	}
	if len(s.List()) != 0 {
		t.Error("校验失败不应产生条目")
	}
}

// TestUpdateRequiresProject 验证 Update 同样要求 project 非空。
func TestUpdateRequiresProject(t *testing.T) {
	s, _ := newTestStore(t)
	it, err := s.Add("/proj", "原文", "low", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := s.Update(it.ID, "", "新文本", "low", nil); !errors.Is(err, ErrProjectRequired) {
		t.Errorf("Update 空 project 应返回 ErrProjectRequired, got %v", err)
	}
	if _, err := s.Update(it.ID, "  ", "新文本", "low", nil); !errors.Is(err, ErrProjectRequired) {
		t.Errorf("Update 纯空白 project 应返回 ErrProjectRequired, got %v", err)
	}
	// 校验失败不应改动原条目
	got := s.List()[0]
	if got.Text != "原文" || got.Project != "/proj" {
		t.Errorf("失败 Update 不应改动条目: %+v", got)
	}
}

// TestAddTrimsProject 验证 project 去空白后保存。
func TestAddTrimsProject(t *testing.T) {
	s, _ := newTestStore(t)
	it, err := s.Add("  /work/proj  ", "文本", "low", nil)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if it.Project != "/work/proj" {
		t.Errorf("Project 应去首尾空白, got %q", it.Project)
	}
}

// TestSameWorkspacePath 验证归一化匹配：大小写不敏感、斜杠方向统一、去尾部多余分隔符。
// 语义对齐前端 lib/workspacePath.ts 的 sameWorkspacePath。
func TestSameWorkspacePath(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{`D:\Work\Proj`, `d:/work/proj`, true},          // 大小写 + 斜杠方向
		{`D:\work\proj\`, `d:/work/proj`, true},         // 尾部分隔符
		{`D:\work\proj\\`, `d:/work/proj//`, true},      // 多个尾部分隔符
		{`/home/u/proj/`, `/home/u/proj`, true},         // POSIX 尾分隔符
		{`D:\work\proj`, `D:\work\other`, false},        // 不同路径
		{`D:\work\proj2`, `D:\work\proj`, false},        // 前缀相同但路径不同
		{"", "", true},                                  // 双空视为相等
		{"", `D:\work`, false},                          // 空与非空不等
	}
	for _, c := range cases {
		if got := sameWorkspacePath(c.a, c.b); got != c.want {
			t.Errorf("sameWorkspacePath(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// TestMigratePersistsCurrentVersion 验证迁移落盘文件带当前版本号（旧测试口径跟随 fileVersion）。
func TestMigratePersistsCurrentVersion(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"version":1,"items":[{"id":"v1","text":"迁移","priority":"low","dispatchedTo":"D:/work/proj","status":"active","ts":1}]}`
	writeLegacyFile(t, dir, legacy)
	s := New(dir)
	_ = s.Load()
	s.SetWorkspaceResolver(func() []string { return []string{"D:/work/proj"} })
	s.List()

	data, err := os.ReadFile(filepath.Join(dir, "requirements.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"version":3`) {
		t.Errorf("迁移后落盘应含 version=3: %s", data)
	}
}
