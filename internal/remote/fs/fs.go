package fs

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/remote"
)

// Runner 可注入的远端命令执行器；单测用 fake，生产可包一层 remote.Run。
type Runner func(ctx context.Context, conn remote.Connection, cmd string) (stdout, stderr []byte, err error)

// DirEntry 远端目录一项（字段对齐本地浏览所需最小集）。
type DirEntry struct {
	Name    string
	IsDir   bool
	Size    int64
	ModTime time.Time
}

// FS 经系统 ssh（由 Runner 完成）访问远端文件系统。首期假定远端为 Unix。
type FS struct {
	Conn remote.Connection
	Opts remote.SSHOptions
	Run  Runner
}

// ListDir 列出 absDir 下一层条目。用 find -printf 产出可解析的固定列，避免 ls 本地化差异。
func (f *FS) ListDir(ctx context.Context, absDir string) ([]DirEntry, error) {
	if f == nil || f.Run == nil {
		return nil, fmt.Errorf("err.remote.fs_no_runner")
	}
	dir := strings.TrimSpace(absDir)
	if dir == "" {
		return nil, fmt.Errorf("err.remote.fs_empty_path")
	}
	dir = normalizeRemote(dir)

	// GNU find -printf：type / size / mtime(epoch) / basename，制表符分隔，一行一项。
	cmd := fmt.Sprintf(
		"LC_ALL=C find %s -mindepth 1 -maxdepth 1 -printf '%%y\\t%%s\\t%%T@\\t%%f\\n'",
		shellSingleQuote(dir),
	)
	stdout, stderr, err := f.Run(ctx, f.Conn, cmd)
	if err != nil {
		if len(stderr) > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(stderr)))
		}
		return nil, err
	}
	return parseFindList(stdout)
}

func parseFindList(stdout []byte) ([]DirEntry, error) {
	text := string(stdout)
	if text == "" {
		return nil, nil
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	out := make([]DirEntry, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) != 4 {
			return nil, fmt.Errorf("err.remote.fs_parse|bad_fields")
		}
		size, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("err.remote.fs_parse|size: %w", err)
		}
		sec, err := strconv.ParseFloat(parts[2], 64)
		if err != nil {
			return nil, fmt.Errorf("err.remote.fs_parse|mtime: %w", err)
		}
		name := parts[3]
		if name == "" || name == "." || name == ".." {
			continue
		}
		out = append(out, DirEntry{
			Name:    name,
			IsDir:   parts[0] == "d",
			Size:    size,
			ModTime: time.Unix(int64(sec), 0).UTC(),
		})
	}
	return out, nil
}

// shellSingleQuote 用单引号包裹，防止路径中的空格/元字符被远端 shell 再解析。
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
