//go:build windows

package executil

import "strings"

// QuoteCmdArg 按 cmd.exe 规则给参数加引号：内嵌 " 写成 ""。
// 含空白或 cmd 元字符时才包裹，避免无谓改写简单参数。
func QuoteCmdArg(s string) string {
	if s == "" {
		return `""`
	}
	need := false
	for _, r := range s {
		switch r {
		case ' ', '\t', '"', '&', '<', '>', '(', ')', '@', '^', '|', '!', '%':
			need = true
		}
		if need {
			break
		}
	}
	if !need {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// BatchCommandLine 构造经 CreateProcess 直接下发的命令行（配合 SysProcAttr.CmdLine）：
//
//	comspec /S /C "<quoted-bat> <quoted-args...>"
//
// /S 让 cmd 剥掉 /C 后首尾引号，从而保留 bat 路径与 JSON 参数内部的引号。
// 若改用 argv + ComposeCommandLine，bat 在 Program Files 且参数需引号时会被截成 `C:\Program`。
func BatchCommandLine(comspec, bat string, args []string) string {
	var b strings.Builder
	b.WriteString(QuoteCmdArg(bat))
	for _, a := range args {
		b.WriteByte(' ')
		b.WriteString(QuoteCmdArg(a))
	}
	return comspec + ` /S /C "` + b.String() + `"`
}
