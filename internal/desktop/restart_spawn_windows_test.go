//go:build windows

package desktop

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestRestartWaitStartPowerShell(t *testing.T) {
	pid := 12345
	exe := `C:\Program Files\kshell\app.exe`
	ps := restartWaitStartPowerShell(pid, exe)
	if !strings.Contains(ps, fmt.Sprintf("Wait-Process -Id %d", pid)) {
		t.Fatalf("应等待 PID %d: %q", pid, ps)
	}
	if !strings.Contains(ps, "C:\\Program Files\\kshell\\app.exe") {
		t.Fatalf("应包含 LiteralPath 路径: %q", ps)
	}

	pid = os.Getpid()
	ps = restartWaitStartPowerShell(pid, exe)
	if !strings.Contains(ps, fmt.Sprintf("Wait-Process -Id %d", pid)) {
		t.Fatalf("应包含当前 PID %d: %q", pid, ps)
	}
}
