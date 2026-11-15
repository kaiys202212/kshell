package agentapi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestMemoryAppendRecentSearch(t *testing.T) {
	dir := t.TempDir()
	m := NewManagerMemory(dir)

	e1 := MemoryEntry{Channel: "hub", Input: "修复登录页", Outcome: "已提交 96e9bef"}
	e2 := MemoryEntry{Channel: "wechat", Input: "跑一下测试", Outcome: "全绿", Actions: "go test ./..."}
	e3 := MemoryEntry{Channel: "hub", Input: "部署桌面版", Outcome: "完成"}

	for _, e := range []MemoryEntry{e1, e2, e3} {
		if err := m.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	// Ts 零值时写入补当前时刻
	if m.entries[0].Ts == 0 || m.entries[2].Ts == 0 {
		t.Fatal("Append 未补 Ts")
	}

	// Recent 倒序
	got := m.Recent(2)
	if len(got) != 2 {
		t.Fatalf("Recent(2) 长度 = %d, want 2", len(got))
	}
	if got[0].Input != e3.Input || got[1].Input != e2.Input {
		t.Fatalf("Recent(2) 顺序错误: %q, %q", got[0].Input, got[1].Input)
	}

	// Recent 副本：修改不影响内部状态
	got[0].Input = "篡改"
	if m.Recent(1)[0].Input == "篡改" {
		t.Fatal("Recent 返回了内部切片")
	}

	// Recent(0) 与负数返回空
	if got := m.Recent(0); len(got) != 0 {
		t.Fatalf("Recent(0) 长度 = %d, want 0", len(got))
	}
	if got := m.Recent(-1); len(got) != 0 {
		t.Fatalf("Recent(-1) 长度 = %d, want 0", len(got))
	}

	// Search 命中 Input / Outcome / Actions 任一字段
	if got := m.Search("部署"); len(got) != 1 || got[0].Input != e3.Input {
		t.Fatalf("Search(Input) = %v", got)
	}
	if got := m.Search("全绿"); len(got) != 1 || got[0].Outcome != e2.Outcome {
		t.Fatalf("Search(Outcome) = %v", got)
	}
	if got := m.Search("go test"); len(got) != 1 || got[0].Actions != e2.Actions {
		t.Fatalf("Search(Actions) = %v", got)
	}
}

func TestMemorySearchCaseInsensitiveAndEmpty(t *testing.T) {
	dir := t.TempDir()
	m := NewManagerMemory(dir)
	if err := m.Append(MemoryEntry{Channel: "hub", Input: "Build Desktop App", Outcome: "ok"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	if got := m.Search("build desktop"); len(got) != 1 {
		t.Fatalf("大小写不敏感搜索失败: %v", got)
	}
	if got := m.Search("  "); len(got) != 0 {
		t.Fatalf("空 keyword 应返回空: %v", got)
	}
	if got := m.Search("不存在"); len(got) != 0 {
		t.Fatalf("未命中应返回空: %v", got)
	}
}

func TestMemorySearchLimit(t *testing.T) {
	dir := t.TempDir()
	m := NewManagerMemory(dir)
	for i := 0; i < 30; i++ {
		if err := m.Append(MemoryEntry{Channel: "hub", Input: fmt.Sprintf("任务%d", i)}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if got := m.Search("任务"); len(got) != 20 {
		t.Fatalf("Search 上限 = %d, want 20", len(got))
	}
	// 结果新在前：第一条应是最后写入的一条
	if got := m.Search("任务"); got[0].Input != "任务29" {
		t.Fatalf("Search 结果顺序不是新在前: %q", got[0].Input)
	}
}

func TestMemoryLoadMissingAndCorruptFile(t *testing.T) {
	dir := t.TempDir()

	// 文件不存在按空处理
	m1 := NewManagerMemory(dir)
	if got := m1.Recent(10); len(got) != 0 {
		t.Fatalf("缺失文件应按空处理: %v", got)
	}

	// 损坏文件：好行 + 坏行 + 好行，逐行解析跳过坏行
	path := filepath.Join(dir, "memory.jsonl")
	content := "{\"ts\":1,\"channel\":\"hub\",\"input\":\"第一条\"}\n" +
		"这不是JSON\n" +
		"{\"ts\":2,\"channel\":\"hub\",\"input\":\"第二条\"}\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写损坏文件: %v", err)
	}
	m2 := NewManagerMemory(dir)
	got := m2.Recent(10)
	if len(got) != 2 {
		t.Fatalf("应跳过坏行保留 2 条, got %d", len(got))
	}
	if got[0].Input != "第二条" || got[1].Input != "第一条" {
		t.Fatalf("加载顺序错误: %v", got)
	}

	// 后续 Append 正常追加
	if err := m2.Append(MemoryEntry{Channel: "hub", Input: "第三条"}); err != nil {
		t.Fatalf("Append after corrupt load: %v", err)
	}
	if got := m2.Recent(1); got[0].Input != "第三条" {
		t.Fatalf("损坏加载后追加失败: %v", got)
	}
}

func TestMemoryCapTruncates(t *testing.T) {
	dir := t.TempDir()
	m := NewManagerMemory(dir)
	for i := 0; i < 505; i++ {
		if err := m.Append(MemoryEntry{Channel: "hub", Input: "条目", Outcome: string(rune('a' + i%26))}); err != nil {
			t.Fatalf("Append #%d: %v", i, err)
		}
	}

	if len(m.entries) != memoryCap {
		t.Fatalf("内存条数 = %d, want %d", len(m.entries), memoryCap)
	}

	// 文件只剩 500 行（cap 触发整体重写清掉溢出）
	data, err := os.ReadFile(filepath.Join(dir, "memory.jsonl"))
	if err != nil {
		t.Fatalf("读文件: %v", err)
	}
	lines := strings.Count(strings.TrimRight(string(data), "\n"), "\n") + 1
	if lines != memoryCap {
		t.Fatalf("文件行数 = %d, want %d", lines, memoryCap)
	}

	// 最新保留：最后写入的 Outcome 对应 i=504（'a'+504%26）
	want := string(rune('a' + 504%26))
	got := m.Recent(1)
	if got[0].Outcome != want {
		t.Fatalf("最新条目被截断: Outcome = %q, want %q", got[0].Outcome, want)
	}

	// 重启后加载仍是 500 条
	m2 := NewManagerMemory(dir)
	if len(m2.entries) != memoryCap {
		t.Fatalf("重启加载条数 = %d, want %d", len(m2.entries), memoryCap)
	}
}

func TestMemoryConcurrentAppend(t *testing.T) {
	dir := t.TempDir()
	m := NewManagerMemory(dir)

	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				if err := m.Append(MemoryEntry{Channel: "hub", Input: "并发"}); err != nil {
					t.Errorf("Append: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	if len(m.entries) != 100 {
		t.Fatalf("并发写入条数 = %d, want 100", len(m.entries))
	}
	m2 := NewManagerMemory(dir)
	if len(m2.entries) != 100 {
		t.Fatalf("落盘条数 = %d, want 100", len(m2.entries))
	}
}

func TestMemoryAppendPersistence(t *testing.T) {
	// 追加路径（未触发重写）也要落盘，重启可见
	dir := t.TempDir()
	m := NewManagerMemory(dir)
	if err := m.Append(MemoryEntry{Channel: "hub", Input: "持久化"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	m2 := NewManagerMemory(dir)
	got := m2.Recent(1)
	if len(got) != 1 || got[0].Input != "持久化" {
		t.Fatalf("重启后应读到追加条目: %v", got)
	}
	if got[0].Ts <= 0 {
		t.Fatal("落盘 Ts 未补")
	}
}

func TestMemoryAppendAfterCrashTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.jsonl")
	// 模拟崩溃残尾：合法单行 JSON 但缺尾换行
	if err := os.WriteFile(path, []byte(`{"ts":1,"channel":"hub","input":"残尾"}`), 0o600); err != nil {
		t.Fatalf("写残尾文件: %v", err)
	}
	m := NewManagerMemory(dir)
	if got := m.Recent(1); len(got) != 1 || got[0].Input != "残尾" {
		t.Fatalf("残尾文件应可解析一条: %v", got)
	}

	// 追加前应先补换行，否则新行会粘在残尾上
	if err := m.Append(MemoryEntry{Channel: "hub", Input: "补换行后追加"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// 重新加载：两条都可解析
	m2 := NewManagerMemory(dir)
	got := m2.Recent(10)
	if len(got) != 2 {
		t.Fatalf("补换行后应有 2 条, got %d", len(got))
	}
	if got[0].Input != "补换行后追加" || got[1].Input != "残尾" {
		t.Fatalf("加载内容错误: %v", got)
	}
}
