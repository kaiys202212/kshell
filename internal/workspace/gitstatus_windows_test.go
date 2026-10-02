//go:build windows

package workspace

import (
	"context"
	"testing"
)

// TestGitCmd_隐藏控制台窗口 回归：进入项目页签时 FileTree 挂载会触发 GitStatus，
// 桌面版（windowsgui 子系统，无控制台可继承）若裸露执行 git.exe 就会闪一个黑窗。
func TestGitCmd_隐藏控制台窗口(t *testing.T) {
	cmd := gitCmd(context.Background(), `C:\tmp`, "status", "--porcelain=v1")
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr 为空：git 子进程会弹出可见控制台黑窗")
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Error("HideWindow 未设置")
	}
	// CREATE_NO_WINDOW = 0x08000000
	if cmd.SysProcAttr.CreationFlags != 0x08000000 {
		t.Errorf("CreationFlags = %#x，期望 CREATE_NO_WINDOW(0x08000000)", cmd.SysProcAttr.CreationFlags)
	}

	// 参数拼接不能破坏「-C <root>」前缀，否则 git 会在错误目录下执行
	want := []string{"-C", `C:\tmp`, "status", "--porcelain=v1"}
	if len(cmd.Args) != 1+len(want) {
		t.Fatalf("Args = %v，期望 git 后跟 %v", cmd.Args, want)
	}
	for i, w := range want {
		if cmd.Args[i+1] != w {
			t.Fatalf("Args[%d] = %q，期望 %q", i+1, cmd.Args[i+1], w)
		}
	}
}
