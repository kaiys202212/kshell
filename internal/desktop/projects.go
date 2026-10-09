package desktop

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/applang"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/remote"
	remotefs "github.com/yangk/kshell/internal/remote/fs"
)

// CreateProject 弹出原生目录选择器，选中即登记为项目（显示名取目录名）。
// 用户取消返回 ""（不算错误），前端据此不做任何提示。
func (a *App) CreateProject() (string, error) {
	a.mu.Lock()
	ctx, st := a.ctx, a.opts.Projects
	a.mu.Unlock()
	if st == nil {
		return "", errors.New("err.projects.not_ready")
	}

	dir, err := pickDirectory(ctx, applang.T("dialog.pick_project_dir"))
	if err != nil {
		return "", err
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", nil // 用户取消
	}
	if !discovery.DirExists(dir) {
		return "", fmt.Errorf("err.projects.dir_missing|%s", dir)
	}
	if err := st.Add(dir); err != nil {
		return "", err
	}
	a.applyProjects()
	return dir, nil
}

// RemoteDirEntryView 远端目录浏览条目（供前端 RemoteDirPicker）。
type RemoteDirEntryView struct {
	Name    string `json:"name"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"` // RFC3339
}

// RemoteDirListView 一次远端目录浏览结果（含解析后的当前绝对路径）。
type RemoteDirListView struct {
	Dir     string               `json:"dir"`
	Entries []RemoteDirEntryView `json:"entries"`
}

// AddSSHProject 把远端目录登记为 ssh 工作区，返回 FormatSSHRef。
// 会校验连接存在、路径非空，并经 RemoteRun 确认是目录。
func (a *App) AddSSHProject(connID, remotePath string) (string, error) {
	a.mu.Lock()
	st := a.opts.Projects
	a.mu.Unlock()
	if st == nil {
		return "", errors.New("err.projects.not_ready")
	}
	connID = strings.TrimSpace(connID)
	remotePath = strings.TrimSpace(remotePath)
	if connID == "" || remotePath == "" {
		return "", errors.New("err.projects.empty_path")
	}
	c, ok := a.connByID(connID)
	if !ok {
		return "", errConnNotFound
	}
	remotePath = discovery.NormalizeRemotePath(remotePath)

	ctx, cancel := context.WithTimeout(a.sshCtx(), 30*time.Second)
	defer cancel()
	if err := a.remoteEnsureDir(ctx, c, remotePath); err != nil {
		return "", err
	}
	if err := st.AddEntry(discovery.ProjectEntry{
		Kind:   discovery.KindSSH,
		ConnID: connID,
		Path:   remotePath,
	}); err != nil {
		return "", err
	}
	ref := discovery.FormatSSHRef(connID, remotePath)
	a.applyProjects()
	return ref, nil
}

// ListRemoteDir 列出远端目录一层条目。absDir 空时先解析远端 $HOME。
// 返回结构含解析后的 Dir，供前端路径栏与「确认当前目录」使用。
func (a *App) ListRemoteDir(connID, absDir string) (RemoteDirListView, error) {
	c, ok := a.connByID(connID)
	if !ok {
		return RemoteDirListView{}, errConnNotFound
	}
	ctx, cancel := context.WithTimeout(a.sshCtx(), 30*time.Second)
	defer cancel()

	dir := strings.TrimSpace(absDir)
	if dir == "" {
		home, err := a.remoteHome(ctx, c)
		if err != nil {
			return RemoteDirListView{}, err
		}
		dir = home
	}
	dir = discovery.NormalizeRemotePath(dir)

	fs := &remotefs.FS{Conn: c, Opts: a.sshOptions(), Run: a.remoteRunner()}
	entries, err := fs.ListDir(ctx, dir)
	if err != nil {
		return RemoteDirListView{}, err
	}
	out := make([]RemoteDirEntryView, 0, len(entries))
	for _, e := range entries {
		out = append(out, RemoteDirEntryView{
			Name:    e.Name,
			IsDir:   e.IsDir,
			Size:    e.Size,
			ModTime: e.ModTime.UTC().Format(time.RFC3339),
		})
	}
	return RemoteDirListView{Dir: dir, Entries: out}, nil
}

// HideProject 逻辑删除项目：只是从列表隐藏并进回收站，磁盘内容不受影响。
// path 可为本地路径或 ssh:// Ref。
func (a *App) HideProject(path string) error {
	a.mu.Lock()
	st := a.opts.Projects
	a.mu.Unlock()
	if st == nil {
		return errors.New("err.projects.not_ready")
	}
	if err := st.Hide(path); err != nil {
		return err
	}
	a.applyProjects()
	return nil
}

// RestoreProject 把回收站里的项目还原回列表。
// path 可为本地路径或 ssh:// Ref。
func (a *App) RestoreProject(path string) error {
	a.mu.Lock()
	st := a.opts.Projects
	a.mu.Unlock()
	if st == nil {
		return errors.New("err.projects.not_ready")
	}
	if err := st.Restore(path); err != nil {
		return err
	}
	a.applyProjects()
	return nil
}

// DeletedProjectView 是回收站条目的前端视图：多带一个「目录是否还在」的判断，
// 方便界面对已被物理删除的目录给出提示。
type DeletedProjectView struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	At     string `json:"at"` // RFC3339，前端自行本地化
	Exists bool   `json:"exists"`
}

// GetDeletedProjects 返回回收站列表（最近删除的在前）。
func (a *App) GetDeletedProjects() []DeletedProjectView {
	a.mu.Lock()
	st := a.opts.Projects
	a.mu.Unlock()
	if st == nil {
		return []DeletedProjectView{}
	}

	deleted := st.Deleted()
	out := make([]DeletedProjectView, 0, len(deleted))
	for _, d := range deleted {
		view := DeletedProjectView{
			Path:   d.Path,
			Name:   discovery.WorkspaceName(d.Path),
			At:     d.At.Format("2006-01-02T15:04:05Z07:00"),
			Exists: discovery.DirExists(d.Path),
		}
		if d.Kind == discovery.KindSSH {
			remote := discovery.NormalizeRemotePath(d.Path)
			view.Path = discovery.FormatSSHRef(d.ConnID, remote)
			view.Name = remoteBaseName(remote)
			view.Exists = true // 远端可达性不在回收站探测
		}
		out = append(out, view)
	}
	return out
}

// applyProjects 用项目表重算当前结果的工作区列表并广播变更。
// 必须从原始扫描结果派生：列表一旦被过滤掉，还原就再也拿不回那条项目。
func (a *App) applyProjects() {
	a.mu.Lock()
	hasResult := a.result != nil
	raw, st := a.rawWorkspaces, a.opts.Projects
	a.mu.Unlock()
	if !hasResult {
		return
	}
	// presentWorkspaces / snapshot 会再取锁，故先在锁外派生
	filtered := a.presentWorkspaces(discovery.ApplyProjects(raw, st, nil))

	a.mu.Lock()
	if a.result != nil {
		a.result.Workspaces = filtered
	}
	res, rawOut, tools := a.result, a.rawWorkspaces, a.tools
	a.mu.Unlock()

	if res == nil {
		return
	}
	a.saveSnapshot(res, rawOut, tools)
	a.Emit("projects:changed", map[string]any{"workspaces": filtered})
}

// saveSnapshot 落盘扫描快照。注意统一存**未叠加项目表**的工作区列表：
// 过滤只在读取/展示时做，否则「删除 → 重启 → 还原」会拿不回原始列表。
func (a *App) saveSnapshot(res *discovery.Result, raw []discovery.Workspace, tools []discovery.Tool) {
	if res == nil {
		return
	}
	snap := *res
	snap.Workspaces = raw
	if snap.Workspaces == nil {
		snap.Workspaces = []discovery.Workspace{}
	}
	_ = discovery.SaveSnapshot(a.snapshot().SnapshotPath, &snap, tools)
}

// presentWorkspaces 把内部 Workspace（ssh 的 Path 仍是远端路径）转成前端 DTO：
// Path=FormatSSHRef、RemotePath=远端路径、ConnName 从连接表填充。
func (a *App) presentWorkspaces(list []discovery.Workspace) []discovery.Workspace {
	if len(list) == 0 {
		return list
	}
	out := make([]discovery.Workspace, len(list))
	copy(out, list)
	for i := range out {
		if out[i].Kind != discovery.KindSSH {
			if out[i].Kind == "" {
				out[i].Kind = discovery.KindLocal
			}
			continue
		}
		remotePath := out[i].RemotePath
		if remotePath == "" {
			remotePath = out[i].Path
		}
		// 若 Path 已是 Ref（例如事件回灌），解析出远端路径
		if strings.HasPrefix(remotePath, "ssh://") {
			_, _, p, err := discovery.ParseWorkspaceRef(remotePath)
			if err == nil {
				remotePath = p
			}
		}
		remotePath = discovery.NormalizeRemotePath(remotePath)
		out[i].RemotePath = remotePath
		out[i].Path = discovery.FormatSSHRef(out[i].ConnID, remotePath)
		if c, ok := a.connByID(out[i].ConnID); ok {
			out[i].ConnName = c.Name
		}
	}
	return out
}

func (a *App) sshCtx() context.Context {
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (a *App) remoteRunner() remotefs.Runner {
	a.mu.Lock()
	run := a.opts.RemoteRun
	a.mu.Unlock()
	if run != nil {
		return run
	}
	opts := a.sshOptions()
	return func(ctx context.Context, conn remote.Connection, cmd string, stdin []byte) ([]byte, []byte, error) {
		res, err := remote.RunWithStdin(ctx, conn, cmd, stdin, opts)
		out, errout := []byte(res.Stdout), []byte(res.Stderr)
		if err != nil {
			return out, errout, err
		}
		if res.ExitCode != 0 {
			return out, errout, fmt.Errorf("err.ssh.exit|%d", res.ExitCode)
		}
		return out, errout, nil
	}
}

func (a *App) remoteHome(ctx context.Context, c remote.Connection) (string, error) {
	run := a.remoteRunner()
	stdout, stderr, err := run(ctx, c, `echo -n "$HOME"`, nil)
	if err != nil {
		if len(stderr) > 0 {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(stderr)))
		}
		return "", err
	}
	home := strings.TrimSpace(string(stdout))
	if home == "" {
		stdout, stderr, err = run(ctx, c, "pwd", nil)
		if err != nil {
			if len(stderr) > 0 {
				return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(stderr)))
			}
			return "", err
		}
		home = strings.TrimSpace(string(stdout))
	}
	if home == "" {
		return "", errors.New("err.remote.fs_empty_path")
	}
	return discovery.NormalizeRemotePath(home), nil
}

func (a *App) remoteEnsureDir(ctx context.Context, c remote.Connection, absDir string) error {
	run := a.remoteRunner()
	cmd := fmt.Sprintf("test -d %s && echo -n ok", shellSingleQuote(absDir))
	stdout, stderr, err := run(ctx, c, cmd, nil)
	if err != nil {
		if len(stderr) > 0 {
			return fmt.Errorf("err.projects.dir_missing|%s: %s", absDir, strings.TrimSpace(string(stderr)))
		}
		return fmt.Errorf("err.projects.dir_missing|%s", absDir)
	}
	if strings.TrimSpace(string(stdout)) != "ok" {
		return fmt.Errorf("err.projects.dir_missing|%s", absDir)
	}
	return nil
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func remoteBaseName(p string) string {
	clean := discovery.NormalizeRemotePath(p)
	base := path.Base(clean)
	if base == "/" || base == "." {
		return clean
	}
	return base
}
