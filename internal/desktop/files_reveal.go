package desktop

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/yangk/kshell/internal/discovery"
)

// RevealInExplorer 在系统文件管理器中打开 path（文件则选中，目录则打开该目录）。
// 词法 underPath 后再 resolveRealPath：拦 Windows 目录 junction / Unix 符号链接越出工作区。
func (a *App) RevealInExplorer(wsPath, path string) error {
	kind, local, _, err := a.parseWSRef(wsPath)
	if err != nil {
		return err
	}
	if kind == discovery.KindSSH {
		return errRemoteFSOpUnsupported
	}
	cleanedWS := local
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
