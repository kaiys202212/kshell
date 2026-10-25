//go:build windows

// Package autostart 封装「开机自启动」的注册表读写：Windows 操作
// HKCU\Software\Microsoft\Windows\CurrentVersion\Run，其它平台整体不支持
//（other.go 返回 ErrUnsupported，由调用方在 UI 层整体隐藏）。
// 只做注册表这一件事，不碰配置文件——config.Autostart 与注册表的对齐
// 由 internal/desktop 的 Startup 同步完成。
package autostart

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// runKeyPath 是自启动注册表键路径（HKCU 下），包级变量便于测试注入
// 测试专用键（Software\kshell-autostart-test），真读写注册表验证后清理。
var runKeyPath = "Software\\Microsoft\\Windows\\CurrentVersion\\Run"

// ValueName 是本应用在 Run 键下的注册表值名。
const ValueName = "kshell-desktop"

// Supported 报告当前平台是否支持开机自启动。
func Supported() bool { return true }

// Enable 写入自启动注册表值：数据 = "exePath" --autostart-hidden。
// 路径必须双引号包裹（可能含空格），静默参数让桌面版启动即收进托盘。
func Enable(exePath string) error {
	// CreateKey 对已存在的键等价于按权限打开（真实 Run 键常驻）；注入的测试键
	// 首次不存在时由这里创建，测试结束再清理。
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开自启动注册表键失败: %w", err)
	}
	defer key.Close()
	// 手工拼引号而非 %q：%q 会把路径里的 \ 转义成 \\，注册表命令行数据必须保持原生反斜杠。
	if err := key.SetStringValue(ValueName, fmt.Sprintf(`"%s" --autostart-hidden`, exePath)); err != nil {
		return fmt.Errorf("写入自启动注册表值失败: %w", err)
	}
	return nil
}

// Disable 删除自启动注册表值；值或键不存在（已关闭过）不算错误。
func Disable() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("打开自启动注册表键失败: %w", err)
	}
	defer key.Close()
	if err := key.DeleteValue(ValueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("删除自启动注册表值失败: %w", err)
	}
	return nil
}

// Enabled 报告自启动注册表值是否存在（不做内容比对，存在即视为开启）。
func Enabled() (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("打开自启动注册表键失败: %w", err)
	}
	defer key.Close()
	if _, _, err := key.GetStringValue(ValueName); err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("读取自启动注册表值失败: %w", err)
	}
	return true, nil
}
