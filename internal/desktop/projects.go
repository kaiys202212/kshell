package desktop

import (
	"context"
	"errors"
	"strings"

	"github.com/yangk/kshell/internal/discovery"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// pickDirectory 是原生目录选择器的注入点：测试替换掉它，避免真弹窗卡住用例。
var pickDirectory = func(ctx context.Context, title string) (string, error) {
	if ctx == nil {
		return "", errNotReady
	}
	return wailsRuntime.OpenDirectoryDialog(ctx, wailsRuntime.OpenDialogOptions{
		Title:                title,
		CanCreateDirectories: true,
	})
}

// CreateProject 弹出原生目录选择器，选中即登记为项目（显示名取目录名）。
// 用户取消返回 ""（不算错误），前端据此不做任何提示。
func (a *App) CreateProject() (string, error) {
	a.mu.Lock()
	ctx, st := a.ctx, a.opts.Projects
	a.mu.Unlock()
	if st == nil {
		return "", errors.New("项目表未装配")
	}

	dir, err := pickDirectory(ctx, "选择项目目录")
	if err != nil {
		return "", err
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", nil // 用户取消
	}
	if !discovery.DirExists(dir) {
		return "", errors.New("目录不存在：" + dir)
	}
	if err := st.Add(dir); err != nil {
		return "", err
	}
	a.applyProjects()
	return dir, nil
}

// HideProject 逻辑删除项目：只是从列表隐藏并进回收站，磁盘内容不受影响。
func (a *App) HideProject(path string) error {
	a.mu.Lock()
	st := a.opts.Projects
	a.mu.Unlock()
	if st == nil {
		return errors.New("项目表未装配")
	}
	if err := st.Hide(path); err != nil {
		return err
	}
	a.applyProjects()
	return nil
}

// RestoreProject 把回收站里的项目还原回列表。
func (a *App) RestoreProject(path string) error {
	a.mu.Lock()
	st := a.opts.Projects
	a.mu.Unlock()
	if st == nil {
		return errors.New("项目表未装配")
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
		out = append(out, DeletedProjectView{
			Path:   d.Path,
			Name:   discovery.WorkspaceName(d.Path),
			At:     d.At.Format("2006-01-02T15:04:05Z07:00"),
			Exists: discovery.DirExists(d.Path),
		})
	}
	return out
}

// applyProjects 用项目表重算当前结果的工作区列表并广播变更。
// 必须从原始扫描结果派生：列表一旦被过滤掉，还原就再也拿不回那条项目。
func (a *App) applyProjects() {
	a.mu.Lock()
	var filtered []discovery.Workspace
	if a.result != nil {
		filtered = discovery.ApplyProjects(a.rawWorkspaces, a.opts.Projects, nil)
		a.result.Workspaces = filtered
	}
	res, raw, tools := a.result, a.rawWorkspaces, a.tools
	a.mu.Unlock()

	if res == nil {
		return
	}
	a.saveSnapshot(res, raw, tools)
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
