package providers

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const versionProbeTimeout = 3 * time.Second

// Detect 按 PATH → 常见安装目录 → 配置目录存在性 的顺序判断工具是否安装。
// 任一命中即算装了：只靠 PATH 会漏掉没写进环境变量的安装方式。
func Detect(spec DetectSpec, home string) Detection {
	if bin, err := exec.LookPath(spec.BinName); err == nil && bin != "" {
		return Detection{Installed: true, BinPath: bin, Source: "path"}
	}

	for _, dir := range spec.InstallDirs {
		root := expandHome(dir, home)
		for _, candidate := range binCandidates(root, spec.BinName) {
			if isFile(candidate) {
				return Detection{Installed: true, BinPath: candidate, Source: "install-dir"}
			}
		}
	}

	for _, dir := range spec.ConfigDirs {
		if isDir(expandHome(dir, home)) {
			return Detection{Installed: true, Source: "config-dir"}
		}
	}

	return Detection{}
}

// ProbeVersion 运行 <bin> --version 取首行；失败或超时一律返回 unknown，绝不阻塞扫描。
func ProbeVersion(bin string) string {
	if bin == "" {
		return "unknown"
	}

	ctx, cancel := context.WithTimeout(context.Background(), versionProbeTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	if err != nil {
		return "unknown"
	}
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if line == "" {
		return "unknown"
	}
	return line
}

func expandHome(path, home string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		return filepath.Join(home, path[2:])
	}
	return path
}

// binCandidates 列出目录下可能的可执行文件名。Windows 上 npm 全局安装的 CLI 常是 .cmd/.ps1 包装脚本，
// 只认 .exe 会漏掉 codex、opencode 这类工具（执行时的 shim 解析由 launcher 负责）。
func binCandidates(dir, name string) []string {
	if runtime.GOOS != "windows" {
		return []string{filepath.Join(dir, name)}
	}
	return []string{
		filepath.Join(dir, name+".exe"),
		filepath.Join(dir, name+".cmd"),
		filepath.Join(dir, name+".bat"),
		filepath.Join(dir, name+".ps1"),
	}
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
