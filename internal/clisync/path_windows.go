//go:build windows

package clisync

import (
	"errors"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// ensurePathWindows 把 binDir 追加进用户级 PATH（HKCU\Environment\Path）。
// 已存在（不分大小写）返回 ready；否则追加并保留原值类型写回，再广播
// WM_SETTINGCHANGE 让新进程能看到。已开着的终端不生效（Windows 语义）。
func ensurePathWindows(binDir string) (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return stateError, err
	}
	defer k.Close()
	val, valType, err := k.GetStringValue("Path")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return stateError, err
	}
	if containsPathEntry(splitPathEnv(val), binDir) {
		return pathReady, nil
	}
	newVal := binDir
	if val != "" {
		newVal = strings.TrimSuffix(val, string(os.PathListSeparator)) + string(os.PathListSeparator) + binDir
	}
	// 原值是 REG_EXPAND_SZ（含 %VAR%）必须保型；全新值也用 EXPAND_SZ 更稳。
	if valType == registry.SZ {
		err = k.SetStringValue("Path", newVal)
	} else {
		err = k.SetExpandStringValue("Path", newVal)
	}
	if err != nil {
		return stateError, err
	}
	broadcastEnvChange()
	return pathAdded, nil
}

// readUserPathEntries 读用户级 PATH（注册表），供 PATH 体检。
func readUserPathEntries() []string {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	val, _, err := k.GetStringValue("Path")
	if err != nil {
		return nil
	}
	return splitPathEnv(val)
}

// broadcastEnvChange 广播环境变量变更：Explorer 等收到后，新启动的进程才能
// 看到新 PATH。失败静默（最坏情况：用户重开终端前看不到，不影响功能）。
func broadcastEnvChange() {
	user32 := windows.NewLazySystemDLL("user32.dll")
	proc := user32.NewProc("SendMessageTimeoutW")
	const (
		hwndBroadcast   = uintptr(0xFFFF)
		wmSettingChange = 0x001A
		smtoAbortIfHung = 0x0002
	)
	name, _ := windows.UTF16PtrFromString("Environment")
	_, _, _ = proc.Call(
		hwndBroadcast,
		wmSettingChange,
		0,
		uintptr(unsafe.Pointer(name)),
		smtoAbortIfHung,
		2000,
		0,
	)
}
