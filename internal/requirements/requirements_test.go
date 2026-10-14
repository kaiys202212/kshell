package requirements

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestStore 在临时目录创建并加载一个 store。
func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s := New(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return s, dir
}

func TestAddAndList(t *testing.T) {
	s, _ := newTestStore(t)
	it, err := s.Add("  实现登录功能  ", "high", []string{"auth", "web"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if it.Text != "实现登录功能" {
		t.Errorf("Add 应去首尾空白, got %q", it.Text)
	}
	if it.Priority != "high" {
		t.Errorf("priority = %q, want high", it.Priority)
	}
	if it.ID == "" {
		t.Error("ID 不应为空")
	}
	if it.Ts == 0 {
		t.Error("Ts 不应为 0")
	}
	items := s.List()
	if len(items) != 1 || items[0].ID != it.ID {
		t.Fatalf("List = %+v, want 1 条", items)
	}
}

func TestAddEmptyTextRejected(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.Add("   ", "high", nil); err == nil {
		t.Error("纯空白文本应报错")
	}
	if _, err := s.Add("", "high", nil); err == nil {
		t.Error("空文本应报错")
	}
}

func TestPriorityNormalization(t *testing.T) {
	s, _ := newTestStore(t)
	for _, p := range []string{"HIGH", "urgent", "", "h1gh"} {
		it, err := s.Add("x", p, nil)
		if err != nil {
			t.Fatalf("Add(%q): %v", p, err)
		}
		if it.Priority != "medium" {
			t.Errorf("priority %q 应回退 medium, got %q", p, it.Priority)
		}
	}
	for _, p := range []string{"high", "medium", "low"} {
		it, err := s.Add("x", p, nil)
		if err != nil {
			t.Fatalf("Add(%q): %v", p, err)
		}
		if it.Priority != p {
			t.Errorf("合法 priority %q 应保留, got %q", p, it.Priority)
		}
	}
}

func TestNewestFirst(t *testing.T) {
	s, _ := newTestStore(t)
	a, _ := s.Add("第一条", "low", nil)
	b, _ := s.Add("第二条", "low", nil)
	items := s.List()
	if len(items) != 2 {
		t.Fatalf("List 长度 = %d, want 2", len(items))
	}
	if items[0].ID != b.ID || items[1].ID != a.ID {
		t.Errorf("新条目应插入列表头: got [%s, %s], want [%s, %s]", items[0].ID, items[1].ID, b.ID, a.ID)
	}
}

func TestIDUnique(t *testing.T) {
	s, _ := newTestStore(t)
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		it, err := s.Add("x", "low", nil)
		if err != nil {
			t.Fatalf("Add #%d: %v", i, err)
		}
		if seen[it.ID] {
			t.Fatalf("ID 重复: %s", it.ID)
		}
		seen[it.ID] = true
		if len(it.ID) != 32 {
			t.Errorf("ID 应为 16 字节 hex（32 字符）, got len %d", len(it.ID))
		}
	}
}

func TestUpdate(t *testing.T) {
	s, _ := newTestStore(t)
	it, _ := s.Add("原文", "low", []string{"a"})
	got, err := s.Update(it.ID, "改后文本", "high", []string{"b", "c"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Text != "改后文本" || got.Priority != "high" {
		t.Errorf("Update 结果 = %+v", got)
	}
	if len(got.Tags) != 2 {
		t.Errorf("Tags 应被整体替换, got %v", got.Tags)
	}
	items := s.List()
	if items[0].Text != "改后文本" {
		t.Errorf("List 未反映更新: %+v", items[0])
	}
}

func TestUpdateValidation(t *testing.T) {
	s, _ := newTestStore(t)
	it, _ := s.Add("原文", "low", nil)
	if _, err := s.Update(it.ID, "  ", "low", nil); err == nil {
		t.Error("Update 空文本应报错")
	}
	got, err := s.Update(it.ID, "正文", "bogus", nil)
	if err != nil {
		t.Fatalf("Update 非法 priority 不应报错, got %v", err)
	}
	if got.Priority != "medium" {
		t.Errorf("Update 非法 priority 应回退 medium, got %q", got.Priority)
	}
	if _, err := s.Update("不存在", "x", "low", nil); err == nil {
		t.Error("Update 不存在的 id 应报错")
	}
}

func TestDeleteIdempotent(t *testing.T) {
	s, _ := newTestStore(t)
	it, _ := s.Add("待删", "low", nil)
	if err := s.Delete(it.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(s.List()) != 0 {
		t.Error("删除后 List 应为空")
	}
	if err := s.Delete(it.ID); err != nil {
		t.Errorf("Delete 不存在的 id 应幂等返回 nil, got %v", err)
	}
}

func TestMarkDispatched(t *testing.T) {
	s, _ := newTestStore(t)
	it, _ := s.Add("需求", "low", nil)
	if err := s.MarkDispatched(it.ID, "D:/work/proj"); err != nil {
		t.Fatalf("MarkDispatched: %v", err)
	}
	items := s.List()
	if items[0].DispatchedTo != "D:/work/proj" {
		t.Errorf("DispatchedTo = %q, want D:/work/proj", items[0].DispatchedTo)
	}
	if err := s.MarkDispatched("不存在", "x"); err == nil {
		t.Error("MarkDispatched 不存在的 id 应报错")
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s1 := New(dir)
	if err := s1.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	a, _ := s1.Add("需求一", "high", []string{"tag1"})
	b, _ := s1.Add("需求二", "low", []string{"tag2", "tag3"})
	_ = s1.MarkDispatched(b.ID, "/tmp/ws")

	// 重新创建 store 模拟进程重启
	s2 := New(dir)
	if err := s2.Load(); err != nil {
		t.Fatalf("重开 Load: %v", err)
	}
	items := s2.List()
	if len(items) != 2 {
		t.Fatalf("重开 List 长度 = %d, want 2", len(items))
	}
	if items[0].ID != b.ID || items[1].ID != a.ID {
		t.Errorf("顺序应保持最新在前: got [%s, %s]", items[0].ID, items[1].ID)
	}
	if items[1].Text != "需求一" || items[1].Priority != "high" {
		t.Errorf("条目未正确往返: %+v", items[1])
	}
	if items[0].DispatchedTo != "/tmp/ws" {
		t.Errorf("DispatchedTo 未持久化: %+v", items[0])
	}
}

func TestLoadCorruptOrMissing(t *testing.T) {
	dir := t.TempDir()
	// 缺失文件
	s := New(dir)
	if err := s.Load(); err != nil {
		t.Errorf("缺失文件 Load 应返回 nil, got %v", err)
	}
	// 损坏 JSON
	if err := os.WriteFile(filepath.Join(dir, "requirements.json"), []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	s = New(dir)
	if err := s.Load(); err != nil {
		t.Errorf("损坏 JSON Load 应返回 nil, got %v", err)
	}
	if len(s.List()) != 0 {
		t.Error("损坏文件应按空处理")
	}
	// 版本不符
	s2 := New(dir)
	if _, err := s2.Add("x", "low", nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "requirements.json"))
	bad := strings.Replace(string(data), `"version":1`, `"version":99`, 1)
	_ = os.WriteFile(filepath.Join(dir, "requirements.json"), []byte(bad), 0o600)
	s3 := New(dir)
	if err := s3.Load(); err != nil {
		t.Errorf("版本不符 Load 应返回 nil, got %v", err)
	}
	if len(s3.List()) != 0 {
		t.Error("版本不符应按空处理")
	}
}

func TestListReturnsCopy(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.Add("原文", "low", nil); err != nil {
		t.Fatal(err)
	}
	items := s.List()
	items[0].Text = "外部篡改"
	items[0].Tags = append(items[0].Tags, "hack")
	again := s.List()
	if again[0].Text != "原文" {
		t.Errorf("List 返回的应是副本: %+v", again[0])
	}
	if len(again[0].Tags) != 0 {
		t.Errorf("内部 Tags 被外部修改污染: %v", again[0].Tags)
	}
}

func TestTagsAreCopiedOnAdd(t *testing.T) {
	s, _ := newTestStore(t)
	tags := []string{"a", "b"}
	if _, err := s.Add("x", "low", tags); err != nil {
		t.Fatal(err)
	}
	// 修改调用方切片不应影响 store 内部状态
	tags[0] = "篡改"
	items := s.List()
	if items[0].Tags[0] != "a" {
		t.Errorf("Add 未拷贝调用方 tags: %v", items[0].Tags)
	}
}

func TestFileWrittenAtomic(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if _, err := s.Add("x", "low", nil); err != nil {
		t.Fatal(err)
	}
	// 原子写后目录里不应残留临时文件
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("残留临时文件: %s", e.Name())
		}
	}
	// 文件内容带 version
	data, err := os.ReadFile(filepath.Join(dir, "requirements.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"version":1`) {
		t.Errorf("落盘文件应含 version=1: %s", data)
	}
}
