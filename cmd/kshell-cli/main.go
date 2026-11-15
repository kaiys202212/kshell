// kshell-cli 是供外部 agent 宿主（WorkBuddy 等）使用的最小核心 CLI：只含
// agentcli 薄客户端（endpoint 发现 + 本地 HTTP），无 TUI/GUI/前端依赖。
// 桌面版构建时先编出本二进制（build.ps1 -Desktop 注入 build/cli/），
// 桌面端运行时经 go:embed 自我释放到用户 bin 目录（见 internal/clisync），
// 使 `kshell agent ...` 在任何新终端/agent 会话里直接可用。
package main

import (
	"os"

	"github.com/yangk/kshell/internal/agentcli"
)

func main() {
	os.Exit(agentcli.Main(os.Args[1:]))
}
