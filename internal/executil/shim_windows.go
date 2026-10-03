//go:build windows

package executil

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolveShim 处理 Windows 上的命令包装脚本。
// npm 全局安装的 CLI（codex、opencode…）在 PATH 上常是 .ps1，不能直接 exec：
// 优先用同目录的 .cmd 兄弟文件，没有就套一层 powershell -Command。
//
// 放在 executil 是为了让 providers（扫描时要调工具自身 CLI，如 opencode db）与
// launcher（启动会话）共用同一套解析，避免两份实现走偏。
func ResolveShim(binPath string, args []string) (string, []string) {
	if !strings.EqualFold(filepath.Ext(binPath), ".ps1") {
		return binPath, args
	}

	cmdSibling := strings.TrimSuffix(binPath, filepath.Ext(binPath)) + ".cmd"
	if info, err := os.Stat(cmdSibling); err == nil && !info.IsDir() {
		return cmdSibling, args
	}

	// 参数必须写进 -Command 脚本内部，否则会被 PowerShell 当成自己的参数吃掉。
	script := "& " + quotePS(binPath)
	for _, a := range args {
		script += " " + quotePS(a)
	}
	return "powershell", []string{"-NoProfile", "-Command", script}
}

// quotePS 用单引号包裹并转义内部单引号：避免引入双引号，减少 exec 的二次转义干扰。
func quotePS(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
