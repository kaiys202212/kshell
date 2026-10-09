package desktop

import (
	"fmt"
	"os"

	"github.com/yangk/kshell/internal/discovery"
)

// openLocalFile 打开本地文件的系统默认关联应用；测试可替换。
var openLocalFile = openLocalFileOS

// OpenInDefaultApp 用系统默认应用打开工作区内的本地文件（如 HTML → 浏览器）。
// 不走 Wails BrowserOpenURL：其 URL 校验拒绝 file://，前端调用会静默失败。
// SSH 远程工作区不支持（无本地路径）。
func (a *App) OpenInDefaultApp(wsPath, path string) error {
	kind, local, _, err := a.parseWSRef(wsPath)
	if err != nil {
		return err
	}
	if kind == discovery.KindSSH {
		return errRemoteFSOpUnsupported
	}
	resolved, err := a.resolveWorkspaceFile(local, path)
	if err != nil {
		return err
	}
	fi, err := os.Stat(resolved)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return fmt.Errorf("err.files.dir_not_openable")
	}
	return openLocalFile(resolved)
}
