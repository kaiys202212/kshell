// Package cursoragent 让 kshell 自身充当 cursor-acp 的 spawn 目标：
// Windows 上 node 入口（node.exe + index.js）不能写成 cursor-acp 能 spawn 的单一文件。
package cursoragent

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/yangk/kshell/internal/executil"
)

const (
	EnvAsProxy = "KSHELL_AS_CURSOR_AGENT"
	EnvNode    = "KSHELL_CURSOR_AGENT_NODE"
	EnvScript  = "KSHELL_CURSOR_AGENT_SCRIPT"
)

// ShouldProxy 表示本进程是 cursor-acp 拉起的代理，不得进入桌面/TUI。
// 三个变量必须齐全：只有 AsProxy 时若进入 Main 会立刻退出，表现为闪退。
func ShouldProxy() bool {
	return os.Getenv(EnvAsProxy) == "1" && os.Getenv(EnvNode) != "" && os.Getenv(EnvScript) != ""
}

// Main 把 os.Args[1:] 转给 node + 主脚本，stdio 原样继承。返回进程退出码。
func Main() int {
	node := os.Getenv(EnvNode)
	script := os.Getenv(EnvScript)
	if node == "" || script == "" {
		fmt.Fprintln(os.Stderr, "cursor-agent 代理缺少 KSHELL_CURSOR_AGENT_NODE/SCRIPT")
		return 1
	}
	args := append([]string{script}, os.Args[1:]...)
	cmd := exec.Command(node, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = childEnv()
	executil.HideWindow(cmd)
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// childEnv 去掉代理相关变量，避免 cursor-agent 再按 CURSOR_AGENT_EXECUTABLE 套一层 kshell。
func childEnv() []string {
	skip := map[string]bool{
		EnvAsProxy:                true,
		EnvNode:                   true,
		EnvScript:                 true,
		"CURSOR_AGENT_EXECUTABLE": true,
	}
	out := make([]string, 0, 32)
	for _, e := range os.Environ() {
		k, _, ok := strings.Cut(e, "=")
		if ok && skip[k] {
			continue
		}
		out = append(out, e)
	}
	return out
}
