package autostart

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// useTestKey 把 Run 键路径切换到测试专用键（HKCU 下无需管理员权限），测试结束清理。
func useTestKey(t *testing.T) {
	t.Helper()
	orig := runKeyPath
	testPath := "Software\\kshell-autostart-test"
	runKeyPath = testPath
	t.Cleanup(func() {
		runKeyPath = orig
		_ = registry.DeleteKey(registry.CURRENT_USER, testPath)
	})
}

func TestSupported(t *testing.T) {
	if !Supported() {
		t.Fatal("windows 上 Supported() 应为 true")
	}
}

func TestEnableWritesQuotedCommand(t *testing.T) {
	useTestKey(t)
	exe := `C:\Program Files\kshell\kshell.exe` // 故意带空格，验证引号包裹
	if err := Enable(exe); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	on, err := Enabled()
	if err != nil {
		t.Fatalf("Enabled: %v", err)
	}
	if !on {
		t.Fatal("Enable 后 Enabled 应为 true")
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		t.Fatalf("打开测试键: %v", err)
	}
	defer k.Close()
	val, _, err := k.GetStringValue(ValueName)
	if err != nil {
		t.Fatalf("读值: %v", err)
	}
	want := `"` + exe + `" --autostart-hidden`
	if val != want {
		t.Fatalf("值 = %q, want %q", val, want)
	}
}

func TestDisableRemovesValue(t *testing.T) {
	useTestKey(t)
	if err := Enable(`C:\kshell\kshell.exe`); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if err := Disable(); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	on, err := Enabled()
	if err != nil {
		t.Fatalf("Enabled: %v", err)
	}
	if on {
		t.Fatal("Disable 后 Enabled 应为 false")
	}
}

func TestDisableIdempotent(t *testing.T) {
	useTestKey(t)
	// 值不存在时 Disable 不报错（幂等），连键都没建过也不报错。
	if err := Disable(); err != nil {
		t.Fatalf("Disable（未 Enable 过）: %v", err)
	}
	if err := Enable(`C:\kshell\kshell.exe`); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if err := Disable(); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if err := Disable(); err != nil {
		t.Fatalf("重复 Disable: %v", err)
	}
}

func TestEnableOverwritesExistingValue(t *testing.T) {
	useTestKey(t)
	if err := Enable(`C:\old\kshell.exe`); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if err := Enable(`D:\new path\kshell.exe`); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		t.Fatalf("打开测试键: %v", err)
	}
	defer k.Close()
	val, _, err := k.GetStringValue(ValueName)
	if err != nil {
		t.Fatalf("读值: %v", err)
	}
	if !strings.HasPrefix(val, `"D:\new path\kshell.exe"`) {
		t.Fatalf("重复 Enable 未覆盖旧值: %q", val)
	}
}
