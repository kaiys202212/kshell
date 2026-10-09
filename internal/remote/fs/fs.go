package fs

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/remote"
)

// Runner 可注入的远端命令执行器；stdin 非空时经 ssh 标准输入送到远端（写文件用）。
// 单测用 fake，生产可包一层 remote.Run / RunWithStdin。
type Runner func(ctx context.Context, conn remote.Connection, cmd string, stdin []byte) (stdout, stderr []byte, err error)

// SearchHit 远端按名搜索的命中项。
type SearchHit struct {
	Name    string
	Path    string // 远端绝对路径
	RelPath string // 相对 root，'/' 分隔
	IsDir   bool
}

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
	stdout, stderr, err := f.Run(ctx, f.Conn, cmd, nil)
	if err != nil {
		if len(stderr) > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(stderr)))
		}
		return nil, err
	}
	return parseFindList(stdout)
}

// ResolveUnderRoot 把相对或绝对路径规范到 root 下；越界返回 err.remote.path_escape。
func ResolveUnderRoot(root, relOrAbs string) (string, error) {
	r := normalizeRemote(root)
	p := strings.TrimSpace(relOrAbs)
	if p == "" || p == "." {
		return r, nil
	}
	var abs string
	if strings.HasPrefix(p, "/") {
		abs = normalizeRemote(p)
	} else {
		abs = normalizeRemote(path.Join(r, p))
	}
	if err := WithinRoot(r, abs); err != nil {
		return "", err
	}
	return abs, nil
}

// ReadFile 读取 root 内文件，最多 limit 字节；超出返回 err.remote.fs_too_large。
// limit<=0 视为拒绝（必须显式上限，避免整盘 cat）。
func (f *FS) ReadFile(ctx context.Context, root, relOrAbs string, limit int64) ([]byte, error) {
	if f == nil || f.Run == nil {
		return nil, fmt.Errorf("err.remote.fs_no_runner")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("err.remote.fs_bad_limit")
	}
	abs, err := ResolveUnderRoot(root, relOrAbs)
	if err != nil {
		return nil, err
	}
	// head -c limit+1：多读一字节用于判定超限，避免先 stat 再 cat 的竞态。
	cmd := fmt.Sprintf("LC_ALL=C head -c %d %s", limit+1, shellSingleQuote(abs))
	stdout, stderr, err := f.Run(ctx, f.Conn, cmd, nil)
	if err != nil {
		if len(stderr) > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(stderr)))
		}
		return nil, err
	}
	if int64(len(stdout)) > limit {
		return nil, fmt.Errorf("err.remote.fs_too_large|%d|%d", len(stdout), limit)
	}
	return stdout, nil
}

// WriteFile 经 stdin 写入远端临时文件再 mv，保证不截断半写；path 须在 root 内。
func (f *FS) WriteFile(ctx context.Context, root, relOrAbs string, data []byte) error {
	if f == nil || f.Run == nil {
		return fmt.Errorf("err.remote.fs_no_runner")
	}
	abs, err := ResolveUnderRoot(root, relOrAbs)
	if err != nil {
		return err
	}
	dir := path.Dir(abs)
	tmpTpl := path.Join(dir, ".kshell-write.XXXXXX")
	cmd := fmt.Sprintf(
		`tmp=$(mktemp %s) && cat > "$tmp" && mv -f "$tmp" %s`,
		shellSingleQuote(tmpTpl),
		shellSingleQuote(abs),
	)
	_, stderr, err := f.Run(ctx, f.Conn, cmd, data)
	if err != nil {
		if len(stderr) > 0 {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(stderr)))
		}
		return err
	}
	return nil
}

// Search 在 root 下按文件名子串（-iname）搜索，上限 max；永久跳过 .git，
// 且默认剪枝 node_modules（与本地内置排除对齐的最小集）。max<=0 视为 1。
func (f *FS) Search(ctx context.Context, root, query string, max int) ([]SearchHit, error) {
	if f == nil || f.Run == nil {
		return nil, fmt.Errorf("err.remote.fs_no_runner")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if max <= 0 {
		max = 1
	}
	r := normalizeRemote(root)
	pattern := "*" + escapeFindGlob(query) + "*"
	cmd := fmt.Sprintf(
		`LC_ALL=C find %s \( -name .git -o -name node_modules \) -prune -o -iname %s -printf '%%y\t%%P\n' | head -n %d`,
		shellSingleQuote(r),
		shellSingleQuote(pattern),
		max,
	)
	stdout, stderr, err := f.Run(ctx, f.Conn, cmd, nil)
	if err != nil {
		if len(stderr) > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(stderr)))
		}
		return nil, err
	}
	return parseSearchHits(r, stdout, max)
}

func parseSearchHits(root string, stdout []byte, max int) ([]SearchHit, error) {
	text := string(stdout)
	if text == "" {
		return nil, nil
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	out := make([]SearchHit, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 || parts[1] == "" {
			return nil, fmt.Errorf("err.remote.fs_parse|search")
		}
		rel := strings.ReplaceAll(parts[1], "\\", "/")
		name := path.Base(rel)
		abs := normalizeRemote(path.Join(root, rel))
		out = append(out, SearchHit{
			Name:    name,
			Path:    abs,
			RelPath: rel,
			IsDir:   parts[0] == "d",
		})
		if len(out) >= max {
			break
		}
	}
	return out, nil
}

// escapeFindGlob 转义 find -iname 模式中的通配符，使查询按字面子串匹配。
func escapeFindGlob(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`)
	return r.Replace(s)
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
