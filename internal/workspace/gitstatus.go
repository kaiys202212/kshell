package workspace

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/executil"
)

// gitStatusTimeout 单次 git status 的上限：仓库很大时 porcelain 也要秒级，超过即放弃。
const gitStatusTimeout = 10 * time.Second

// gitCmd 构造 git 子进程命令：统一在这里隐藏控制台窗口。
// 桌面版（windowsgui 子系统）自己没有控制台，进入项目页签时前端会立刻拉取
// git 状态（FileTree 挂载即刷新），裸露执行 git.exe 会闪过一个黑窗。
func gitCmd(ctx context.Context, root string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	executil.HideWindow(cmd)
	return cmd
}

// GitReport 是工作区 git 视图：状态映射 + 虚拟根/嵌套仓库分支名。
type GitReport struct {
	Status      map[string]string
	IsRepo      bool
	Branch      string
	DirBranches map[string]string // relPath → 分支；"" 表示工作区虚拟根
}

// GitStatus 兼容旧调用：只返回状态映射与是否仓库。
func GitStatus(root string) (status map[string]string, isRepo bool, err error) {
	r, err := InspectGit(root)
	if r.Status == nil {
		r.Status = map[string]string{}
	}
	return r.Status, r.IsRepo, err
}

// InspectGit 扫描工作区及其嵌套 git 根：合并 porcelain，并收集分支名。
func InspectGit(root string) (GitReport, error) {
	root = filepath.Clean(root)
	status, isRepo, err := gitStatusAt(root)
	if err != nil {
		return GitReport{}, err
	}
	rep := GitReport{Status: status, IsRepo: isRepo, DirBranches: map[string]string{}}
	if isRepo {
		rep.Branch = gitBranch(root)
		if rep.Branch != "" {
			rep.DirBranches[""] = rep.Branch
		}
	}
	for _, nested := range nestedGitRoots(root) {
		rel, rerr := filepath.Rel(root, nested)
		if rerr != nil {
			continue
		}
		relKey := filepath.ToSlash(rel)
		if relKey == "." {
			continue
		}
		if br := gitBranch(nested); br != "" {
			rep.DirBranches[relKey] = br
		}
		st, nestedRepo, nerr := gitStatusAt(nested)
		if nerr != nil || !nestedRepo {
			continue
		}
		if rep.Status == nil {
			rep.Status = map[string]string{}
		}
		for k, v := range st {
			rep.Status[relKey+"/"+strings.TrimPrefix(k, "./")] = v
		}
	}
	if rep.Status == nil {
		rep.Status = map[string]string{}
	}
	return rep, nil
}

// gitStatusAt 执行 `git status --porcelain=v1 -z --untracked-files=all --ignored=matching`，
// 返回 relPath（'/' 分隔，相对 root）→ 状态码。
// porcelain 的路径相对「仓库根」输出，而工作区可能是仓库的子目录
// （discovery 按 cwd 聚合），所以先取 toplevel 把键归一到 root 相对路径。
// isRepo=false 表示不是 git 仓库或 git 不可用（二者对前端等价：不显示标记）；
// 超时/其它执行异常以 error 返回。
func gitStatusAt(root string) (status map[string]string, isRepo bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitStatusTimeout)
	defer cancel()

	topOut, execErr := gitCmd(ctx, root, "rev-parse", "--show-toplevel").Output()
	if execErr != nil {
		return nil, false, nil
	}
	top := filepath.Clean(strings.TrimSpace(string(topOut)))

	cmd := gitCmd(ctx, root, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignored=matching")
	var out bytes.Buffer
	cmd.Stdout = &out
	if execErr := cmd.Run(); execErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, true, ctx.Err()
		}
		return nil, false, nil
	}
	return parsePorcelainZ(out.Bytes(), root, top), true, nil
}

func gitBranch(root string) string {
	ctx, cancel := context.WithTimeout(context.Background(), gitStatusTimeout)
	defer cancel()
	out, err := gitCmd(ctx, root, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func isGitDir(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

func nestedGitRoots(wsRoot string) []string {
	skip := map[string]bool{".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true}
	var roots []string
	_ = filepath.WalkDir(wsRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if skip[d.Name()] {
			return filepath.SkipDir
		}
		if path == wsRoot {
			return nil
		}
		if isGitDir(path) {
			roots = append(roots, path)
			return filepath.SkipDir
		}
		return nil
	})
	return roots
}

// parsePorcelainZ 解析 porcelain v1 -z 输出：条目以 NUL 分隔，
// rename/copy 条目为「XY new\0old」两段（old 段丢弃，只标新路径）。
// porcelain 路径相对仓库根（top），键要归一到相对工作区根（wsRoot）的 '/' 分隔路径；
// 工作区之外的条目换算出 ../ 前缀键，前端按 relPath 查不到即自然忽略。
func parsePorcelainZ(data []byte, wsRoot, top string) map[string]string {
	status := map[string]string{}
	rest := data
	for len(rest) > 0 {
		entry := rest
		if i := bytes.IndexByte(rest, 0); i >= 0 {
			entry, rest = rest[:i], rest[i+1:]
		} else {
			rest = nil
		}
		// 最短合法条目「XY p」为 4 字节
		if len(entry) < 4 {
			continue
		}
		x, y, path := entry[0], entry[1], string(entry[3:])
		if x == 'R' || x == 'C' {
			// 源路径占下一个 NUL 段，跳过
			if i := bytes.IndexByte(rest, 0); i >= 0 {
				rest = rest[i+1:]
			} else {
				rest = nil
			}
		}
		if path == "" {
			continue
		}
		if code := statusFromXY(x, y); code != "" {
			status[relKey(wsRoot, top, path)] = code
		}
	}
	return status
}

// relKey 把 porcelain 路径（'/' 分隔、相对仓库根）换算成相对工作区根的键：
// 先 join 到仓库根得绝对路径，再对工作区根取 rel。换算失败时原样返回。
func relKey(wsRoot, top, path string) string {
	rel, err := filepath.Rel(wsRoot, filepath.Join(top, filepath.FromSlash(path)))
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

// statusFromXY 把 porcelain 的 XY 双字符码归约为前端要展示的六类。
func statusFromXY(x, y byte) string {
	if x == '!' && y == '!' {
		return "ignored"
	}
	if x == '?' && y == '?' {
		return "untracked"
	}
	// 冲突态：任一位 U，或双方同改/同删
	if x == 'U' || y == 'U' || (x == 'A' && y == 'A') || (x == 'D' && y == 'D') {
		return "conflicted"
	}
	switch {
	case x == 'A' || x == 'C' || y == 'A':
		return "added"
	case x == 'D' || y == 'D':
		return "deleted"
	case x == 'R' || x == 'C':
		return "renamed"
	case x != ' ' || y != ' ':
		return "modified"
	}
	return ""
}
