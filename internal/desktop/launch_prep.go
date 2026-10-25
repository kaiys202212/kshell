package desktop

import (
	"os"
	"path/filepath"
	"time"

	"github.com/yangk/kshell/internal/config"
)

// 与 Wails Windows SetupSingleInstance 一致：mutexName = "wails-app-" + uniqueId + "sim"
const singleInstanceMutexName = "wails-app-kshell-desktopsim"

const (
	upgradeModTimeWindow = 2 * time.Minute
	defaultGraceWait     = 8 * time.Second
	defaultPollEvery     = 200 * time.Millisecond
	defaultPostKillWait  = 2 * time.Second
)

// afterUpdateFlag 由升级/重启脚本传给新进程，供 PrepareLaunch 识别升级拉起。
const afterUpdateFlag = "--after-update"

type launchPrepDeps struct {
	selfExe     string
	signalPath  string
	listPeers   func() ([]int, error)
	writeSignal func(path string) error
	mutexFree   func() bool
	killPIDs    func(pids []int) error
	sleep       func(d time.Duration)
	exeModTime  func(path string) (time.Time, error)
	newExists   func(exe string) bool
	// removeUpgradeOld 清场结束后尽力删除 exe.upgrade-old / exe.upgrade-old-*
	// 残留；仅升级拉起路径调用，日常双击绝不删文件。
	removeUpgradeOld func(exe string) error
	graceWait        time.Duration
	pollEvery        time.Duration
	postKillWait     time.Duration
	now              func() time.Time
}

func detectUpgradeLaunch(args []string, now time.Time, newExists bool, modTime time.Time) bool {
	for _, a := range args {
		if a == afterUpdateFlag {
			return true
		}
	}
	if newExists {
		return true
	}
	if !modTime.IsZero() && !now.Before(modTime) && now.Sub(modTime) <= upgradeModTimeWindow {
		return true
	}
	return false
}

func isUpgradeLaunch(d launchPrepDeps, args []string) bool {
	nowFn := d.now
	if nowFn == nil {
		nowFn = time.Now
	}
	modTime := time.Time{}
	if d.exeModTime != nil {
		modTime, _ = d.exeModTime(d.selfExe)
	}
	newEx := false
	if d.newExists != nil {
		newEx = d.newExists(d.selfExe)
	}
	return detectUpgradeLaunch(args, nowFn(), newEx, modTime)
}

// prepareLaunchWith 仅在升级拉起时清场：写 exit.signal → 等待 peer/Mutex；
// 超时后强杀。日常双击直接返回，把二次启动交给 Wails 单实例。
func prepareLaunchWith(d launchPrepDeps, args []string) error {
	if !isUpgradeLaunch(d, args) {
		return nil
	}
	if d.listPeers == nil {
		return nil
	}
	peers, err := d.listPeers()
	if err != nil || len(peers) == 0 {
		return nil
	}
	if d.writeSignal != nil && d.signalPath != "" {
		_ = d.writeSignal(d.signalPath)
	}

	// 升级脚本已改为「改名交换」，旧 exe 以 exe.upgrade-old* 形式残留；等
	// peer 清理完成（旧助手进程退出/被杀）后文件不再被占用，是补删的最佳
	// 时机。失败静默忽略，留给下次升级兜底。
	cleanupLeftovers := func() {
		if d.removeUpgradeOld != nil {
			_ = d.removeUpgradeOld(d.selfExe)
		}
	}

	grace := d.graceWait
	if grace <= 0 {
		grace = defaultGraceWait
	}
	poll := d.pollEvery
	if poll <= 0 {
		poll = defaultPollEvery
	}
	sleep := d.sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	nowFn := d.now
	if nowFn == nil {
		nowFn = time.Now
	}

	deadline := nowFn().Add(grace)
	for nowFn().Before(deadline) {
		clear, err := d.listPeers()
		if err == nil && len(clear) == 0 {
			if d.mutexFree == nil || d.mutexFree() {
				cleanupLeftovers()
				return nil
			}
		}
		sleep(poll)
	}

	peers, err = d.listPeers()
	if err != nil || len(peers) == 0 {
		cleanupLeftovers()
		return nil
	}
	if d.killPIDs != nil {
		_ = d.killPIDs(peers)
	}
	if d.postKillWait > 0 {
		sleep(d.postKillWait)
	}
	cleanupLeftovers()
	return nil
}

// PrepareLaunch 供 main 在 wails.Run 前调用：升级拉起时尽量清掉挡路的旧实例。
// 内部错误一律吞掉并返回 nil，避免把「无法启动」变成硬失败。
func PrepareLaunch(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		exe = ""
	}
	signalPath := ""
	if layout, err := config.Paths(); err == nil {
		signalPath = layout.SignalExit
	}
	_ = prepareLaunchWith(launchPrepDeps{
		selfExe:          exe,
		signalPath:       signalPath,
		listPeers:        listDesktopPeerPIDs,
		writeSignal:      writeExitSignalFile,
		mutexFree:        singleInstanceMutexFree,
		killPIDs:         killProcessIDs,
		sleep:            time.Sleep,
		exeModTime:       fileModTime,
		newExists:        exeNewSiblingExists,
		removeUpgradeOld: removeUpgradeOldLeftovers,
		graceWait:        defaultGraceWait,
		pollEvery:        defaultPollEvery,
		postKillWait:     defaultPostKillWait,
	}, args)
	return nil
}

func writeExitSignalFile(path string) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte("1"), 0o600)
}

func fileModTime(path string) (time.Time, error) {
	st, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	return st.ModTime(), nil
}

// exeNewSiblingExists 判断 exe 同目录是否存在升级中间产物。除了老的 exe.new
// （旧覆盖式脚本产物），改名交换脚本会留下 exe.upgrade-old / exe.upgrade-old-<ticks>
// （后者在旧文件被占用时启用），任一前缀残留都说明刚发生过升级拉起。
func exeNewSiblingExists(exe string) bool {
	if exe == "" {
		return false
	}
	if _, err := os.Stat(exe + ".new"); err == nil {
		return true
	}
	matches, err := filepath.Glob(exe + ".upgrade-old*")
	if err != nil {
		return false
	}
	return len(matches) > 0
}

// removeUpgradeOldLeftovers 尽力删除升级改名交换留下的旧 exe 残留。调用时机
// 在清场结束之后（旧助手进程已被清理或自然退出），文件不再被占用，删除通常
// 能成功；个别失败静默忽略，由下次升级脚本的 finally 兜底。
func removeUpgradeOldLeftovers(exe string) error {
	if exe == "" {
		return nil
	}
	matches, err := filepath.Glob(exe + ".upgrade-old*")
	if err != nil {
		return err
	}
	var firstErr error
	for _, m := range matches {
		if err := os.Remove(m); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
