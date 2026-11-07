package desktop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/yangk/kshell/internal/discovery"
)

// fileWatchDebounce 是工作区文件变更事件合并窗口；测试可缩短以加速断言。
var fileWatchDebounce = 300 * time.Millisecond

// gitWatchDebounce 是 .git 元数据（HEAD/index/refs）变更合并窗口；与文件树分开，
// 避免 git add 触发整树重建，也避免文件风暴拖住色标刷新。
var gitWatchDebounce = 300 * time.Millisecond

// watchSkipNames showAll=false 时默认不递归监视的目录名（与 config 默认 Exclude / ignore 内置对齐）。
// showAll=true 时整体收缩为只跳过 .git：树里显示什么就监视什么，否则排除目录永远不刷新。
var watchSkipNames = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true,
}

// fileWatcher 跟踪单个工作区根的 fsnotify 引用与防抖定时器。
type fileWatcher struct {
	refs    int
	w       *fsnotify.Watcher
	cancel  context.CancelFunc
	timerMu sync.Mutex
	closed  bool // 在 timerMu 下：close 后 schedule/AfterFunc 一律 no-op

	fileTimer *time.Timer
	fileGen   uint64
	gitTimer  *time.Timer
	gitGen    uint64

	root    string
	showAll bool            // 监视范围跟随「显示全部」：true 时排除目录也纳入监视
	skip    map[string]bool // 目录 basename → 跳过递归监视
}

// StartFileWatch 为工作区启动递归文件监视（引用计数 +1）。
// 同一路径多次 Start 共享一个 watcher；showAll 变化时重建（显示什么就监视什么，
// 修「显示全部默认勾选但排除目录永不刷新」的洞）。防抖后作废树缓存并 Emit files:changed。
// .git 仅浅监视元数据路径，变更走 git:changed（不重建文件树）。
func (a *App) StartFileWatch(wsPath string, showAll bool) error {
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
		if fw.showAll == showAll {
			fw.refs++
			return nil
		}
		// showAll 变化：整体重建，避免运行中改 skip map 的竞态。
		// 假定每路径单消费者（当前仅 FileTree 一个实例）：重建后 refs 从 1 起，
		// 若将来出现第二个消费方，须改为保留 refs 并同步重建。
		a.closeFileWatcherLocked(cleaned, fw)
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	skip := a.watchSkipMap(showAll)
	if err := addRecursive(w, cleaned, skip); err != nil {
		_ = w.Close()
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	fw := &fileWatcher{refs: 1, w: w, cancel: cancel, root: cleaned, showAll: showAll, skip: skip}
	a.watches[cleaned] = fw
	go a.runFileWatch(ctx, fw)
	return nil
}

// watchSkipMap 按 showAll 返回跳过名单：
// showAll=false 跳过内置排除目录 + 用户 config.Exclude（减事件量）；
// showAll=true 只跳过 .git——树里显示什么就监视什么（.git 元数据仍走 git:changed 分流）。
func (a *App) watchSkipMap(showAll bool) map[string]bool {
	if showAll {
		return map[string]bool{".git": true}
	}
	out := make(map[string]bool, len(watchSkipNames)+4)
	for k, v := range watchSkipNames {
		out[k] = v
	}
	for _, name := range a.snapshot().Config.Exclude {
		if name = filepath.Base(name); name != "" && name != "." && name != ".." {
			out[name] = true
		}
	}
	return out
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
	fw.fileGen++
	fw.gitGen++
	if fw.fileTimer != nil {
		fw.fileTimer.Stop()
		fw.fileTimer = nil
	}
	if fw.gitTimer != nil {
		fw.gitTimer.Stop()
		fw.gitTimer = nil
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
			// 溢出等错误：强制走一次文件树刷新，避免静默漏刷
			_ = err
			a.scheduleFileWatchNotify(fw)
		case ev, ok := <-fw.w.Events:
			if !ok {
				return
			}
			// HEAD/index/refs 优先：父目录监视也会收到 .git 目录本身的 modify，须先分流元数据
			if isGitMetaPath(ev.Name) {
				a.scheduleGitWatchNotify(fw)
				continue
			}
			// 排除目录及其子树（含「只动到排除目录自身 mtime」的父级事件）
			if pathUnderWatchSkip(ev.Name, fw.root, fw.skip) {
				continue
			}
			if pathHasGitSegment(ev.Name) {
				continue // .git/objects 等未归入元数据的噪声
			}
			// 新建目录递归补 watch（跳过排除名），否则其子树变更收不到
			if ev.Has(fsnotify.Create) {
				if st, err := os.Stat(ev.Name); err == nil && st.IsDir() {
					_ = addRecursive(fw.w, ev.Name, fw.skip)
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
	if fw.fileTimer != nil {
		fw.fileTimer.Stop()
	}
	fw.fileGen++
	gen := fw.fileGen
	root := fw.root
	fw.fileTimer = time.AfterFunc(fileWatchDebounce, func() {
		fw.timerMu.Lock()
		if fw.closed || fw.fileGen != gen {
			fw.timerMu.Unlock()
			return
		}
		fw.fileTimer = nil
		fw.timerMu.Unlock()
		a.invalidateTree(root)
		a.Emit("files:changed", map[string]string{"path": root})
	})
}

func (a *App) scheduleGitWatchNotify(fw *fileWatcher) {
	fw.timerMu.Lock()
	defer fw.timerMu.Unlock()
	if fw.closed {
		return
	}
	if fw.gitTimer != nil {
		fw.gitTimer.Stop()
	}
	fw.gitGen++
	gen := fw.gitGen
	root := fw.root
	fw.gitTimer = time.AfterFunc(gitWatchDebounce, func() {
		fw.timerMu.Lock()
		if fw.closed || fw.gitGen != gen {
			fw.timerMu.Unlock()
			return
		}
		fw.gitTimer = nil
		fw.timerMu.Unlock()
		a.Emit("git:changed", map[string]string{"path": root})
	})
}

// addRecursive 递归 Add 目录；跳过 skip 名单（含 .git）；遇到 .git 目录时浅挂元数据监视。
func addRecursive(w *fsnotify.Watcher, root string, skip map[string]bool) error {
	if skip == nil {
		skip = watchSkipNames
	}
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // 个别目录不可读时跳过，不拖垮整棵监视
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		if name == ".git" {
			addGitMetaWatches(w, path)
			return filepath.SkipDir
		}
		if skip[name] && path != root {
			return filepath.SkipDir
		}
		return w.Add(path)
	})
}

// addGitMetaWatches 只挂 .git 根与 refs 浅层，不进 objects（控性能）。
func addGitMetaWatches(w *fsnotify.Watcher, gitDir string) {
	_ = w.Add(gitDir)
	refs := filepath.Join(gitDir, "refs")
	if st, err := os.Stat(refs); err == nil && st.IsDir() {
		_ = w.Add(refs)
	}
	heads := filepath.Join(gitDir, "refs", "heads")
	if st, err := os.Stat(heads); err == nil && st.IsDir() {
		_ = w.Add(heads)
	}
}

// pathUnderWatchSkip 判断事件路径相对 root 的任一路段是否在 skip 名单
//（例如 node_modules 自身被改 mtime，或子文件路径冒泡到已监视父目录）。
func pathUnderWatchSkip(path, root string, skip map[string]bool) bool {
	if skip == nil {
		skip = watchSkipNames
	}
	rel, err := filepath.Rel(root, filepath.Clean(path))
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return false
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." {
			continue
		}
		if skip[seg] {
			return true
		}
	}
	return false
}

// isGitMetaPath 是否为会影响 porcelain/分支展示的 .git 元数据路径。
func isGitMetaPath(path string) bool {
	cleaned := filepath.Clean(path)
	gitDir, ok := gitDirOf(cleaned)
	if !ok {
		return false
	}
	rel, err := filepath.Rel(gitDir, cleaned)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	switch rel {
	case "HEAD", "index", "COMMIT_EDITMSG", "FETCH_HEAD", "ORIG_HEAD", "packed-refs":
		return true
	}
	return rel == "refs" || hasPathPrefix(rel, "refs/")
}

func gitDirOf(path string) (string, bool) {
	cleaned := filepath.Clean(path)
	for cleaned != "" && cleaned != "." && cleaned != string(filepath.Separator) {
		if filepath.Base(cleaned) == ".git" {
			return cleaned, true
		}
		parent := filepath.Dir(cleaned)
		if parent == cleaned {
			break
		}
		cleaned = parent
	}
	return "", false
}

func hasPathPrefix(p, prefix string) bool {
	return len(p) >= len(prefix) && p[:len(prefix)] == prefix
}

// pathHasGitSegment 判断路径任一段是否为 .git（忽略仓库元数据噪声）。
func pathHasGitSegment(path string) bool {
	_, ok := gitDirOf(path)
	return ok
}
