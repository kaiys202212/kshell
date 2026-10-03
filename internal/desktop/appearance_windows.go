//go:build windows

package desktop

import (
	"context"
	"unsafe"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
)

var (
	dwmapi                    = windows.NewLazySystemDLL("dwmapi.dll")
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
)

const dwmwaUseImmersiveDarkMode = 20

// applyNativeTheme 让窗口边框/阴影匹配明暗，并设置窗口背景色避免切换瞬间闪烁。
// 主窗口标题固定为 "kshell"（main.go），用既有 findWindowByTitle 取句柄。
func applyNativeTheme(ctx context.Context, resolved string) {
	dark := resolved != "light"
	if hwnd := findWindowByTitle("kshell"); hwnd != 0 {
		v := int32(0)
		if dark {
			v = 1
		}
		procDwmSetWindowAttribute.Call(
			hwnd,
			uintptr(dwmwaUseImmersiveDarkMode),
			uintptr(unsafe.Pointer(&v)),
			unsafe.Sizeof(v),
		)
	}
	if ctx == nil {
		return
	}
	if dark {
		runtime.WindowSetBackgroundColour(ctx, 13, 17, 23, 255)
	} else {
		runtime.WindowSetBackgroundColour(ctx, 244, 246, 248, 255)
	}
}
