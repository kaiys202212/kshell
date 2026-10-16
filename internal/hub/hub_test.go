package hub

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ts 生成指定年月日（UTC 中午）的 UnixNano，避免时区把测试跨月。
func ts(y int, mo time.Month, d int) int64 {
	return time.Date(y, mo, d, 12, 0, 0, 0, time.UTC).UnixNano()
}

func newTestStore(t *testing.T, cap int) *Store {
	t.Helper()
	s := New(t.TempDir(), cap)
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return s
}

func TestAppendAndList(t *testing.T) {
	s := newTestStore(t, 500)
	if err := s.Append(Message{Kind: "user", Text: "hi", ChatID: "c1", Tool: "claude", Workspace: `D:\w`}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got := s.List()
	if len(got) != 1 {
		t.Fatalf("List 应返回 1 条，得到 %d", len(got))
	}
	m := got[0]
	if m.Kind != "user" || m.Text != "hi" || m.ChatID != "c1" || m.Tool != "claude" || m.Workspace != `D:\w` {
		t.Fatalf("字段未原样保留: %+v", m)
	}
	if m.ID != 1 {
		t.Fatalf("首条 ID 应为 1，得到 %d", m.ID)
	}
	if m.Ts == 0 {
		t.Fatalf("Ts 应被自动填充")
	}
}

func TestListEmptyNonNil(t *testing.T) {
	s := newTestStore(t, 10)
	if got := s.List(); got == nil {
		t.Fatalf("空 List 不应为 nil")
	}
	if got := s.ListArchive(); got == nil {
		t.Fatalf("空 ListArchive 不应为 nil")
	}
}

func TestIDsMonotonic(t *testing.T) {
	s := newTestStore(t, 10)
	for i := 0; i < 5; i++ {
		if err := s.Append(Message{Kind: "user", Text: fmt.Sprintf("m%d", i), Ts: ts(2026, 10, 15)}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	ids := map[int64]bool{}
	for _, m := range s.List() {
		if ids[m.ID] {
			t.Fatalf("ID 重复: %d", m.ID)
		}
		ids[m.ID] = true
	}
}

func TestOverflowArchivesOldest(t *testing.T) {
	s := newTestStore(t, 3)
	for i := 0; i < 5; i++ {
		if err := s.Append(Message{Kind: "user", Text: fmt.Sprintf("m%d", i), Ts: ts(2026, 10, 15)}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	live := s.List()
	if len(live) != 3 {
		t.Fatalf("live 应保留 3 条，得到 %d", len(live))
	}
	if live[0].Text != "m2" || live[2].Text != "m4" {
		t.Fatalf("live 应保留最新 3 条，得到 %q..%q", live[0].Text, live[2].Text)
	}
	arch, err := s.LoadArchive("2026-10")
	if err != nil {
		t.Fatalf("LoadArchive: %v", err)
	}
	if len(arch) != 2 || arch[0].Text != "m0" || arch[1].Text != "m1" {
		t.Fatalf("归档应含最旧两条 m0/m1，得到 %+v", arch)
	}
	months := s.ListArchive()
	if len(months) != 1 || months[0].Month != "2026-10" || months[0].Count != 2 {
		t.Fatalf("ListArchive 应返回 2026-10/2，得到 %+v", months)
	}
}

func TestArchiveByNaturalMonth(t *testing.T) {
	s := newTestStore(t, 2)
	days := []struct {
		mo time.Month
		d  int
	}{
		{time.October, 10}, {time.October, 11},
		{time.November, 1}, {time.November, 2}, {time.November, 3},
	}
	for i, v := range days {
		if err := s.Append(Message{Kind: "user", Text: fmt.Sprintf("m%d", i), Ts: ts(2026, v.mo, v.d)}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	oct, err := s.LoadArchive("2026-10")
	if err != nil {
		t.Fatalf("LoadArchive oct: %v", err)
	}
	if len(oct) != 2 || oct[0].Text != "m0" || oct[1].Text != "m1" {
		t.Fatalf("10 月归档应为 m0/m1，得到 %+v", oct)
	}
	nov, err := s.LoadArchive("2026-11")
	if err != nil {
		t.Fatalf("LoadArchive nov: %v", err)
	}
	if len(nov) != 1 || nov[0].Text != "m2" {
		t.Fatalf("11 月归档应为 m2，得到 %+v", nov)
	}
	months := s.ListArchive()
	if len(months) != 2 || months[0].Month != "2026-11" || months[1].Month != "2026-10" {
		t.Fatalf("归档月份应倒序（新在前），得到 %+v", months)
	}
}

func TestArchiveOnlyGrows(t *testing.T) {
	s := newTestStore(t, 2)
	for i := 0; i < 4; i++ {
		if err := s.Append(Message{Kind: "user", Text: fmt.Sprintf("m%d", i), Ts: ts(2026, 10, 15)}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	arch, err := s.LoadArchive("2026-10")
	if err != nil {
		t.Fatalf("LoadArchive: %v", err)
	}
	if len(arch) != 2 || arch[0].Text != "m0" || arch[1].Text != "m1" {
		t.Fatalf("归档应累积 m0/m1 不被覆盖，得到 %+v", arch)
	}
}

func TestLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, 500)
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := s.Append(Message{Kind: "agent", Text: "done", TermKey: "session:s1", Event: "Stop"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := s.Append(Message{Kind: "user", Text: "hi"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	s2 := New(dir, 500)
	if err := s2.Load(); err != nil {
		t.Fatalf("Load2: %v", err)
	}
	got := s2.List()
	if len(got) != 2 || got[0].Text != "done" || got[1].Text != "hi" {
		t.Fatalf("重载后应保留两条，得到 %+v", got)
	}
}

func TestLoadCorruptIsLenient(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "live.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(dir, 10)
	if err := s.Load(); err != nil {
		t.Fatalf("损坏文件不应报错，得到 %v", err)
	}
	if len(s.List()) != 0 {
		t.Fatalf("损坏文件应按空处理")
	}
}

func TestNextIDWatermarkAcrossArchive(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, 2)
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := s.Append(Message{Kind: "user", Text: fmt.Sprintf("m%d", i), Ts: ts(2026, 10, 15)}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	s2 := New(dir, 2)
	if err := s2.Load(); err != nil {
		t.Fatalf("Load2: %v", err)
	}
	if err := s2.Append(Message{Kind: "user", Text: "next", Ts: ts(2026, 10, 16)}); err != nil {
		t.Fatalf("Append next: %v", err)
	}
	got := s2.List()
	last := got[len(got)-1]
	if last.ID != 4 {
		t.Fatalf("归档后新消息 ID 应为 4（水位恢复），得到 %d", last.ID)
	}
}

// 归档幂等：同一批消息再次送入 archive 不产生重复条目
// （writeLive 失败 + 重启重载的极端序列会重归档同一批）。
func TestArchiveIdempotentByID(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, 2)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if err := s.Append(Message{Kind: "user", Text: fmt.Sprintf("m%d", i), Ts: ts(2026, 10, 15)}); err != nil {
			t.Fatal(err)
		}
	}
	// 模拟重载后把已归档的同一批再送一次（直接调 archive 内部路径）
	again := []Message{
		{ID: 1, Kind: "user", Text: "m0", Ts: ts(2026, 10, 15)},
		{ID: 2, Kind: "user", Text: "m1", Ts: ts(2026, 10, 15)},
	}
	if err := s.archive(again); err != nil {
		t.Fatal(err)
	}
	arch, err := s.LoadArchive("2026-10")
	if err != nil {
		t.Fatal(err)
	}
	if len(arch) != 2 || arch[0].ID != 1 || arch[1].ID != 2 {
		t.Fatalf("归档应保持 2 条不重复，得到 %+v", arch)
	}
}

func TestStatusRoundtrip(t *testing.T) {
	s := newTestStore(t, 10)
	if err := s.Append(Message{Kind: "user", Text: "m0", Status: StatusQueued}); err != nil {
		t.Fatal(err)
	}
	if got := s.List()[0].Status; got != StatusQueued {
		t.Fatalf("Status = %q", got)
	}
}

func TestReserveIDAndAppendWithID(t *testing.T) {
	s := newTestStore(t, 10)
	id := s.ReserveID()
	if err := s.AppendWithID(Message{ID: id, Kind: "user", Text: "预留"}); err != nil {
		t.Fatal(err)
	}
	got := s.List()
	if len(got) != 1 || got[0].ID != id {
		t.Fatalf("应保留预留 ID: %+v", got)
	}
	// 预留后正常 Append 不撞车
	if err := s.Append(Message{Kind: "user", Text: "next"}); err != nil {
		t.Fatal(err)
	}
	if got[0].ID+1 != s.List()[1].ID {
		t.Fatalf("ID 不连续: %+v", s.List())
	}
}

func TestSetStatus(t *testing.T) {
	s := newTestStore(t, 10)
	if err := s.Append(Message{Kind: "user", Text: "m0", Status: StatusQueued}); err != nil {
		t.Fatal(err)
	}
	id := s.List()[0].ID
	if !s.SetStatus(id, StatusRecalled) {
		t.Fatal("SetStatus 应命中")
	}
	if got := s.List()[0].Status; got != StatusRecalled {
		t.Fatalf("Status = %q", got)
	}
	// 持久化验证
	s2 := New(s.dir, 10)
	_ = s2.Load()
	if got := s2.List()[0].Status; got != StatusRecalled {
		t.Fatalf("重载后 Status = %q", got)
	}
	// 未知 ID 返回 false
	if s.SetStatus(999, StatusRecalled) {
		t.Fatal("未知 ID 应返回 false")
	}
}

func TestLoadArchiveMissing(t *testing.T) {
	s := newTestStore(t, 10)
	got, err := s.LoadArchive("2026-01")
	if err != nil {
		t.Fatalf("缺失月份不应报错: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("缺失月份应返回空")
	}
	if _, err := s.LoadArchive("bad"); err == nil {
		t.Fatalf("非法月份应报错")
	}
}

// TestLoadV1LiveMigratesOnNextWrite：v1 live.json 升级到 v2 时历史不得丢失。
func TestLoadV1LiveMigratesOnNextWrite(t *testing.T) {
	dir := t.TempDir()
	v1 := `{"version":1,"messages":[` +
		`{"id":1,"kind":"user","text":"old-1","ts":100},` +
		`{"id":2,"kind":"agent","text":"old-2","ts":200}]}`
	if err := os.WriteFile(filepath.Join(dir, "live.json"), []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(dir, 0)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if got := s.List(); len(got) != 2 || got[0].Text != "old-1" {
		t.Fatalf("Load 后应保留 v1 消息，got %d 条", len(got))
	}
	if err := s.Append(Message{Kind: "user", Text: "new"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "live.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	if f.Version != fileVersion {
		t.Fatalf("重写后 version = %d, want %d", f.Version, fileVersion)
	}
	if len(f.Messages) != 3 || f.Messages[0].Text != "old-1" {
		t.Fatalf("旧消息应保留，got %d 条", len(f.Messages))
	}
}

// TestLoadRejectsUnknownVersions：version=0（缺失/非本程序产出）与大于当前版本的
// live.json 都按空处理，防止误解析未知结构。
func TestLoadRejectsUnknownVersions(t *testing.T) {
	t.Run("v0", func(t *testing.T) { testLoadRejectsVersion(t, `{"messages":[{"id":1,"kind":"user","text":"x","ts":1}]}`) })
	t.Run("v0显式", func(t *testing.T) { testLoadRejectsVersion(t, `{"version":0,"messages":[{"id":1,"kind":"user","text":"x","ts":1}]}`) })
	t.Run("未来版本", func(t *testing.T) { testLoadRejectsVersion(t, `{"version":99,"messages":[{"id":1,"kind":"user","text":"x","ts":1}]}`) })
}

func testLoadRejectsVersion(t *testing.T, raw string) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "live.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(dir, 0)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if got := s.List(); len(got) != 0 {
		t.Fatalf("应按空处理，got %d 条", len(got))
	}
}
