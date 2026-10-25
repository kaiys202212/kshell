//go:build !windows

// 非 Windows 平台：开机自启动整体不支持（macOS launchd / Linux .desktop 按
// YAGNI 不做），全部接口为安全 no-op，由调用方（desktop/UI）按 Supported
// 整体隐藏功能。other.go 由 GOOS 无法在 Windows 上交叉单测，逻辑靠
// Supported 短路覆盖，不做平台断言。
package autostart

import "errors"

// ErrUnsupported 是非 Windows 平台上 Enable/Disable 返回的哨兵错误，
// 调用方可用 errors.Is 判定。
var ErrUnsupported = errors.New("autostart: 当前平台不支持开机自启动")

// Supported 报告当前平台是否支持开机自启动（恒 false）。
func Supported() bool { return false }

// Enable 恒返回 ErrUnsupported。
func Enable(exePath string) error { return ErrUnsupported }

// Disable 恒返回 ErrUnsupported。
func Disable() error { return ErrUnsupported }

// Enabled 恒为 (false, nil)：不支持平台视为未开启。
func Enabled() (bool, error) { return false, nil }
