package desktop

import (
	"os"
	"path/filepath"
	"strings"
)

// RevealInExplorer 在系统文件管理器中打开 path（文件则选中，目录则打开该目录）。
// 词法 underPath 后再 resolveRealPath：拦 Windows 目录 junction / Unix 符号链接越出工作区。
func (a *App) RevealInExplorer(wsPath, path string) error {
	cleanedWS := filepath.Clean(strings.TrimSpace(wsPath))
	abs := filepath.Clean(strings.TrimSpace(path))
	if !underPath(cleanedWS, abs) {
		return errPathOutsideWorkspace
	}
	resolved, err := resolveRealPath(abs)
	if err != nil {
		return err
	}
	if !underPath(cleanedWS, resolved) {
		return errPathOutsideWorkspace
	}
	fi, err := os.Stat(resolved)
	if err != nil {
		return err
	}
	return revealInOS(resolved, fi.IsDir())
}
