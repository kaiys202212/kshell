//go:build windows

package update

import (
	"os"
	"strings"
	"testing"
)

func TestReplaceWaitStartPowerShell(t *testing.T) {
	pid := 4242
	cur := `C:\Program Files\kshell\kshell-desktop.exe`
	nxt := cur + ".new"
	ps := replaceWaitStartPowerShell(pid, cur, nxt)
	for _, want := range []string{
		"Wait-Process -Id 4242",
		"Move-Item -LiteralPath",
		cur + ".new",
		"Start-Process -LiteralPath",
		"-WindowStyle Normal",
		"Start-Sleep",
	} {
		if !strings.Contains(ps, want) {
			t.Fatalf("脚本缺少 %q:\n%s", want, ps)
		}
	}
	if !strings.Contains(ps, "for (") && !strings.Contains(ps, "for(") {
		t.Fatalf("应重试 Move-Item:\n%s", ps)
	}

	ps = replaceWaitStartPowerShell(os.Getpid(), `C:\a'b.exe`, `C:\a'b.exe.new`)
	if strings.Contains(ps, `a'b`) && !strings.Contains(ps, `a''b`) {
		t.Fatalf("单引号应转义: %s", ps)
	}
}

func TestSpawnReplaceCmd_脱离父进程(t *testing.T) {
	cmd := spawnReplaceCmd(`C:\kshell\kshell-desktop.exe`, `C:\kshell\kshell-desktop.exe.new`)
	if cmd.SysProcAttr == nil {
		t.Fatal("未脱离父进程 Job")
	}
	if cmd.SysProcAttr.CreationFlags&0x01000000 == 0 {
		t.Fatalf("CreationFlags = %#x 缺少 CREATE_BREAKAWAY_FROM_JOB", cmd.SysProcAttr.CreationFlags)
	}
}
