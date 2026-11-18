package desktop

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/update"
	"github.com/yangk/kshell/internal/version"
)

// checkOverallTimeout 限制单次检查的整体耗时：跨源取最大后不再提前返回，
// 6 个源 × 单源 12s 的最坏串行等待对同步 UI 调用不可接受。
const checkOverallTimeout = 25 * time.Second

// UpdateInfo 给前端的检查结果（JSON 字段名与绑定一致）。
type UpdateInfo struct {
	Current   string
	Latest    string
	Notes     string
	Source    string
	Available bool
	// Ready 表示当前平台的升级包已预下载并通过 SHA-256 校验，可直接安装。
	Ready   bool
	Skipped bool
	Reason  string
}

// updateCacheDir 返回预下载缓存目录；优先 opts.Home（测试隔离），
// 解析失败返回空串（缓存失效但升级仍可用）。
func (a *App) updateCacheDir() string {
	if a.opts.Home != "" {
		return filepath.Join(a.opts.Home, ".kshell", "update")
	}
	p, err := config.Paths()
	if err != nil {
		return ""
	}
	return p.Update
}

var checkUpdateFn = func() (update.CheckResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), checkOverallTimeout)
	defer cancel()
	return update.Client{Current: version.Current()}.Check(ctx)
}

var prepareUpdateFn = func(ctx context.Context, r update.CheckResult, cacheDir string) error {
	return update.Prepare(ctx, update.PrepareOptions{
		Tag:      r.Latest,
		ZipURL:   r.ZipURL,
		SumsURL:  r.SumsURL,
		CacheDir: cacheDir,
	})
}

var applyUpdateFn = func(ctx context.Context, dest string, r update.CheckResult, cacheDir string) error {
	return update.Apply(ctx, update.ApplyOptions{
		Tag:      r.Latest,
		ZipURL:   r.ZipURL,
		SumsURL:  r.SumsURL,
		DestExe:  dest,
		CacheDir: cacheDir,
	})
}

var executablePath = os.Executable

func (a *App) toUpdateInfo(r update.CheckResult) UpdateInfo {
	ready := r.Available && update.IsCached(a.updateCacheDir(), r.Latest)
	return UpdateInfo{
		Current:   r.Current,
		Latest:    r.Latest,
		Notes:     r.Notes,
		Source:    r.Source,
		Available: r.Available,
		Ready:     ready,
		Skipped:   r.Skipped,
		Reason:    r.Reason,
	}
}

// GetAppVersion 返回当前构建注入的版本号。
func (a *App) GetAppVersion() string { return version.Current() }

// CheckForUpdate 只查询版本信息，不下载、不弹窗；供设置页手动检查。
func (a *App) CheckForUpdate() (UpdateInfo, error) {
	r, err := checkUpdateFn()
	if err != nil {
		return UpdateInfo{Current: version.Current()}, err
	}
	return a.toUpdateInfo(r), nil
}

// checkAndPrepare 是自动轮询入口：有新版本时先下载并校验当前平台包，
// 成功后才推 update:available（前端据此弹「安装并重启」）。失败静默等下轮。
func (a *App) checkAndPrepare() {
	r, err := checkUpdateFn()
	if err != nil || !r.Available {
		return
	}
	if err := a.prepare(r); err != nil {
		log.Printf("update: 预下载 %s 失败: %v", r.Latest, err)
		return
	}
	info := a.toUpdateInfo(r)
	if !info.Ready {
		return
	}
	a.Emit("update:available", info)
}

// prepare 串行下载并校验升级包；已缓存则直接复用。
func (a *App) prepare(r update.CheckResult) error {
	a.updateMu.Lock()
	defer a.updateMu.Unlock()
	dir := a.updateCacheDir()
	if update.IsCached(dir, r.Latest) {
		return nil
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return prepareUpdateFn(ctx, r, dir)
}

// PrepareUpdate 供设置页「下载并安装」：先确认有更新，再下载校验到缓存。
func (a *App) PrepareUpdate() error {
	r, err := checkUpdateFn()
	if err != nil {
		return err
	}
	if !r.Available {
		return fmt.Errorf("err.update.no_update")
	}
	return a.prepare(r)
}

// ApplyUpdate 再次确认有更新后安装（优先用已预下载的包），成功则退出当前进程。
// 签名不带 context.Context：Wails v2 绑定会把 ctx 计成一个入参，
// 而前端以 0 参调用，导致 "received 0 arguments, expected 1"。
func (a *App) ApplyUpdate() error {
	dir := a.updateCacheDir()
	// 弹窗只会在预下载就绪后出现；优先安装那份已就绪的缓存包。
	cached := ""
	if tag := update.CachedTag(dir); tag != "" && update.IsCached(dir, tag) &&
		(!version.IsRelease() || update.Newer(version.Current(), tag)) {
		cached = tag
	}
	r, err := checkUpdateFn()
	switch {
	case err != nil:
		// 网络抖动时仍允许安装已预下载并校验过的包（弹窗本就基于该包出现）。
		if cached == "" {
			return err
		}
		r = update.CheckResult{Current: version.Current(), Latest: cached, Available: true}
	case !r.Available:
		if cached == "" {
			return fmt.Errorf("err.update.no_update")
		}
		r = update.CheckResult{Current: version.Current(), Latest: cached, Available: true}
	case cached != "" && update.Newer(cached, r.Latest):
		// 检查到更新的版本但还没预下载：仍安装用户确认过的缓存版本，
		// 避免「弹窗说已就绪」却因新版本缺包/下载失败而装不上。
		r = update.CheckResult{Current: version.Current(), Latest: cached, Available: true}
	}
	exe, err := executablePath()
	if err != nil {
		return err
	}
	// 与自动预下载共用锁：避免两份缓存写入交叉。
	a.updateMu.Lock()
	defer a.updateMu.Unlock()
	if err := applyUpdateFn(a.ctx, exe, r, dir); err != nil {
		return err
	}
	a.quitApp(a.ctx)
	return nil
}
