package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
)

// writeSnapshot 造一个内容可辨识的快照文件（工作区路径用作断言标记）。
func writeSnapshot(t *testing.T, path, wsPath string) {
	t.Helper()
	sessions := []providers.Session{{ID: "cached-1", ToolID: "claude", Workspace: wsPath, Title: "上次的会话"}}
	if err := discovery.SaveSnapshot(path, &discovery.Result{Sessions: sessions, Workspaces: discovery.GroupSessions(sessions)}, nil); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
}

// TestLoadSnapshotServesCachedResult 验证「打开即见上次结果」：不跑扫描就有数据，
// 但不算已完成首轮真实扫描（新工作区/会话仍需扫描补上）。
func TestLoadSnapshotServesCachedResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	writeSnapshot(t, path, `D:\cached-ws`)

	app, _, _ := newTestApp(t)
	app.opts.SnapshotPath = path
	app.loadSnapshot()

	got := app.GetWorkspaces()
	if len(got) != 1 || got[0].Path != `D:\cached-ws` {
		t.Fatalf("快照应立即可用（秒开）, got %+v", got)
	}
	if sessions := app.GetSessions(); len(sessions) != 1 || sessions[0].ID != "cached-1" {
		t.Fatalf("GetSessions 应返回快照会话, got %+v", sessions)
	}

	app.mu.Lock()
	scanned := app.scanned
	app.mu.Unlock()
	if scanned {
		t.Fatalf("快照不能被当作已完成首轮扫描，否则启动后新出现的工作区会被判为不存在")
	}
}

// TestLoadSnapshotIgnoresBrokenCache 验证缓存坏了不影响启动：只是没有秒开数据。
func TestLoadSnapshotIgnoresBrokenCache(t *testing.T) {
	dir := t.TempDir()
	corrupt := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{oops"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	for _, path := range []string{corrupt, filepath.Join(dir, "missing.json"), ""} {
		app, _, _ := newTestApp(t)
		app.opts.SnapshotPath = path
		app.loadSnapshot() // 不应 panic、不应报错
		if got := app.GetWorkspaces(); len(got) != 0 {
			t.Fatalf("损坏/缺失快照不应产生结果: %+v", got)
		}
	}
}

// TestLoadSnapshotDoesNotOverrideFreshResult 验证运行期间不会被旧快照回灌。
func TestLoadSnapshotDoesNotOverrideFreshResult(t *testing.T) {
	app, _, _ := newTestApp(t)
	app.runScan() // 本进程已扫出 D:\ws-a

	path := filepath.Join(t.TempDir(), "snapshot.json")
	writeSnapshot(t, path, `D:\stale-ws`)
	app.opts.SnapshotPath = path
	app.loadSnapshot()

	got := app.GetWorkspaces()
	if len(got) != 1 || got[0].Path != `D:\ws-a` {
		t.Fatalf("已有扫描结果时不应被快照覆盖, got %+v", got)
	}
}

// TestRunScanWritesSnapshot 验证扫描成功后落盘快照（下次启动秒开的数据来源）。
func TestRunScanWritesSnapshot(t *testing.T) {
	app, _, _ := newTestApp(t)
	path := filepath.Join(t.TempDir(), "snapshot.json")
	app.opts.SnapshotPath = path

	app.runScan()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("扫描成功应写出快照: %v", err)
	}
	res, _, err := discovery.LoadSnapshot(path)
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if len(res.Sessions) != 1 || len(res.Workspaces) != 1 {
		t.Fatalf("快照应含本次扫描结果, got %+v", res)
	}

	app.mu.Lock()
	scanned := app.scanned
	app.mu.Unlock()
	if !scanned {
		t.Fatalf("真实扫描完成应标记 scanned")
	}
}

// TestRunScanFailureWritesNoSnapshot 验证扫描失败不写快照、不算已完成首扫。
func TestRunScanFailureWritesNoSnapshot(t *testing.T) {
	app, _, _ := newTestApp(t)
	path := filepath.Join(t.TempDir(), "snapshot.json")
	app.opts.SnapshotPath = path
	app.opts.Scan = func(string, []providers.Provider, string, discovery.ScanOptions) (*discovery.Result, error) {
		return nil, errors.New("boom")
	}

	app.runScan()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("扫描失败不应写快照")
	}
	app.mu.Lock()
	scanned := app.scanned
	app.mu.Unlock()
	if scanned {
		t.Fatalf("扫描失败不应标记 scanned")
	}
}

// TestGetToolsWaitsForFirstScan 验证工具表在首扫完成前不会被读成空：
// 前端工作区页签只在挂载时取一次工具，「选择 agent」下拉因此会一直禁用。
func TestGetToolsWaitsForFirstScan(t *testing.T) {
	app, _, _ := newTestApp(t)

	app.mu.Lock()
	empty := len(app.tools) == 0
	app.mu.Unlock()
	if !empty {
		t.Fatalf("前置条件：测试 App 初始不应有工具表")
	}

	if got := app.GetTools(); len(got) == 0 {
		t.Fatalf("GetTools 应等待首轮扫描并返回工具表（含未安装项），got %+v", got)
	}
}

// TestEnsureScanReadyWaitsAfterSnapshotRestore 验证快照恢复后仍会等首轮真实扫描：
// 否则启动瞬间用旧快照查新工作区/会话会误判为不存在。
func TestEnsureScanReadyWaitsAfterSnapshotRestore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	writeSnapshot(t, path, `D:\cached-ws`)

	app, _, _ := newTestApp(t)
	app.opts.SnapshotPath = path
	app.loadSnapshot()

	app.ensureScanReady()

	app.mu.Lock()
	scanned := app.scanned
	app.mu.Unlock()
	if !scanned {
		t.Fatalf("快照恢复后 ensureScanReady 应触发并等到真实扫描完成")
	}
	if got := app.GetWorkspaces(); len(got) != 1 || got[0].Path != `D:\ws-a` {
		t.Fatalf("等待结束后应拿到真实扫描结果（覆盖快照）, got %+v", got)
	}
}
