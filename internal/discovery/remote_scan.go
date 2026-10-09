package discovery

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/remote"
	remotefs "github.com/yangk/kshell/internal/remote/fs"
)

// RemoteRunner 远端命令执行器（与 remotefs.Runner 同签名），便于 discovery 不直接依赖 fs 包名。
type RemoteRunner = remotefs.Runner

// RemoteCacheDir 返回连接级远端会话缓存目录：~/.kshell/cache/remote/<connID>/。
// cacheRoot 一般为 paths.Cache；空则返回空串（调用方视为禁用持久缓存）。
func RemoteCacheDir(cacheRoot, connID string) string {
	if strings.TrimSpace(cacheRoot) == "" || strings.TrimSpace(connID) == "" {
		return ""
	}
	return filepath.Join(cacheRoot, "remote", connID)
}

// ScanRemoteSessions 经 Runner 在远端列会话文件 / 执行 enumerator，拉文件头后本地 ParseSession，
// 只保留 Workspace 归一化等于 remoteWS 的会话。
// cachePath 非空时按 mtime+size 门控复用解析（index.json）；空则不做持久缓存。
func ScanRemoteSessions(
	ctx context.Context,
	conn remote.Connection,
	opts ScanOptions,
	ps []providers.Provider,
	remoteWS string,
	run RemoteRunner,
	cachePath string,
) ([]providers.Session, error) {
	if run == nil {
		return nil, fmt.Errorf("err.remote.fs_no_runner")
	}
	if opts.MaxFiles <= 0 {
		opts.MaxFiles = defaultMaxFiles
	}
	if opts.HeadLimit <= 0 {
		opts.HeadLimit = defaultHeadLimit
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}
	wantWS := NormalizeRemotePath(remoteWS)

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	home, err := remoteEchoHome(ctx, conn, run)
	if err != nil {
		return nil, err
	}

	idx := newIndex()
	if cachePath != "" {
		idx = LoadIndex(cachePath)
	}

	var (
		sessions []providers.Session
		failed   []string
		files    int
	)

	for _, p := range ps {
		roots := remoteSessionRoots(p, home)
		pattern := p.SessionFilePattern()
		if pattern == "" || len(roots) == 0 {
			continue
		}
		for _, root := range roots {
			listed, lerr := remoteListSessionFiles(ctx, conn, run, root, pattern)
			if lerr != nil {
				failed = append(failed, fmt.Sprintf("%s: %v", p.DisplayName(), lerr))
				continue
			}
			for _, f := range listed {
				if files >= opts.MaxFiles {
					break
				}
				rel := strings.TrimPrefix(strings.TrimPrefix(f.path, root), "/")
				if pm, ok := p.(providers.PathMatcher); ok && !pm.MatchSessionRel(rel) {
					continue
				}
				files++
				s, serr := parseRemoteFile(ctx, conn, run, p, f, opts.HeadLimit, idx)
				if serr != nil {
					failed = append(failed, f.path)
					continue
				}
				if remoteWorkspaceKey(s.Workspace) != wantWS {
					continue
				}
				s.Workspace = remoteWorkspaceKey(s.Workspace)
				sessions = append(sessions, *s)
			}
		}
	}

	sessions = append(sessions, remoteEnumerate(ctx, conn, run, home, ps, wantWS, &failed)...)
	sessions = dedupeSessions(sessions)

	if cachePath != "" {
		idx.Version = indexVersion
		_ = idx.Save(cachePath) // 写失败不阻断结果
	}
	_ = failed // 部分失败已跳过；整连接失败只在拿不到 home 时返回
	return sessions, nil
}

type remoteFileMeta struct {
	path  string
	size  int64
	mtime int64 // UnixNano
}

func remoteEchoHome(ctx context.Context, conn remote.Connection, run RemoteRunner) (string, error) {
	stdout, stderr, err := run(ctx, conn, `echo -n "$HOME"`, nil)
	if err != nil {
		if len(stderr) > 0 {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(stderr)))
		}
		return "", err
	}
	home := strings.TrimSpace(string(stdout))
	if home == "" {
		stdout, stderr, err = run(ctx, conn, "pwd", nil)
		if err != nil {
			if len(stderr) > 0 {
				return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(stderr)))
			}
			return "", err
		}
		home = strings.TrimSpace(string(stdout))
	}
	if home == "" {
		return "", fmt.Errorf("err.remote.fs_empty_path")
	}
	return NormalizeRemotePath(home), nil
}

// remoteSessionRoots 把 provider 的 SessionRoots 转成 POSIX 远端路径；
// Cursor 无 SessionRoots 时用 projects 目录做文件兜底。
func remoteSessionRoots(p providers.Provider, home string) []string {
	home = NormalizeRemotePath(home)
	var out []string
	for _, r := range p.SessionRoots(home) {
		if r == "" {
			continue
		}
		out = append(out, NormalizeRemotePath(filepath.ToSlash(r)))
	}
	if len(out) == 0 && p.ID() == "cursor" && p.SessionFilePattern() != "" {
		out = append(out, path.Join(home, ".cursor", "projects"))
	}
	return out
}

func remoteListSessionFiles(ctx context.Context, conn remote.Connection, run RemoteRunner, root, pattern string) ([]remoteFileMeta, error) {
	// -name 用 basename glob；不存在的根 find 会非零退出，视为无文件。
	cmd := fmt.Sprintf(
		"LC_ALL=C find %s -type f -name %s -printf '%%s\\t%%T@\\t%%p\\n' 2>/dev/null || true",
		shellSingleQuote(root),
		shellSingleQuote(pattern),
	)
	stdout, stderr, err := run(ctx, conn, cmd, nil)
	if err != nil {
		if len(stderr) > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(stderr)))
		}
		return nil, err
	}
	return parseRemoteFindFiles(stdout)
}

func parseRemoteFindFiles(stdout []byte) ([]remoteFileMeta, error) {
	text := string(stdout)
	if text == "" {
		return nil, nil
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	out := make([]remoteFileMeta, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("err.remote.fs_parse|session_list")
		}
		size, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("err.remote.fs_parse|size: %w", err)
		}
		sec, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			return nil, fmt.Errorf("err.remote.fs_parse|mtime: %w", err)
		}
		p := NormalizeRemotePath(parts[2])
		out = append(out, remoteFileMeta{
			path:  p,
			size:  size,
			mtime: time.Unix(int64(sec), 0).UnixNano(),
		})
	}
	return out, nil
}

func parseRemoteFile(
	ctx context.Context,
	conn remote.Connection,
	run RemoteRunner,
	p providers.Provider,
	f remoteFileMeta,
	headLimit int,
	idx *Index,
) (*providers.Session, error) {
	if idx.shouldReuse(f.path, f.mtime, f.size) {
		s := idx.Entries[f.path].Session
		return &s, nil
	}
	limit := headLimit
	if strings.HasSuffix(f.path, ".json") && limit < defaultSingleFileHead {
		limit = defaultSingleFileHead
	}
	cmd := fmt.Sprintf("LC_ALL=C head -c %d %s", limit, shellSingleQuote(f.path))
	stdout, stderr, err := run(ctx, conn, cmd, nil)
	if err != nil {
		if len(stderr) > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(stderr)))
		}
		return nil, err
	}
	s, err := p.ParseSession(f.path, stdout)
	if err != nil || s == nil {
		return nil, err
	}
	idx.Entries[f.path] = Entry{MTime: f.mtime, Size: f.size, Session: *s}
	return s, nil
}

// remoteEnumerate 处理 OpenCode 等 DB 枚举：远端 command -v + db 查询，stdout 本地解析。
func remoteEnumerate(
	ctx context.Context,
	conn remote.Connection,
	run RemoteRunner,
	home string,
	ps []providers.Provider,
	wantWS string,
	failed *[]string,
) []providers.Session {
	var out []providers.Session
	for _, p := range ps {
		if _, ok := p.(providers.SessionEnumerator); !ok {
			continue
		}
		if p.ID() != "opencode" {
			// Cursor 等依赖本机 FS/SQLite 的枚举：远端改走文件兜底（remoteSessionRoots），此处跳过。
			continue
		}
		bin, berr := remoteWhich(ctx, conn, run, "opencode")
		if berr != nil || bin == "" {
			continue // CLI 不在远端不算整扫失败
		}
		sql := providers.OpencodeSessionsSQL()
		cmd := fmt.Sprintf("%s db %s --format json", shellSingleQuote(bin), shellSingleQuote(sql))
		stdout, stderr, err := run(ctx, conn, cmd, nil)
		if err != nil {
			msg := err.Error()
			if len(stderr) > 0 {
				msg = strings.TrimSpace(string(stderr))
			}
			*failed = append(*failed, fmt.Sprintf("%s 会话查询失败: %s", p.DisplayName(), msg))
			continue
		}
		got, perr := providers.ParseOpencodeSessionsJSON(stdout, path.Join(home, ".local/share/opencode/opencode.db"))
		if perr != nil {
			*failed = append(*failed, fmt.Sprintf("%s: %v", p.DisplayName(), perr))
			continue
		}
		for _, s := range got {
			if remoteWorkspaceKey(s.Workspace) == wantWS {
				// 远端路径保持 POSIX，避免 Windows 上 filepath.Clean 把 /home 改成 \home
				s.Workspace = remoteWorkspaceKey(s.Workspace)
				out = append(out, s)
			}
		}
	}
	return out
}

// remoteWorkspaceKey 把会话里的工作区路径按远端 POSIX 规则归一（含 ToSlash）。
func remoteWorkspaceKey(ws string) string {
	return NormalizeRemotePath(filepath.ToSlash(strings.TrimSpace(ws)))
}

func remoteWhich(ctx context.Context, conn remote.Connection, run RemoteRunner, name string) (string, error) {
	cmd := fmt.Sprintf("command -v %s", shellSingleQuote(name))
	stdout, _, err := run(ctx, conn, cmd, nil)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(stdout)), nil
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
