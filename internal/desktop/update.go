package desktop

import (
	"context"
	"fmt"
	"os"

	"github.com/yangk/kshell/internal/update"
	"github.com/yangk/kshell/internal/version"
)

// UpdateInfo 给前端的检查结果（JSON 字段名与绑定一致）。
type UpdateInfo struct {
	Current   string
	Latest    string
	Notes     string
	Source    string
	Available bool
	Skipped   bool
	Reason    string
}

var checkUpdateFn = func() (update.CheckResult, error) {
	return update.Client{Current: version.Current()}.Check(context.Background())
}

var applyUpdateFn = func(ctx context.Context, dest string, r update.CheckResult) error {
	return update.Apply(ctx, update.ApplyOptions{
		ZipURL:  r.ZipURL,
		SumsURL: r.SumsURL,
		DestExe: dest,
	})
}

var executablePath = os.Executable

func toUpdateInfo(r update.CheckResult) UpdateInfo {
	return UpdateInfo{
		Current:   r.Current,
		Latest:    r.Latest,
		Notes:     r.Notes,
		Source:    r.Source,
		Available: r.Available,
		Skipped:   r.Skipped,
		Reason:    r.Reason,
	}
}

// GetAppVersion 返回当前构建注入的版本。
func (a *App) GetAppVersion() string { return version.Current() }

// CheckForUpdate 按代理链检查 GitHub latest；有更新时推送 update:available。
func (a *App) CheckForUpdate() (UpdateInfo, error) {
	r, err := checkUpdateFn()
	if err != nil {
		return UpdateInfo{Current: version.Current()}, err
	}
	info := toUpdateInfo(r)
	if info.Available {
		a.Emit("update:available", info)
	}
	return info, nil
}

// ApplyUpdate 再次确认有更新后下载替换，成功则退出当前进程。
func (a *App) ApplyUpdate(ctx context.Context) error {
	r, err := checkUpdateFn()
	if err != nil {
		return err
	}
	if !r.Available {
		return fmt.Errorf("没有可用更新")
	}
	exe, err := executablePath()
	if err != nil {
		return err
	}
	if err := applyUpdateFn(ctx, exe, r); err != nil {
		return err
	}
	a.quitApp(ctx)
	return nil
}
