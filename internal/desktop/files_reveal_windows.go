//go:build windows

package desktop

import "os/exec"

func revealInOS(abs string, isDir bool) error {
	cmd := exec.Command("explorer", explorerArgs(abs, isDir)...)
	// 故意不 HideWindow：CREATE_NO_WINDOW 会让 explorer 窗口表现异常（点了像没反应）
	return cmd.Start()
}

func explorerArgs(abs string, isDir bool) []string {
	p := quoteExplorerPath(abs)
	if isDir {
		return []string{p}
	}
	return []string{"/select," + p}
}

// quoteExplorerPath 路径含空格时加双引号；已有引号则原样返回。
func quoteExplorerPath(abs string) string {
	if abs == "" {
		return abs
	}
	if abs[0] == '"' {
		return abs
	}
	for _, r := range abs {
		if r == ' ' {
			return `"` + abs + `"`
		}
	}
	return abs
}
