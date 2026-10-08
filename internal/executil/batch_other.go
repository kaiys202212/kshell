//go:build !windows

package executil

// QuoteCmdArg 非 Windows 无 cmd 引号需求，原样返回。
func QuoteCmdArg(s string) string { return s }

// BatchCommandLine 非 Windows 不会走 cmd 批处理包装，返回空串。
func BatchCommandLine(string, string, []string) string { return "" }
