package clisync

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// rcMarker 是写入 shell rc 的标记行：存在即视为已配置，幂等跳过。
const rcMarker = "# kshell CLI PATH (managed by kshell)"

// ensurePathUnix 把 binDir 加入 shell 启动文件（~/.bashrc、~/.zshrc）。
// pathEnv 已含 binDir 时不动任何文件（ready）；否则对每个 rc：已有标记跳过，
// 没有则追加标记注释 + export 行。参数化的 pathEnv/rcs 供测试注入。
func ensurePathUnix(binDir, pathEnv string, rcs []string) (string, error) {
	for _, e := range splitPathEnv(pathEnv) {
		if sameDir(e, binDir) {
			return pathReady, nil
		}
	}
	// 手工拼双引号而非 %q：Unix 路径含反斜杠的场景几乎不存在，%q 会把
	// Windows 测试路径里的分隔符转义成 \\ 反而破坏可读性。
	block := "\n" + rcMarker + "\n" + fmt.Sprintf("export PATH=\"%s:$PATH\"", binDir) + "\n"
	for _, rc := range rcs {
		if data, err := os.ReadFile(rc); err == nil && bytes.Contains(data, []byte(rcMarker)) {
			continue
		}
		f, err := os.OpenFile(rc, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return pathMissing, err
		}
		if _, err := f.WriteString(block); err != nil {
			f.Close()
			return pathMissing, err
		}
		f.Close()
	}
	return pathAdded, nil
}

// userRCFiles 返回需要补写的 shell 启动文件路径。
func userRCFiles() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	return []string{filepath.Join(home, ".bashrc"), filepath.Join(home, ".zshrc")}
}

// currentPathEnv 返回当前进程的 PATH。
func currentPathEnv() string { return os.Getenv("PATH") }

// splitPathEnv 按 OS 分隔符切分 PATH 并清洗空项。
func splitPathEnv(pathEnv string) []string {
	var out []string
	for _, e := range strings.Split(pathEnv, string(os.PathListSeparator)) {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	return out
}

// auditEntries 找出「排在 binDir 之前（binDir 不在 PATH 时视为全部）且目录内
// 存在 kshell 可执行文件」的条目——这些位置的旧副本可能抢先命中。
func auditEntries(binDir string, entries []string) []string {
	binDir = filepath.Clean(binDir)
	limit := len(entries)
	for i, e := range entries {
		if sameDir(e, binDir) {
			limit = i
			break
		}
	}
	var out []string
	for i := 0; i < limit; i++ {
		d := entries[i]
		if d == "" || sameDir(d, binDir) {
			continue
		}
		if fileExists(filepath.Join(d, BinaryName())) {
			out = append(out, d)
		}
	}
	return out
}

// sameDir 比较两个目录是否同一位置（Windows 不区分大小写）。
func sameDir(a, b string) bool {
	a, b = filepath.Clean(strings.TrimSpace(a)), filepath.Clean(strings.TrimSpace(b))
	if a == b {
		return true
	}
	return runtime.GOOS == "windows" && strings.EqualFold(a, b)
}

// containsPathEntry 报告 entries 是否含 dir。
func containsPathEntry(entries []string, dir string) bool {
	for _, e := range entries {
		if sameDir(e, dir) {
			return true
		}
	}
	return false
}

// pathStatusWith 按给定 PATH 条目集合判断释放目录可达性（只读）。
func pathStatusWith(lookup func() []string, binDir string) string {
	if containsPathEntry(lookup(), binDir) {
		return pathReady
	}
	return pathMissing
}
