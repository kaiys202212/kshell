//go:build !windows

package clisync

import "errors"

// 非 Windows 平台的占位：ensurePath 的运行时分发只在 Windows 上调用它，
// 这里仅为符号可编译。真实读取走 readUserPathEntries（PATH 环境变量）。

func ensurePathWindows(binDir string) (string, error) {
	return stateError, errors.New("clisync: ensurePathWindows 仅限 Windows")
}

// readUserPathEntries 非 Windows 下读当前进程 PATH。
func readUserPathEntries() []string {
	return splitPathEnv(currentPathEnv())
}
