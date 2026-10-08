// Wails 桌面版入口（根包 main）。TUI 入口在 cmd/kshell，两者互不影响。
// 注意：本文件不加构建标签——wails build 的绑定生成阶段会剔除 desktop 等标签，
// 根包必须无条件可编译；frontend/dist 由 .gitkeep 占位保证 embed 始终可解析。
// 平台差异逻辑一律放 internal/desktop（托盘、关闭行为），这里只做装配。
package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/yangk/kshell/internal/agenthook"
	"github.com/yangk/kshell/internal/cursoragent"
	"github.com/yangk/kshell/internal/desktop"
	"github.com/yangk/kshell/internal/mcparchive"
	"github.com/yangk/kshell/internal/remote"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIconPNG []byte

//go:embed build/windows/icon.ico
var trayIconICO []byte

// trayIcon 按平台选择托盘图标：Windows 通知区只认 ICO（见 energye/systray Windows 实现），
// 其它平台（Linux 走 dbus）用 PNG。
func trayIcon() []byte {
	if runtime.GOOS == "windows" {
		return trayIconICO
	}
	return appIconPNG
}

// desktopApp 是暴露给前端的绑定对象（Task 3 起逐步实现各方法）；
// 托盘图标在装配期经 Options 注入（embed 数据在包初始化时即可用）。
var desktopApp = desktop.NewAppWith(desktop.Options{TrayIcon: trayIcon()})

// RunDesktop 用给定的前端资源启动 Wails 窗口（拆出来便于测试）。
//
// Frameless：标题栏由前端自绘（页签直接排进标题栏）。Wails v2 默认走
// framelessWithDecorations 分支（DWM 扩展边框进客户区），因此缩放边框、系统阴影、
// 最小化/最大化动画都保留，只是标题栏内容归前端——窗口拖拽靠 CSS
// --wails-draggable:drag（见 style.css 的 .kshell-drag）。
func RunDesktop(src fs.FS) error {
	return wails.Run(&options.App{
		Title:     "kshell",
		Width:     1280,
		Height:    800,
		MinWidth:  960,
		MinHeight: 640,
		Frameless: true,
		// 默认最大化启动（2026-10-04 优化轮）
		WindowStartState: options.Maximised,
		AssetServer: &assetserver.Options{
			Assets: src,
		},
		OnStartup:     desktopApp.Startup,
		OnBeforeClose: desktopApp.BeforeClose,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "kshell-desktop",
			OnSecondInstanceLaunch: desktopApp.OnSecondInstanceLaunch,
		},
		// 退出时收掉所有内嵌终端，避免 pty 里的 CLI 子进程残留
		OnShutdown: desktopApp.Shutdown,
		Bind: []interface{}{
			desktopApp,
		},
	})
}

func main() {
	// SSH ASKPASS：OpenSSH 会再拉起本进程回填密码；必须在一切初始化之前退出。
	if remote.TryAskPassMain(os.Args) {
		return
	}
	// agent-hook 隐藏子命令：agent CLI 的 hook/notify 会拉起本进程投递事件。
	// 必须放在一切初始化之前直接返回，绝不初始化 Wails / 拉起第二个窗口；
	// Handle 恒返回 0，hook 非零退出会向 agent 报错、打扰对话。
	if len(os.Args) >= 2 && os.Args[1] == "agent-hook" {
		os.Exit(agenthook.Handle(os.Args[1:]))
	}
	// 必须在 Wails 单实例锁之前返回，否则作为 MCP / cursor-agent 代理启动会变成第二次拉起桌面窗口。
	if cursoragent.ShouldProxy() {
		os.Exit(cursoragent.Main())
	}
	if len(os.Args) > 1 && os.Args[1] == "mcp-archive" {
		if err := mcparchive.Main(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	// 升级拉起时在单实例锁之前清场（兼容旧版替换脚本只 Start-Process 的路径）。
	_ = desktop.PrepareLaunch(os.Args)
	if err := RunDesktop(assets); err != nil {
		fmt.Fprintln(os.Stderr, "kshell 桌面版启动失败:", err)
		os.Exit(1)
	}
}
