//go:build windows

package desktop

import (
	"path/filepath"
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
		name    string
		exeName string
		wantSub string
		notWant string
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

func TestDesktopPeerFilterSelectsCommandLine(t *testing.T) {
	// peer 枚举须一并取 CommandLine，供 isGuiPeer 白名单区分 GUI 实例与助手进程。
	cmd := desktopPeerFilterCmd()
	filter := cmd.Args[len(cmd.Args)-1]
	if !strings.Contains(filter, "Select-Object ProcessId,CommandLine") {
		t.Fatalf("WQL 管道应取 ProcessId,CommandLine: %s", filter)
	}
}

func TestIsGuiPeer(t *testing.T) {
	// 升级清场白名单：仅 GUI 实例可被等待/强杀；agent 会话的长效助手
	// （mcp-archive / mcp-ssh / agent-hook / --ssh-askpass）一律豁免——
	// 改名交换后它们已不阻塞任何替换步骤，杀它们纯属破坏。
	exe := `C:\Program Files\kshell\kshell-desktop.exe`
	tests := []struct {
		name    string
		cmdline string
		want    bool
	}{
		{name: "空命令行保守视为 GUI", cmdline: "", want: true},
		{name: "纯路径（含空格无引号）为 GUI", cmdline: exe, want: true},
		{name: "带引号纯路径为 GUI", cmdline: `"` + exe + `"`, want: true},
		{name: "相对名纯路径为 GUI", cmdline: "kshell-desktop.exe", want: true},
		{name: "--after-update 为 GUI", cmdline: exe + " --after-update", want: true},
		{name: "mcp-archive 助手不杀", cmdline: exe + " mcp-archive --url http://127.0.0.1:9", want: false},
		{name: "mcp-ssh 助手不杀", cmdline: exe + " mcp-ssh --stdio", want: false},
		{name: "agent-hook 助手不杀", cmdline: exe + " agent-hook --payload x", want: false},
		{name: "ssh-askpass 助手不杀", cmdline: exe + " --ssh-askpass", want: false},
		{name: "任意其他参数一律不杀", cmdline: exe + " --whatever", want: false},
		{name: "引号路径 + 助手参数不杀", cmdline: `"` + exe + `" mcp-archive --url x`, want: false},
		// 判定顺序锁定：助手标记先于/后于纯路径判定都不得误伤含 mcp- 的安装目录
		{name: "安装目录含 mcp- 的纯路径仍为 GUI", cmdline: `C:\mcp-tools\` + filepath.Base(exe), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isGuiPeer(tt.cmdline, filepath.Base(exe)); got != tt.want {
				t.Fatalf("isGuiPeer(%q) = %v, want %v", tt.cmdline, got, tt.want)
			}
		})
	}
}

func TestParsePeerOutput(t *testing.T) {
	gui := `C:\Program Files\kshell\kshell-desktop.exe`
	name := filepath.Base(gui)
	tests := []struct {
		name string
		out  string
		self int
		want []int
	}{
		{name: "空输出"},
		{name: "GUI 实例保留", out: "101 " + gui + "\r\n", want: []int{101}},
		{name: "自身剔除", out: "7 " + gui + "\r\n", self: 7},
		{name: "助手进程剔除", out: "101 " + gui + " mcp-archive --url x\r\n202 " + gui + " agent-hook\r\n"},
		{name: "GUI 与助手混合只留 GUI", out: "101 " + gui + "\r\n202 " + gui + " --ssh-askpass\r\n303 " + gui + "\r\n", want: []int{101, 303}},
		{name: "CommandLine 为空视为 GUI", out: "505 \r\n", want: []int{505}},
		{name: "非数字行跳过", out: "abc\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parsePeerOutput([]byte(tt.out), tt.self, name)
			if len(got) != len(tt.want) {
				t.Fatalf("parsePeerOutput = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("parsePeerOutput = %v, want %v", got, tt.want)
				}
			}
		})
	}
}
