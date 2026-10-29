//go:build windows

package executil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// wrapBatchFile 把 .cmd/.bat 改写成 %COMSPEC% /S /C 命令行。
// 必须在 HideWindow 之后调用：HideWindow 会重建 SysProcAttr，提前写的 CmdLine 会被抹掉。
func wrapBatchFile(cmd *exec.Cmd) {
	if cmd == nil || len(cmd.Args) == 0 || !isBatchFile(cmd.Path) {
		return
	}
	comspec := comspecPath()
	args := append([]string(nil), cmd.Args[1:]...)
	cmdline := BatchCommandLine(comspec, cmd.Path, args)
	// 真实命令行在 CmdLine 里：Path 只用于 CreateProcess 的 lpApplicationName，
	// Args 置空避免 os/exec 再拼一份（CmdLine 非空时会被忽略，这里保持可读）。
	cmd.Path = comspec
	cmd.Args = []string{comspec}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CmdLine = cmdline
}

func isBatchFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cmd", ".bat":
		return true
	default:
		return false
	}
}

// comspecPath 取命令解释器绝对路径；与 terminal 后端的兜底规则保持一致。
func comspecPath() string {
	comspec := strings.TrimSpace(os.Getenv("COMSPEC"))
	if comspec == "" {
		comspec = "cmd.exe"
	}
	if filepath.Base(comspec) == comspec {
		if lp, err := exec.LookPath(comspec); err == nil {
			return lp
		}
	}
	return comspec
}
