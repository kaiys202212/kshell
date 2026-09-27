//go:build windows

package launcher

import (
	"os"
	"path/filepath"
	"strings"
)

// Resolve 处理 Windows 上的命令包装脚本。
// npm 全局安装的 CLI（codex、opencode…）在 PATH 上是 .ps1，不能直接 exec：
// 优先用同目录的 .cmd 兄弟文件，没有就套一层 powershell -Command。
func Resolve(binPath string, args []string) Spec {
	if !strings.EqualFold(filepath.Ext(binPath), ".ps1") {
		return Spec{Path: binPath, Args: args}
	}

	cmdSibling := strings.TrimSuffix(binPath, filepath.Ext(binPath)) + ".cmd"
	if info, err := os.Stat(cmdSibling); err == nil && !info.IsDir() {
		return Spec{Path: cmdSibling, Args: args}
	}

	// 参数必须写进 -Command 脚本内部，否则会被 PowerShell 当成自己的参数吃掉。
	script := "& " + quotePS(binPath)
	for _, a := range args {
		script += " " + quotePS(a)
	}
	return Spec{Path: "powershell", Args: []string{"-NoProfile", "-Command", script}}
}

// quotePS 用单引号包裹并转义内部单引号：避免引入双引号，减少 exec 的二次转义干扰。
func quotePS(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
