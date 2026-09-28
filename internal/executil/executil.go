// Package executil 提供跨平台的子进程窗口控制辅助。
package executil

import "os/exec"

// HideWindow 阻止子进程弹出控制台黑窗。
//
// 背景：GUI 子系统的父进程（如 Wails 桌面壳，本身无控制台）启动控制台子系统
// 的子进程（ssh、powershell、cmd 等）时，Windows 会为子进程分配一个新的可见
// 控制台窗口——即使用户只想要它的 stdout，也会看到黑窗一闪而过。
// HideWindow 通过 CREATE_NO_WINDOW 让子进程拿到一个不可见控制台，
// 输出捕获不受影响。
//
// 控制台父进程（TUI 版 kshell）下子进程直接继承父控制台，本就无新窗口，
// 设置后亦无副作用，因此所有调用方可以无差别使用。
func HideWindow(cmd *exec.Cmd) { hideWindow(cmd) }
