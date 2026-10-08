package terminal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/aymanbagabas/go-pty"
	"github.com/yangk/kshell/internal/executil"
)

// NewPTYBackend 返回真实终端后端：Windows 走 ConPTY，其它平台走 creack/pty（由 go-pty 分平台实现）。
func NewPTYBackend() Backend { return ptyBackend{} }

// ptyBackend 用 go-pty 把子进程挂到伪终端上启动。
type ptyBackend struct{}

func (ptyBackend) Start(spec Spec, cols, rows int) (Handle, error) {
	path, args, cmdline, err := commandLine(spec)
	if err != nil {
		return nil, err
	}

	p, err := pty.New()
	if err != nil {
		return nil, fmt.Errorf("err.terminal.pty_create_failed|%w", err)
	}

	cmd := p.Command(path, args...)
	cmd.Dir = spec.Dir
	cmd.Env = mergedEnv(spec.Env) // nil 表示继承当前进程环境
	applyCmdLine(cmd, cmdline)    // Windows 批处理：手写 CmdLine，避免 /c 截断含空格路径
	if err := cmd.Start(); err != nil {
		_ = p.Close()
		return nil, fmt.Errorf("err.terminal.spawn_failed|%w", err)
	}

	h := &ptyHandle{p: p, cmd: cmd}
	if cols > 0 && rows > 0 {
		// 初始尺寸失败不致命（部分环境不支持 Resize），保持伪终端默认 80x25。
		_ = h.Resize(cols, rows)
	}
	return h, nil
}

// commandLine 决定实际启动的命令。
// Windows 上 .cmd/.bat 是批处理脚本，CreateProcess 不能直接执行，
// 必须经 %COMSPEC% 交给命令解释器（npm 全局 CLI 的 .cmd 兄弟文件走的就是这条路）；
// go-pty 在 Windows 直接用 CreateProcess，不像 exec.Cmd 会自动包壳。
// 批处理返回非空 cmdline（/S /C 手写命令行），由 applyCmdLine 写入 SysProcAttr，
// 避免 ComposeCommandLine + cmd /c 在「路径含空格 + 参数需引号」时截成 C:\Program。
// .ps1 已在 launcher.Resolve 里转成 .cmd 兄弟文件或 powershell -Command，这里无需特判。
func commandLine(spec Spec) (path string, args []string, cmdline string, err error) {
	if strings.TrimSpace(spec.Path) == "" {
		return "", nil, "", errEmptyStart
	}
	path = spec.Path
	// go-pty 在 Windows 上会把「无路径分隔符」的命令拼到 Spec.Dir 下再 LookPath，
	// 预览区「+」开 powershell 时 Dir 是工作区，就会变成 <工作区>\powershell 找不到。
	// 先在 PATH 上解析成绝对路径，CreateProcess 才不会跑偏。
	if filepath.Base(path) == path {
		lp, err := exec.LookPath(path)
		if err != nil {
			// %w 仅为 errors.Is 保留；{{1}} 的明细与 {{0}} 重复，当前不在 UI 展示。
			return "", nil, "", fmt.Errorf("err.terminal.exec_not_found|%s|%w", path, err)
		}
		path = lp
	}
	if runtime.GOOS == "windows" && isBatchFile(path) {
		comspec := os.Getenv("COMSPEC")
		if strings.TrimSpace(comspec) == "" {
			comspec = "cmd.exe"
		}
		if filepath.Base(comspec) == comspec {
			lp, err := exec.LookPath(comspec)
			if err != nil {
				// %w 仅为 errors.Is 保留；{{1}} 的明细与 {{0}} 重复，当前不在 UI 展示。
				return "", nil, "", fmt.Errorf("err.terminal.exec_not_found|%s|%w", comspec, err)
			}
			comspec = lp
		}
		// Args 留空：真实命令行走 cmdline，避免 go-pty 再用 ComposeCommandLine 二次转义。
		return comspec, nil, executil.BatchCommandLine(comspec, path, spec.Args), nil
	}
	return path, spec.Args, "", nil
}

// isBatchFile 报告路径是否是 Windows 批处理脚本。
func isBatchFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cmd", ".bat":
		return true
	default:
		return false
	}
}

// ptyHandle 把 go-pty 的 Pty/Cmd 适配成 Handle。
type ptyHandle struct {
	p   pty.Pty
	cmd *pty.Cmd

	waitOnce  sync.Once
	closeOnce sync.Once
	code      int
	werr      error
}

func (h *ptyHandle) Read(b []byte) (int, error)  { return h.p.Read(b) }
func (h *ptyHandle) Write(b []byte) (int, error) { return h.p.Write(b) }

func (h *ptyHandle) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return errBadSize
	}
	return h.p.Resize(cols, rows)
}

// Wait 等进程退出并返回退出码：非 0 退出不算错误（退出码已在返回值里），
// 只有「等不到进程状态」这类失败才返回 error。重复调用返回同一次结果。
func (h *ptyHandle) Wait() (int, error) {
	h.waitOnce.Do(func() {
		err := h.cmd.Wait()
		if st := h.cmd.ProcessState; st != nil {
			h.code = st.ExitCode()
		}
		var exitErr *exec.ExitError
		if err != nil && !errors.As(err, &exitErr) {
			h.werr = err
		}
	})
	return h.code, h.werr
}

// mergedEnv 把额外环境变量叠加到父进程环境之上；空表示完全继承（返回 nil）。
func mergedEnv(extra []string) []string {
	if len(extra) == 0 {
		return nil
	}
	return append(os.Environ(), extra...)
}

// Close 结束子进程并关闭伪终端，幂等。
// 先杀进程再关句柄：ConPTY 关掉句柄不会终止已挂上的子进程；反过来先关句柄会让读协程立刻拿到错误。
func (h *ptyHandle) Close() error {
	var err error
	h.closeOnce.Do(func() {
		if h.cmd.Process != nil {
			_ = h.cmd.Process.Kill()
		}
		err = h.p.Close()
	})
	return err
}
