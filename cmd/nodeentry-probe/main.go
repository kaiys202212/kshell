// go run ./cmd/nodeentry-probe —— 一次性真机验证：node 入口兜底检测 + 启动参数组装。
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/executil"
	"github.com/yangk/kshell/internal/launch"
	"github.com/yangk/kshell/internal/providers"
)

func main() {
	home, _ := os.UserHomeDir()
	tools := discovery.DetectAll(home, providers.Builtins())
	for _, t := range tools {
		if t.ID != "cursor" {
			continue
		}
		fmt.Printf("Tool: %+v\n", t)
		if t.BinPath == "" {
			fmt.Println("BinPath 为空（如本机 shim/本体均缺失则属预期）")
			return
		}
		// 内联 ProbeVersion 实现，打印被吞掉的真实错误
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		full := append(append([]string(nil), t.BinArgs...), "--version")
		cmd := exec.CommandContext(ctx, t.BinPath, full...)
		executil.HideWindow(cmd)
		out, err := cmd.Output()
		cancel()
		fmt.Printf("内联探测: out=%q err=%v args=%q\n", string(out), err, full)
		if ee, ok := err.(*exec.ExitError); ok {
			fmt.Printf("stderr=%q\n", string(ee.Stderr))
		}
		s := providers.Session{ToolID: "cursor", ID: "demo-id"}
		l, err2 := launch.ForSession(providers.Builtins(), tools, s, launch.ThemeOptions{}, launch.ModelOptions{}, launch.PermissionOptions{})
		if err2 != nil {
			fmt.Println("ForSession error:", err2)
			os.Exit(1)
		}
		fmt.Printf("Resume: path=%s args=%v\n", l.Path, l.Args)
		return
	}
	fmt.Println("cursor not detected")
	os.Exit(1)
}
