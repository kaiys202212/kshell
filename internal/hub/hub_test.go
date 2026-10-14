package hub

import (
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
