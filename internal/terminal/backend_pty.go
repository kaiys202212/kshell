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
)

// NewPTYBackend 返回真实终端后端：Windows 走 ConPTY，其它平台走 creack/pty（由 go-pty 分平台实现）。
func NewPTYBackend() Backend { return ptyBackend{} }

// ptyBackend 用 go-pty 把子进程挂到伪终端上启动。
type ptyBackend struct{}

func (ptyBackend) Start(spec Spec, cols, rows int) (Handle, error) {
	path, args, err := commandLine(spec)
	if err != nil {
		return nil, err
	}

	p, err := pty.New()
	if err != nil {
		return nil, fmt.Errorf("创建伪终端失败: %w", err)
	}

	cmd := p.Command(path, args...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env // nil 表示继承当前进程环境
	if err := cmd.Start(); err != nil {
		_ = p.Close()
		return nil, fmt.Errorf("启动终端进程失败: %w", err)
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
// 必须经 %COMSPEC% /c 交给命令解释器（npm 全局 CLI 的 .cmd 兄弟文件走的就是这条路）；
// go-pty 在 Windows 直接用 CreateProcess，不像 exec.Cmd 会自动包壳。
// .ps1 已在 launcher.Resolve 里转成 .cmd 兄弟文件或 powershell -Command，这里无需特判。
func commandLine(spec Spec) (string, []string, error) {
	if strings.TrimSpace(spec.Path) == "" {
		return "", nil, errEmptyStart
	}
	if runtime.GOOS == "windows" && isBatchFile(spec.Path) {
		comspec := os.Getenv("COMSPEC")
		if strings.TrimSpace(comspec) == "" {
			comspec = "cmd.exe"
		}
		return comspec, append([]string{"/c", spec.Path}, spec.Args...), nil
	}
	return spec.Path, spec.Args, nil
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
