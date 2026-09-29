package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/providers"
)

func sampleResult() *Result {
	sessions := []providers.Session{
		{ID: "s1", ToolID: "claude", Workspace: `D:\ws-a`, Title: "会话一", UpdatedAt: time.Now().Truncate(time.Second)},
		{ID: "s2", ToolID: "codex", Workspace: `D:\ws-a`, Title: "会话二", UpdatedAt: time.Now().Truncate(time.Second)},
	}
	return &Result{Sessions: sessions, Workspaces: GroupSessions(sessions)}
}

func TestSnapshotRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "snapshot.json")
	res := sampleResult()
	tools := []Tool{{ID: "claude", Name: "Claude Code", BinPath: "claude", Version: "1.2.3", Installed: true}}

	if err := SaveSnapshot(path, res, tools); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	got, gotTools, err := LoadSnapshot(path)
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if len(got.Sessions) != 2 || got.Sessions[0].ID != "s1" {
		t.Fatalf("sessions 未往返: %+v", got.Sessions)
	}
	if len(got.Workspaces) != 1 || got.Workspaces[0].Path != `D:\ws-a` || got.Workspaces[0].SessionCount != 2 {
		t.Fatalf("workspaces 未往返: %+v", got.Workspaces)
	}
	if len(gotTools) != 1 || gotTools[0].Version != "1.2.3" {
		t.Fatalf("tools 未往返: %+v", gotTools)
	}
}

func TestSaveSnapshotIgnoresEmptyInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	if err := SaveSnapshot(path, nil, nil); err != nil {
		t.Fatalf("nil result 不应报错: %v", err)
	}
	if err := SaveSnapshot("", sampleResult(), nil); err != nil {
		t.Fatalf("空路径不应报错: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("nil result 不应写文件")
	}
}

func TestLoadSnapshotRejectsBadInput(t *testing.T) {
	dir := t.TempDir()

	if _, _, err := LoadSnapshot(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatalf("缺失文件应返回 error 让调用方退回实时扫描")
	}
	if _, _, err := LoadSnapshot(""); err == nil {
		t.Fatalf("空路径应返回 error")
	}

	corrupt := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, _, err := LoadSnapshot(corrupt); err == nil {
		t.Fatalf("损坏文件应返回 error")
	}

	old := filepath.Join(dir, "old.json")
	data, _ := json.Marshal(Snapshot{Version: snapshotVersion + 1})
	if err := os.WriteFile(old, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, _, err := LoadSnapshot(old); err == nil {
		t.Fatalf("版本不符应返回 error")
	}
}

// TestLoadSnapshotNormalizesEmptyCollections 保证反序列化后切片/map 非 nil：
// 前端直接遍历它们，null 会炸。
func TestLoadSnapshotNormalizesEmptyCollections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	res := &Result{Workspaces: []Workspace{{Path: `D:\ws-a`}}} // ToolCounts 故意为 nil
	if err := SaveSnapshot(path, res, nil); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	got, tools, err := LoadSnapshot(path)
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if got.Sessions == nil || tools == nil {
		t.Fatalf("空集合应规范化为非 nil 切片: sessions=%v tools=%v", got.Sessions, tools)
	}
	if got.Workspaces[0].ToolCounts == nil {
		t.Fatalf("ToolCounts 应为空 map 而非 nil")
	}
}
