//go:build windows

package desktop

import (
	"strings"
	"testing"
)

func TestSingleInstanceMutexNameMatchesWails(t *testing.T) {
	// Wails: id="wails-app-"+uniqueId; mutexName=id+"sim"；UniqueId=kshell-desktop
	if singleInstanceMutexName != "wails-app-kshell-desktopsim" {
		t.Fatalf("got %q", singleInstanceMutexName)
	}
}

func TestDesktopPeerFilterUsesExecutableName(t *testing.T) {
	// exe 改名安装（如 kshell-desktop1.exe）时，peer 枚举必须按实际安装名过滤，
	// 否则残留实例/助手清不掉，新进程被单实例锁挡住静默退出。
	orig := desktopExeName
	defer func() { desktopExeName = orig }()

	tests := []struct {
		name     string
		exeName  string
		wantSub  string
		notWant  string
	}{
		{name: "改名安装按实际名过滤", exeName: "kshell-desktop1.exe", wantSub: "Name='kshell-desktop1.exe'", notWant: "Name='kshell-desktop.exe'"},
		{name: "名字含单引号成对转义", exeName: `my'app.exe`, wantSub: "Name='my''app.exe'"},
		{name: "默认名", exeName: "kshell-desktop.exe", wantSub: "Name='kshell-desktop.exe'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desktopExeName = func() string { return tt.exeName }
			cmd := desktopPeerFilterCmd()
			filter := cmd.Args[len(cmd.Args)-1]
			if !strings.Contains(filter, tt.wantSub) {
				t.Fatalf("WQL 应包含 %q, got %q", tt.wantSub, filter)
			}
			if tt.notWant != "" && strings.Contains(filter, tt.notWant) {
				t.Fatalf("WQL 不应再按旧名过滤, got %q", filter)
			}
		})
	}
}

func TestDesktopExeNameDefaultIsBareFileName(t *testing.T) {
	// 默认实现取 os.Executable() 的 base name；测试二进制本身在 Windows 上
	// 也应得到不带路径的文件名。
	got := desktopExeName()
	if got == "" || strings.ContainsAny(got, `/\`) {
		t.Fatalf("desktopExeName 应为不带路径的文件名, got %q", got)
	}
}
