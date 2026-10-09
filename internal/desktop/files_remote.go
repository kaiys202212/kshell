package desktop

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/remote"
	remotefs "github.com/yangk/kshell/internal/remote/fs"
	"github.com/yangk/kshell/internal/workspace"
)

var errRemoteFSOpUnsupported = errors.New("err.remote.fs_op_unsupported")

// remoteWS 是解析后的 SSH 工作区句柄。
type remoteWS struct {
	conn remote.Connection
	root string
	fs   *remotefs.FS
}

// parseWSRef 解析 wsPath：ssh Ref → remote；否则 localPath（已 Clean）。
// 必须在 filepath.Clean 之前调用，否则 Windows 会把 ssh:// 弄坏。
func (a *App) parseWSRef(wsPath string) (kind, localPath string, remote *remoteWS, err error) {
	wsPath = strings.TrimSpace(wsPath)
	kind, connID, remotePath, err := discovery.ParseWorkspaceRef(wsPath)
	if err != nil {
		return "", "", nil, err
	}
	if kind != discovery.KindSSH {
		return discovery.KindLocal, filepath.Clean(wsPath), nil, nil
	}
	c, ok := a.connByID(connID)
	if !ok {
		return "", "", nil, errConnNotFound
	}
	root := discovery.NormalizeRemotePath(remotePath)
	fs := &remotefs.FS{Conn: c, Opts: a.sshOptions(), Run: a.remoteRunner()}
	return discovery.KindSSH, "", &remoteWS{conn: c, root: root, fs: fs}, nil
}

func (a *App) remoteFSCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(a.sshCtx(), 60*time.Second)
}

func (a *App) listFilesRemote(r *remoteWS, relPath string, showAll bool) ([]workspace.Node, error) {
	abs, err := remotefs.ResolveUnderRoot(r.root, relPath)
	if err != nil {
		return nil, errPathOutsideWorkspace
	}
	ctx, cancel := a.remoteFSCtx()
	defer cancel()
	entries, err := r.fs.ListDir(ctx, abs)
	if err != nil {
		return nil, err
	}
	out := make([]workspace.Node, 0, len(entries))
	for _, e := range entries {
		if e.Name == ".git" {
			continue
		}
		if !showAll && remoteBuiltinSkip(e.Name) {
			continue
		}
		child := path.Join(abs, e.Name)
		out = append(out, workspace.Node{
			Name:  e.Name,
			Path:  child,
			IsDir: e.IsDir,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func remoteBuiltinSkip(name string) bool {
	switch name {
	case "node_modules", "vendor", "dist", "build":
		return true
	default:
		return false
	}
}

func (a *App) readFileForEditRemote(r *remoteWS, filePath string) (workspace.EditContent, error) {
	abs, err := remotefs.ResolveUnderRoot(r.root, filePath)
	if err != nil {
		return workspace.EditContent{}, errPathOutsideWorkspace
	}
	ctx, cancel := a.remoteFSCtx()
	defer cancel()
	data, err := r.fs.ReadFile(ctx, r.root, abs, workspace.MaxEditBytes)
	if err != nil {
		if strings.Contains(err.Error(), "err.remote.fs_too_large") {
			return workspace.EditContent{}, workspace.ErrFileTooLarge
		}
		return workspace.EditContent{}, err
	}
	return workspace.EditContentFromBytes(data)
}

func (a *App) saveFileRemote(r *remoteWS, filePath, text, eol string) error {
	abs, err := remotefs.ResolveUnderRoot(r.root, filePath)
	if err != nil {
		return errPathOutsideWorkspace
	}
	data, err := workspace.EncodeEditText(text, eol)
	if err != nil {
		return err
	}
	ctx, cancel := a.remoteFSCtx()
	defer cancel()
	return r.fs.WriteFile(ctx, r.root, abs, data)
}

func (a *App) readFileBytesRemote(r *remoteWS, filePath string) (FileBytes, error) {
	abs, err := remotefs.ResolveUnderRoot(r.root, filePath)
	if err != nil {
		return FileBytes{}, errPathOutsideWorkspace
	}
	ctx, cancel := a.remoteFSCtx()
	defer cancel()
	data, err := r.fs.ReadFile(ctx, r.root, abs, maxPreviewBytes)
	if err != nil {
		if strings.Contains(err.Error(), "err.remote.fs_too_large") {
			return FileBytes{}, fmt.Errorf("err.files.too_big_preview|%d|%d", maxPreviewBytes+1, maxPreviewBytes)
		}
		return FileBytes{}, err
	}
	return FileBytes{
		Base64: base64.StdEncoding.EncodeToString(data),
		Mime:   mimeByExt(abs),
		Size:   int64(len(data)),
	}, nil
}

func (a *App) searchFilesRemote(r *remoteWS, query string) ([]workspace.SearchHit, error) {
	ctx, cancel := a.remoteFSCtx()
	defer cancel()
	hits, err := r.fs.Search(ctx, r.root, query, 2000)
	if err != nil {
		return nil, err
	}
	out := make([]workspace.SearchHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, workspace.SearchHit{
			Node:    workspace.Node{Name: h.Name, Path: h.Path, IsDir: h.IsDir},
			RelPath: h.RelPath,
		})
	}
	if out == nil {
		out = []workspace.SearchHit{}
	}
	return out, nil
}

func (a *App) previewFileRemote(r *remoteWS, filePath string) (workspace.Preview, error) {
	abs, err := remotefs.ResolveUnderRoot(r.root, filePath)
	if err != nil {
		return workspace.Preview{}, errPathOutsideWorkspace
	}
	ctx, cancel := a.remoteFSCtx()
	defer cancel()
	const head = 512 * 1024
	data, err := r.fs.ReadFile(ctx, r.root, abs, head)
	if err != nil {
		// 超限仍返回头部：再以 head 读会失败；改用 head 作为上限已截断则不应 too_large。
		// ReadFile 用 limit+1 判定，恰好 head 字节的文件会通过。过大则报错——预览改为允许截断：
		if strings.Contains(err.Error(), "err.remote.fs_too_large") {
			data, err = a.readRemoteHead(ctx, r, abs, head)
			if err != nil {
				return workspace.Preview{}, err
			}
			return previewFromRemoteBytes(data, true), nil
		}
		return workspace.Preview{}, err
	}
	return previewFromRemoteBytes(data, false), nil
}

func (a *App) readRemoteHead(ctx context.Context, r *remoteWS, abs string, head int64) ([]byte, error) {
	// 直接 head -c head（不 +1），用于超大文件预览截断。
	cmd := fmt.Sprintf("LC_ALL=C head -c %d %s", head, shellSingleQuote(abs))
	stdout, stderr, err := r.fs.Run(ctx, r.conn, cmd, nil)
	if err != nil {
		if len(stderr) > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(stderr)))
		}
		return nil, err
	}
	return stdout, nil
}

func previewFromRemoteBytes(data []byte, forceTrunc bool) workspace.Preview {
	if len(data) == 0 {
		return workspace.Preview{Lines: []string{}, Text: "", Info: "preview.file_info|0|"}
	}
	if bytesContainNUL(data) {
		return workspace.Preview{
			Binary: true,
			Info:   fmt.Sprintf("preview.binary_file|%d|", len(data)),
		}
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !utf8.ValidString(text) {
		text = strings.ToValidUTF8(text, "�")
	}
	lines := strings.Split(text, "\n")
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	truncated := forceTrunc
	const maxLines = workspace.DefaultPreviewMaxLines
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		truncated = true
	}
	text = strings.Join(lines, "\n")
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		out = append(out, fmt.Sprintf("%4d │ %s", i+1, line))
	}
	return workspace.Preview{
		Lines:     out,
		Text:      text,
		Truncated: truncated,
		Info:      fmt.Sprintf("preview.file_info|%d|", len(data)),
	}
}

func bytesContainNUL(buf []byte) bool {
	limit := len(buf)
	if limit > 8000 {
		limit = 8000
	}
	for i := 0; i < limit; i++ {
		if buf[i] == 0 {
			return true
		}
	}
	return false
}
