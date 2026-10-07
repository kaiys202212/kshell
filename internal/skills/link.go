package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	modeLink = "link"
	modeCopy = "copy"
)

// InstallTarget 将 entityDir 安装到 targetDir（链接优先，失败则复制）。
// targetDir 是完整的 skill 目录路径（.../skills/<name>）。
// force 为 true 时，若目标已存在且不是本实体，先删除再装。
func InstallTarget(entityDir, targetDir string, force bool) (mode string, err error) {
	entityDir = filepath.Clean(entityDir)
	targetDir = filepath.Clean(targetDir)
	if entityDir == "" || targetDir == "" {
		return "", fmt.Errorf("%w|empty path", errTargetConf)
	}
	if st, err := os.Lstat(targetDir); err == nil {
		if sameTarget(entityDir, targetDir, st) {
			return modeLink, nil
		}
		if !force {
			return "", errTargetConf
		}
		if err := RemoveTarget(targetDir); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(targetDir), 0o755); err != nil {
		return "", err
	}
	if err := tryLink(entityDir, targetDir); err == nil {
		return modeLink, nil
	}
	if err := copyDir(entityDir, targetDir); err != nil {
		return "", err
	}
	return modeCopy, nil
}

// RemoveTarget 删除链接或副本目录。
func RemoveTarget(targetDir string) error {
	targetDir = filepath.Clean(targetDir)
	if targetDir == "" || targetDir == "." || targetDir == string(filepath.Separator) {
		return fmt.Errorf("%w|refuse remove", errTargetConf)
	}
	err := os.RemoveAll(targetDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func sameTarget(entityDir, targetDir string, st os.FileInfo) bool {
	if st.Mode()&os.ModeSymlink != 0 {
		dest, err := os.Readlink(targetDir)
		if err != nil {
			return false
		}
		if !filepath.IsAbs(dest) {
			dest = filepath.Join(filepath.Dir(targetDir), dest)
		}
		return filepath.Clean(dest) == entityDir
	}
	// junction 在 Windows 上 Lstat 常显示为目录；尝试 EvalSymlinks
	resolved, err := filepath.EvalSymlinks(targetDir)
	if err == nil && filepath.Clean(resolved) == entityDir {
		return true
	}
	return false
}

func tryLink(entityDir, targetDir string) error {
	if err := os.Symlink(entityDir, targetDir); err == nil {
		return nil
	} else if runtime.GOOS != "windows" {
		return err
	}
	return junction(entityDir, targetDir)
}

func junction(entityDir, targetDir string) error {
	// Windows：cmd mklink /J；失败则返回错误让上层复制。
	if runtime.GOOS != "windows" {
		return errors.New("junction unsupported")
	}
	return windowsJunction(entityDir, targetDir)
}
