package desktop

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/yangk/kshell/internal/discovery"
)

// fileWatchDebounce 是文件变更事件合并窗口；测试可缩短以加速断言。
var fileWatchDebounce = 300 * time.Millisecond

// fileWatcher 跟踪单个工作区根的 fsnotify 引用与防抖定时器。
type fileWatcher struct {
	refs    int
	w       *fsnotify.Watcher
	cancel  context.CancelFunc
	timer   *time.Timer
	timerMu sync.Mutex
	closed  bool  // 在 timerMu 下：close 后 schedule/AfterFunc 一律 no-op
	gen     uint64 // 在 timerMu 下：防 Stop()==false 时旧回调与新定时器双触发
	root    string
}

// StartFileWatch 为工作区启动递归文件监视（引用计数 +1）。
// 同一路径多次 Start 共享一个 watcher；防抖后作废树缓存并 Emit files:changed。
func (a *App) StartFileWatch(wsPath string) error {
	kind, local, _, err := a.parseWSRef(wsPath)
	if err != nil {
		return err
	}
	if kind == discovery.KindSSH {
		return nil // 远端无本地 fsnotify
	}
	cleaned := local
	if cleaned == "." || cleaned == "" {
		return errWorkspaceNotFound
	}
	if st, err := os.Stat(cleaned); err != nil || !st.IsDir() {
		return errWorkspaceNotFound
	}

	a.watchMu.Lock()
	defer a.watchMu.Unlock()

	if a.watches == nil {
		a.watches = make(map[string]*fileWatcher)
	}
	if fw, ok := a.watches[cleaned]; ok {
		fw.refs++
		return nil
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	if err := addRecursive(w, cleaned); err != nil {
		_ = w.Close()
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	fw := &fileWatcher{refs: 1, w: w, cancel: cancel, root: cleaned}
	a.watches[cleaned] = fw
	go a.runFileWatch(ctx, fw)
	return nil
}

// StopFileWatch 减少工作区监视引用；归零时关闭 watcher 并从 map 删除。
func (a *App) StopFileWatch(wsPath string) {
	kind, local, _, err := a.parseWSRef(wsPath)
	if err != nil || kind == discovery.KindSSH {
		return
	}
	cleaned := local
	if cleaned == "." || cleaned == "" {
		return
	}

	a.watchMu.Lock()
	defer a.watchMu.Unlock()

	fw, ok := a.watches[cleaned]
	if !ok {
		return
	}
	fw.refs--
	if fw.refs > 0 {
		return
	}
	a.closeFileWatcherLocked(cleaned, fw)
}

// stopAllFileWatches 退出时关闭全部监视器（防泄漏）。
func (a *App) stopAllFileWatches() {
	a.watchMu.Lock()
	defer a.watchMu.Unlock()
	for root, fw := range a.watches {
		a.closeFileWatcherLocked(root, fw)
	}
}

func (a *App) closeFileWatcherLocked(root string, fw *fileWatcher) {
	fw.cancel()
	fw.timerMu.Lock()
	fw.closed = true
	fw.gen++ // 作废进行中/未停住的 AfterFunc
	if fw.timer != nil {
		fw.timer.Stop() // false 时回调可能已在跑；靠 closed/gen 挡 Emit
		fw.timer = nil
	}
	fw.timerMu.Unlock()
	_ = fw.w.Close()
	delete(a.watches, root)
}

func (a *App) runFileWatch(ctx context.Context, fw *fileWatcher) {
	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-fw.w.Errors:
			if !ok {
				return
			}
			_ = err // 监视错误不向上抛；前端靠手动刷新兜底
		case ev, ok := <-fw.w.Events:
			if !ok {
				return
			}
			if pathHasGitSegment(ev.Name) {
				continue
			}
			// 新建目录需补 watch，否则其子树变更收不到
			if ev.Has(fsnotify.Create) {
				if st, err := os.Stat(ev.Name); err == nil && st.IsDir() && !pathHasGitSegment(ev.Name) {
					_ = fw.w.Add(ev.Name)
				}
			}
			a.scheduleFileWatchNotify(fw)
		}
	}
}

func (a *App) scheduleFileWatchNotify(fw *fileWatcher) {
	fw.timerMu.Lock()
	defer fw.timerMu.Unlock()
	if fw.closed {
		return
	}
	if fw.timer != nil {
		fw.timer.Stop() // false：旧回调可能仍执行，gen 递增后会被丢弃
	}
	fw.gen++
	gen := fw.gen
	root := fw.root
	fw.timer = time.AfterFunc(fileWatchDebounce, func() {
		fw.timerMu.Lock()
		if fw.closed || fw.gen != gen {
			fw.timerMu.Unlock()
			return
		}
		fw.timer = nil
		fw.timerMu.Unlock()
		a.invalidateTree(root)
		a.Emit("files:changed", map[string]string{"path": root})
	})
}

// addRecursive 递归 Add 目录；跳过名为 .git 的目录。
func addRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // 个别目录不可读时跳过，不拖垮整棵监视
		}
		if !d.IsDir() {
			return nil
		}
		if d.Name() == ".git" {
			return filepath.SkipDir
		}
		return w.Add(path)
	})
}

// pathHasGitSegment 判断路径任一段是否为 .git（忽略仓库元数据噪声）。
func pathHasGitSegment(path string) bool {
	cleaned := filepath.Clean(path)
	for cleaned != "" && cleaned != "." && cleaned != string(filepath.Separator) {
		if filepath.Base(cleaned) == ".git" {
			return true
		}
		parent := filepath.Dir(cleaned)
		if parent == cleaned {
			break
		}
		cleaned = parent
	}
	return false
}
