package desktop

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yangk/kshell/internal/config"
)

func repairTestApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	return NewAppWith(Options{
		Layout: config.Layout{Root: root, State: filepath.Join(root, "state")},
	})
}

// 卸载成功写标记；安装成功清标记。
func TestUninstallMarkerLifecycle(t *testing.T) {
	a := repairTestApp(t)
	if a.uninstalledByUser("cursor") {
		t.Fatal("初始不应有卸载标记")
	}
	a.setUninstalledMarker("cursor")
	if !a.uninstalledByUser("cursor") {
		t.Fatal("写标记后应读到")
	}
	a.clearUninstalledMarker("cursor")
	if a.uninstalledByUser("cursor") {
		t.Fatal("清标记后不应读到")
	}
}

// 重试计数：递增、读取、清零。
func TestRepairAttemptsLifecycle(t *testing.T) {
	a := repairTestApp(t)
	if n := a.readRepairAttempts("cursor"); n != 0 {
		t.Fatalf("初始 attempts=%d", n)
	}
	a.bumpRepairAttempts("cursor")
	a.bumpRepairAttempts("cursor")
	if n := a.readRepairAttempts("cursor"); n != 2 {
		t.Fatalf("attempts=%d, want 2", n)
	}
	a.clearRepairState("cursor")
	if n := a.readRepairAttempts("cursor"); n != 0 {
		t.Fatalf("清零后 attempts=%d", n)
	}
}

// 损坏的状态文件视为 0，不阻塞修复流程。
func TestRepairAttemptsCorruptFile(t *testing.T) {
	a := repairTestApp(t)
	if err := os.MkdirAll(a.opts.Layout.State, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(a.opts.Layout.State, "cursor-repair.json")
	if err := os.WriteFile(path, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if n := a.readRepairAttempts("cursor"); n != 0 {
		t.Fatalf("损坏文件应视为 0，got %d", n)
	}
}

// Layout.State 未装配（部分测试的零值 Layout）：标记与计数全部静默 no-op。
func TestRepairStateWithoutLayout(t *testing.T) {
	a := NewAppWith(Options{})
	a.setUninstalledMarker("cursor")
	if a.uninstalledByUser("cursor") {
		t.Fatal("无 State 目录不应读到标记")
	}
	a.bumpRepairAttempts("cursor")
	if n := a.readRepairAttempts("cursor"); n != 0 {
		t.Fatalf("无 State 目录 attempts 应恒为 0，got %d", n)
	}
}
