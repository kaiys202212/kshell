package agentcli

import (
	"fmt"

	"github.com/yangk/kshell/internal/version"
)

// versionWarn 在 CLI 自身版本与 endpoint 携带的桌面端版本不一致时返回一行
// stderr 提醒；一致或一方未知（空串）返回空串。只提醒不阻断：薄客户端命令
// 大多向后兼容，升级窗口期的旧 CLI 仍应可用。
func versionWarn(app string) string {
	cur := version.Current()
	if app == "" || cur == "" || app == cur {
		return ""
	}
	return fmt.Sprintf("kshell agent: 提示——CLI 版本 %s 与桌面端版本 %s 不一致，建议更新 ~/.kshell/bin 下的 kshell\n", cur, app)
}
