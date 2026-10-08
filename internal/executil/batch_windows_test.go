//go:build windows

package executil

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestQuoteCmdArg(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"plain", "plain"},
		{"", `""`},
		{`C:\Program Files\tool.cmd`, `"C:\Program Files\tool.cmd"`},
		{`{"a":"b"}`, `"{""a"":""b""}"`},
		{`say "hi"`, `"say ""hi"""`},
	}
	for _, c := range cases {
		if got := QuoteCmdArg(c.in); got != c.want {
			t.Fatalf("QuoteCmdArg(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBatchCommandLineShape(t *testing.T) {
	comspec := `C:\Windows\System32\cmd.exe`
	bat := `C:\Program Files\nodejs\claude.cmd`
	got := BatchCommandLine(comspec, bat, []string{"--settings", `{"a":1}`})
	if !strings.HasPrefix(got, comspec+` /S /C "`) {
		t.Fatalf("应使用 /S /C 包裹整段命令, got %q", got)
	}
	if !strings.Contains(got, `"C:\Program Files\nodejs\claude.cmd"`) {
		t.Fatalf("bat 路径应被 cmd 引号包裹, got %q", got)
	}
	if !strings.HasSuffix(got, `"`) {
		t.Fatalf("整段 /C 余部应有收尾引号, got %q", got)
	}
}

// TestBatchCommandLineRunsSpacedBatWithJSON 复现「'C:\Program' 不是内部或外部命令」：
// 含空格的 .cmd + 需引号的 --settings，必须经 BatchCommandLine + CmdLine 才能跑通。
func TestBatchCommandLineRunsSpacedBatWithJSON(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Program Files Fake")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bat := filepath.Join(dir, "tool.cmd")
	body := "@echo off\r\necho ARGS:[%*]\r\nexit /b 0\r\n"
	if err := os.WriteFile(bat, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	comspec := os.Getenv("COMSPEC")
	if comspec == "" {
		comspec = "cmd.exe"
	}
	settings := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"\"C:\\Program Files\\kshell\\app.exe\" agent-hook claude"}]}]}}`
	cmdline := BatchCommandLine(comspec, bat, []string{"--settings", settings})

	cmd := exec.Command(comspec)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: cmdline, HideWindow: true}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("Run error: %v\ncmdline=%s\nstderr=%s\nstdout=%s", err, cmdline, stderr.String(), stdout.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "ARGS:[--settings") {
		t.Fatalf("脚本未收到 --settings, out=%q stderr=%q", out, stderr.String())
	}
	if strings.Contains(stderr.String(), `'C:\`) && strings.Contains(stderr.String(), "不是内部或外部命令") {
		t.Fatalf("仍触发路径截断: stderr=%q", stderr.String())
	}
}
