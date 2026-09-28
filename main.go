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
	"github.com/yangk/kshell/internal/desktop"
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
func RunDesktop(src fs.FS) error {
	return wails.Run(&options.App{
		Title:     "kshell",
		Width:     1280,
		Height:    800,
		MinWidth:  960,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: src,
		},
		OnStartup:     desktopApp.Startup,
		OnBeforeClose: desktopApp.BeforeClose,
		Bind: []interface{}{
			desktopApp,
		},
	})
}

func main() {
	if err := RunDesktop(assets); err != nil {
		fmt.Fprintln(os.Stderr, "kshell 桌面版启动失败:", err)
		os.Exit(1)
	}
}
