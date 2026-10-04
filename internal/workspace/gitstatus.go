package workspace

import (
	"bytes"
	"context"
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

// GitStatus 执行 `git status --porcelain=v1 -z --untracked-files=all --ignored=matching`，
// 返回 relPath（'/' 分隔，相对 root）→ 状态码。
// porcelain 的路径相对「仓库根」输出，而工作区可能是仓库的子目录
// （discovery 按 cwd 聚合），所以先取 toplevel 把键归一到 root 相对路径。
// isRepo=false 表示不是 git 仓库或 git 不可用（二者对前端等价：不显示标记）；
// 超时/其它执行异常以 error 返回。
func GitStatus(root string) (status map[string]string, isRepo bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitStatusTimeout)
	defer cancel()

	topOut, execErr := gitCmd(ctx, root, "rev-parse", "--show-toplevel").Output()
	if execErr != nil {
		// 退出码 128（非仓库）、git 未安装等：一律按「非仓库」处理
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
