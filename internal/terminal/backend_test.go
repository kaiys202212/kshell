package terminal

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCommandLineRejectsEmptyPath(t *testing.T) {
	if _, _, err := commandLine(Spec{Args: []string{"x"}}); !errors.Is(err, errEmptyStart) {
		t.Fatalf("空路径应报 errEmptyStart: %v", err)
	}
}

func TestCommandLinePassesThroughExecutable(t *testing.T) {
	path, args, err := commandLine(Spec{Path: "claude", Args: []string{"--resume", "s1"}})
	if err != nil {
		t.Fatalf("commandLine error: %v", err)
	}
	if path != "claude" || len(args) != 2 || args[0] != "--resume" {
		t.Fatalf("commandLine = %q %v", path, args)
	}
}

// TestCommandLineBatchWrappingIsWindowsOnly 保证非 Windows 构建不会去构造 %COMSPEC%。
func TestCommandLineBatchWrappingIsWindowsOnly(t *testing.T) {
	path, args, err := commandLine(Spec{Path: `D:\bin\tool.cmd`, Args: []string{"--resume", "s1"}})
	if err != nil {
		t.Fatalf("commandLine error: %v", err)
	}

	if runtime.GOOS != "windows" {
		if path != `D:\bin\tool.cmd` {
			t.Fatalf("非 Windows 平台应原样透传, got %q", path)
		}
		if len(args) != 2 {
			t.Fatalf("非 Windows 平台不应追加参数, got %v", args)
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
	if len(args) != 4 || args[0] != "/c" || args[1] != `D:\bin\tool.cmd` || args[2] != "--resume" {
		t.Fatalf("包装后的参数 = %v", args)
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
// 这条用例在 Windows 上同时覆盖了 .cmd 的 %COMSPEC% /c 包装。
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
