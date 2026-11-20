package asrlocal

import (
	"archive/tar"
	"compress/bzip2"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// extractRuntime 解压 sherpa-onnx runtime 的 tar.bz2：只取压缩包内的 bin/ 与
// lib/（其余 include、示例、LICENSE 都不要），并剥掉顶层目录。安全性：
// 条目路径先 Clean，任何越界（..）或绝对路径直接报错中止，绝不写目标目录外。
// 可执行位在 Unix 平台保留；tar 包实测无软链条目，遇到非常规类型跳过。
func extractRuntime(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	// 先清理目标，避免上次半解压残留混入新版。
	if err := os.RemoveAll(destDir); err != nil {
		return err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}

	tr := tar.NewReader(bzip2.NewReader(f))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取压缩包: %w", err)
		}
		name := path.Clean(strings.ReplaceAll(hdr.Name, "\\", "/"))
		if name == "." || name == "/" {
			continue
		}
		if path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return fmt.Errorf("压缩包条目越界: %q", hdr.Name)
		}
		// 剥掉顶层目录（sherpa-onnx-vX-.../）。
		parts := strings.SplitN(name, "/", 2)
		if len(parts) < 2 {
			continue
		}
		rel := parts[1]
		top := strings.SplitN(rel, "/", 2)[0]
		if top != "bin" && top != "lib" {
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(filepath.Join(destDir, filepath.FromSlash(rel)), 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			target := filepath.Join(destDir, filepath.FromSlash(rel))
			// 二次纵深防御：Join 后仍须在目标目录内。
			if !withinDir(destDir, target) {
				return fmt.Errorf("压缩包条目越界: %q", hdr.Name)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(hdr.Mode).Perm()
			if mode == 0 {
				mode = 0o644
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
			if runtime.GOOS != "windows" {
				_ = os.Chmod(target, mode)
			}
		default:
			// 软链/硬链/设备文件：官方包不含，遇到直接跳过，避免跨平台陷阱。
			continue
		}
	}
	return nil
}

// withinDir 判断 target 是否位于 dir 之内（Windows 大小写不敏感由 Join 保证）。
func withinDir(dir, target string) bool {
	rel, err := filepath.Rel(dir, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}