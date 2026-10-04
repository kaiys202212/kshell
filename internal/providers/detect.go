package providers

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/executil"
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
		for _, name := range specBinNames(spec) {
			for _, candidate := range binCandidates(root, name) {
				if isFile(candidate) {
					return Detection{Installed: true, BinPath: candidate, Source: "install-dir"}
				}
			}
		}
	}

	// node 入口兜底：shim 全被删光但 node.exe + 主脚本仍在（Cursor 更新器行为）。
	// InstallDirs 的先后顺序由 provider 负责（versions 目录已按最新在前）。
	if spec.NodeEntryScript != "" {
		for _, dir := range spec.InstallDirs {
			root := expandHome(dir, home)
			nodeBin := filepath.Join(root, "node.exe")
			if isFile(nodeBin) && isFile(filepath.Join(root, spec.NodeEntryScript)) {
				return Detection{
					Installed: true,
					BinPath:   nodeBin,
					BinArgs:   []string{spec.NodeEntryScript},
					Source:    "node-entry",
				}
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

// FindBins 列出 LookPath 命中的那一份，以及 InstallDirs 里存在的候选文件（去重），不含配置目录。
// 卸载时要删掉每一份拷贝，不能只删 Detect 命中的第一份。
func FindBins(spec DetectSpec, home string) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(p string) {
		if p == "" || !isFile(p) {
			return
		}
		c := filepath.Clean(p)
		if seen[c] {
			return
		}
		seen[c] = true
		out = append(out, p)
	}
	if bin, err := exec.LookPath(spec.BinName); err == nil {
		add(bin)
	}
	for _, dir := range spec.InstallDirs {
		root := expandHome(dir, home)
		for _, name := range specBinNames(spec) {
			for _, candidate := range binCandidates(root, name) {
				add(candidate)
			}
		}
	}
	return out
}

// specBinNames 返回 InstallDirs 内要尝试的全部可执行名：BinName 永远在前优先命中。
// PATH 探测不使用 AltBinNames（如 cursor 的 agent 名太通用），避免误命中无关二进制。
func specBinNames(spec DetectSpec) []string {
	return append([]string{spec.BinName}, spec.AltBinNames...)
}

// ProbeVersion 运行 <bin> --version 取首行；失败或超时一律返回 unknown，绝不阻塞扫描。
func ProbeVersion(bin string) string {
	if bin == "" {
		return "unknown"
	}

	ctx, cancel := context.WithTimeout(context.Background(), versionProbeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "--version")
	executil.HideWindow(cmd) // 桌面端扫描时避免黑窗闪烁
	out, err := cmd.Output()
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
	// InstallDirs 里既有 ~/.local/bin，也有 %LOCALAPPDATA%\cursor-agent。
	// os.ExpandEnv 只展开 $VAR；Windows 的 %VAR% 要单独处理。
	path = expandEnv(path)
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		return filepath.Join(home, path[2:])
	}
	return path
}

func expandEnv(path string) string {
	path = os.ExpandEnv(path)
	if runtime.GOOS != "windows" || !strings.Contains(path, "%") {
		return path
	}
	var b strings.Builder
	for i := 0; i < len(path); i++ {
		if path[i] != '%' {
			b.WriteByte(path[i])
			continue
		}
		rel := strings.IndexByte(path[i+1:], '%')
		if rel <= 0 {
			b.WriteByte('%')
			continue
		}
		name := path[i+1 : i+1+rel]
		b.WriteString(os.Getenv(name))
		i += rel + 1
	}
	return b.String()
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
