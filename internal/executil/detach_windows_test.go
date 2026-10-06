//go:build windows

package executil

import (
	"os/exec"
	"testing"
)

func TestDetachFromParent_脱离Job且无窗口(t *testing.T) {
	cmd := exec.Command("powershell", "-NoProfile", "-Command", "1")
	DetachFromParent(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr 为空")
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Error("HideWindow 未设置")
	}
	const breakaway = 0x01000000
	const newGroup = 0x00000200
	const noWin = 0x08000000
	got := cmd.SysProcAttr.CreationFlags
	for _, bit := range []uint32{breakaway, newGroup, noWin} {
		if got&bit == 0 {
			t.Errorf("CreationFlags = %#x 缺少 %#x", got, bit)
		}
	}
}
