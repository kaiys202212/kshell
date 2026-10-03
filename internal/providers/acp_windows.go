//go:build windows

package providers

// npxNames：Windows 上 npx 是 .cmd 包装脚本，优先探测 .cmd。
func npxNames() []string { return []string{"npx.cmd", "npx"} }
