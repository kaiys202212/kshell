package terminal

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCommandLineRejectsEmptyPath(t *testing.T) {
	if _, _, _, err := commandLine(Spec{Args: []string{"x"}}); !errors.Is(err, errEmptyStart) {
		t.Fatalf("空路径应报 errEmptyStart: %v", err)
	} else if err.Error() != "err.terminal.no_exec" {
		t.Fatalf("空路径错误应为 wire key, got %q", err.Error())
	}
}

func TestCommandLinePassesThroughAbsoluteExecutable(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "claude.exe")
	path, args, cmdline, err := commandLine(Spec{Path: abs, Args: []string{"--resume", "s1"}})
	if err != nil {
		t.Fatalf("commandLine error: %v", err)
	}
	if path != abs || len(args) != 2 || args[0] != "--resume" || cmdline != "" {
		t.Fatalf("commandLine = %q %v cmdline=%q", path, args, cmdline)
	}
}

// TestCommandLineResolvesBareNameFromPATH 覆盖预览区「+」开本地 shell 的根因：
// go-pty 在 Windows 上会把无目录的命令名拼到 Spec.Dir（工作区）下再查找，
// 必须先在 PATH 上解析成绝对路径，否则会变成 <工作区>\powershell 找不到。
func TestCommandLineResolvesBareNameFromPATH(t *testing.T) {
	dir := t.TempDir()
	bin := ptyScript(t, dir, 0, "x", false)
	name := filepath.Base(bin)
	t.Setenv("PATH", dir)

	path, args, cmdline, err := commandLine(Spec{Path: name, Args: []string{"--flag"}})
	if err != nil {
		t.Fatalf("commandLine error: %v", err)
	}
	if runtime.GOOS == "windows" {
		comspec := os.Getenv("COMSPEC")
		if comspec == "" {
			comspec = "cmd.exe"
		}
		if path != comspec {
			t.Fatalf("Windows 上 PATH 里的 .cmd 应经 %q 启动, got %q", comspec, path)
		}
		if len(args) != 0 {
			t.Fatalf("批处理 Args 应为空（走 CmdLine）, got %v", args)
		}
		if !strings.Contains(cmdline, "/S /C ") || !strings.Contains(cmdline, bin) {
			t.Fatalf("cmdline 应含 /S /C 与脚本绝对路径, got %q want bin %q", cmdline, bin)
		}
		return
	}
	if path != bin {
		t.Fatalf("应解析为 PATH 上的绝对路径, got %q want %q", path, bin)
	}
	if len(args) != 1 || args[0] != "--flag" || cmdline != "" {
		t.Fatalf("args = %v cmdline=%q", args, cmdline)
	}
}

func TestCommandLineBareNameNotOnPATH(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, _, _, err := commandLine(Spec{Path: "kshell-definitely-missing-bin"})
	if err == nil {
		t.Fatal("PATH 上不存在的裸命令名应失败")
	}
	if !strings.HasPrefix(err.Error(), "err.terminal.exec_not_found|") {
		t.Fatalf("错误应为 err.terminal.exec_not_found wire 串, got %v", err)
	}
	if !strings.Contains(err.Error(), "kshell-definitely-missing-bin") {
		t.Fatalf("错误应包含命令名, got %v", err)
	}
}

// TestCommandLineBatchWrappingIsWindowsOnly 保证非 Windows 构建不会去构造 %COMSPEC%。
func TestCommandLineBatchWrappingIsWindowsOnly(t *testing.T) {
	path, args, cmdline, err := commandLine(Spec{Path: `D:\bin\tool.cmd`, Args: []string{"--resume", "s1"}})
	if err != nil {
		t.Fatalf("commandLine error: %v", err)
	}

	if runtime.GOOS != "windows" {
		if path != `D:\bin\tool.cmd` {
			t.Fatalf("非 Windows 平台应原样透传, got %q", path)
		}
		if len(args) != 2 || cmdline != "" {
			t.Fatalf("非 Windows 平台不应追加参数或 CmdLine, got args=%v cmdline=%q", args, cmdline)
		}
		return
	}

	want := os.Getenv("COMSPEC")
	if want == "" {
		want = "cmd.exe"
	}
	if path != want {
		t.Fatalf("Windows 上 .cmd 应经 %q 启动, got %q", want, path)
	}
	if len(args) != 0 {
		t.Fatalf("批处理 Args 应为空（走 CmdLine）, got %v", args)
	}
	if !strings.Contains(cmdline, "/S /C ") || !strings.Contains(cmdline, `D:\bin\tool.cmd`) || !strings.Contains(cmdline, "--resume") {
		t.Fatalf("cmdline = %q", cmdline)
	}
}

// TestCommandLineSpacedBatWithSettingsUsesCmdLine 覆盖 Program Files 下 .cmd + --settings JSON
// 被 cmd /c 截成 C:\Program 的根因：必须产出 /S /C 手写 CmdLine。
func TestCommandLineSpacedBatWithSettingsUsesCmdLine(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("批处理包装仅 Windows")
	}
	bat := `C:\Program Files\nodejs\claude.cmd`
	settings := `{"a":1}`
	path, args, cmdline, err := commandLine(Spec{Path: bat, Args: []string{"--settings", settings}})
	if err != nil {
		t.Fatalf("commandLine error: %v", err)
	}
	comspec := os.Getenv("COMSPEC")
	if comspec == "" {
		comspec = "cmd.exe"
	}
	if path != comspec || len(args) != 0 {
		t.Fatalf("path=%q args=%v", path, args)
	}
	if !strings.Contains(cmdline, `/S /C "`) || !strings.Contains(cmdline, `"C:\Program Files\nodejs\claude.cmd"`) {
		t.Fatalf("cmdline 形态不对: %q", cmdline)
	}
}

func TestIsBatchFile(t *testing.T) {
	for _, p := range []string{"a.cmd", `D:\npm\codex.CMD`, "x.bat", "y.BAT"} {
		if !isBatchFile(p) {
			t.Fatalf("%q 应判定为批处理脚本", p)
		}
	}
	for _, p := range []string{"claude.exe", "claude", "a.ps1", "a.sh", ""} {
		if isBatchFile(p) {
			t.Fatalf("%q 不应判定为批处理脚本", p)
		}
	}
}

// ptyScript 写一个「回显标记 + 指定退出码」的脚本，用于真机跑一次后端。
// settle 为真时在回显后停一下：ConPTY 在进程退出瞬间才 flush 的输出有丢失风险（真机实测），
// 需要断言输出的用例必须让输出先落到伪终端再退出。
func ptyScript(t *testing.T, dir string, code int, msg string, settle bool) string {
	t.Helper()
	body := "@echo off\r\necho " + msg + "-%KPTY_TEST_ENV%\r\n"
	if settle {
		body += "ping -n 2 127.0.0.1 >nul\r\n"
	}
	body += "exit /b " + strconv.Itoa(code) + "\r\n"
	name := "tool.cmd"
	if runtime.GOOS != "windows" {
		body = "#!/bin/sh\necho " + msg + "-$KPTY_TEST_ENV\n"
		if settle {
			body += "sleep 0.3\n"
		}
		body += "exit " + strconv.Itoa(code) + "\n"
		name = "tool.sh"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestPTYBackendRunsRealProcess 真机验证后端：能读到输出、拿到退出码、Close 能唤醒读协程。
// 这条用例在 Windows 上同时覆盖了 .cmd 的 %COMSPEC% /S /C + CmdLine 包装。
func TestPTYBackendRunsRealProcess(t *testing.T) {
	t.Setenv("KPTY_TEST_ENV", "inherited")
	bin := ptyScript(t, t.TempDir(), 5, "boom", true)

	h, err := NewPTYBackend().Start(Spec{Path: bin, Dir: t.TempDir()}, 100, 30)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer func() { _ = h.Close() }()

	var out bytes.Buffer
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 4096)
		for {
			n, err := h.Read(buf)
			if n > 0 {
				out.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	code, err := h.Wait()
	if err != nil {
		t.Fatalf("Wait error: %v", err)
	}
	if code != 5 {
		t.Fatalf("退出码 = %d, 期望 5", code)
	}
	_ = h.Close() // ConPTY 不会在子进程退出时给读端 EOF，得靠关句柄唤醒

	select {
	case <-readDone:
	case <-time.After(10 * time.Second):
		t.Fatal("Close 后读协程仍在阻塞")
	}
	if got := out.String(); !strings.Contains(got, "boom-inherited") {
		t.Fatalf("输出 = %q, 期望包含 boom-inherited（Env 为空应继承当前环境）", got)
	}
}

// TestPTYBackendRunsSpacedBatWithSettings 真机复现 Program Files 路径 + --settings JSON：
// 旧实现会立刻打出「'C:\Program' 不是内部或外部命令」并退出。
func TestPTYBackendRunsSpacedBatWithSettings(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("批处理空格路径仅 Windows")
	}
	dir := filepath.Join(t.TempDir(), "Program Files Fake")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "tool.cmd")
	body := "@echo off\r\necho MARKER_OK\r\nping -n 2 127.0.0.1 >nul\r\nexit /b 0\r\n"
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	h, err := NewPTYBackend().Start(Spec{
		Path: bin,
		Args: []string{"--settings", `{"a":1}`},
		Dir:  t.TempDir(),
	}, 80, 24)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer func() { _ = h.Close() }()

	var out bytes.Buffer
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 4096)
		for {
			n, err := h.Read(buf)
			if n > 0 {
				out.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	code, err := h.Wait()
	if err != nil {
		t.Fatalf("Wait error: %v", err)
	}
	_ = h.Close()
	select {
	case <-readDone:
	case <-time.After(10 * time.Second):
		t.Fatal("Close 后读协程仍在阻塞")
	}
	got := out.String()
	if code != 0 {
		t.Fatalf("退出码 = %d, 输出 = %q", code, got)
	}
	if strings.Contains(got, "不是内部或外部命令") || strings.Contains(got, "is not recognized") {
		t.Fatalf("仍截断路径: %q", got)
	}
	if !strings.Contains(got, "MARKER_OK") {
		t.Fatalf("未看到脚本输出, got %q", got)
	}
}

func TestPTYBackendRejectsBadSize(t *testing.T) {
	bin := ptyScript(t, t.TempDir(), 0, "x", false)
	h, err := NewPTYBackend().Start(Spec{Path: bin}, 80, 24)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer func() { _ = h.Close() }()

	if err := h.Resize(0, 24); !errors.Is(err, errBadSize) {
		t.Fatalf("非正尺寸应报 errBadSize: %v", err)
	}
	if err := h.Resize(120, 40); err != nil {
		t.Fatalf("Resize error: %v", err)
	}
	_, _ = h.Wait()
}

// TestPTYHandleWaitIsIdempotent 退出码在多次 Wait 之间保持一致（前端可能重复查询）。
func TestPTYHandleWaitIsIdempotent(t *testing.T) {
	bin := ptyScript(t, t.TempDir(), 2, "bye", false)
	h, err := NewPTYBackend().Start(Spec{Path: bin}, 80, 24)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer func() { _ = h.Close() }()

	for i := 0; i < 2; i++ {
		code, err := h.Wait()
		if err != nil {
			t.Fatalf("第 %d 次 Wait error: %v", i, err)
		}
		if code != 2 {
			t.Fatalf("第 %d 次 Wait 退出码 = %d, 期望 2", i, code)
		}
	}
}

func TestPTYBackendStartFailsOnMissingBinary(t *testing.T) {
	if _, err := NewPTYBackend().Start(Spec{Path: filepath.Join(t.TempDir(), "nope-cmd")}, 80, 24); err == nil {
		t.Fatal("不存在的可执行文件应导致 Start 失败")
	}
}

// TestPTYBackendStartsBarePATHNameFromOtherDir 模拟预览「+」：命令在 PATH 上、工作目录是仓库根。
// 未先 LookPath 时 go-pty 会去 <Dir>\<name> 找，Start 直接失败。
func TestPTYBackendStartsBarePATHNameFromOtherDir(t *testing.T) {
	binDir := t.TempDir()
	workDir := t.TempDir()
	bin := ptyScript(t, binDir, 0, "ok", false)
	t.Setenv("PATH", binDir)

	h, err := NewPTYBackend().Start(Spec{Path: filepath.Base(bin), Dir: workDir}, 80, 24)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer func() { _ = h.Close() }()
	if _, err := h.Wait(); err != nil {
		t.Fatalf("Wait error: %v", err)
	}
}

// TestPTYBackendStartsPowerShellBareNameFromOtherDir 对齐用户复现：预览「+」Path=powershell、Dir=工作区。
func TestPTYBackendStartsPowerShellBareNameFromOtherDir(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("预览区本地 shell 在 Windows 上走 powershell")
	}
	if _, err := exec.LookPath("powershell"); err != nil {
		t.Skip("本机 PATH 上没有 powershell")
	}
	h, err := NewPTYBackend().Start(Spec{
		Path: "powershell",
		Args: []string{"-NoProfile", "-NonInteractive", "-Command", "exit 0"},
		Dir:  t.TempDir(),
	}, 80, 24)
	if err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer func() { _ = h.Close() }()
	if _, err := h.Wait(); err != nil {
		t.Fatalf("Wait error: %v", err)
	}
}
