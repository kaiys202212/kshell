//go:build !windows

package desktop

import "context"

// applyNativeTheme 非 Windows 无原生窗口着色实现。
func applyNativeTheme(context.Context, string) {}
