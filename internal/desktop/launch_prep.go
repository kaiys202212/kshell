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
	selfExe      string
	signalPath   string
	listPeers    func() ([]int, error)
	writeSignal  func(path string) error
	mutexFree    func() bool
	killPIDs     func(pids []int) error
	sleep        func(d time.Duration)
	exeModTime   func(path string) (time.Time, error)
	newExists    func(exe string) bool
	graceWait    time.Duration
	pollEvery    time.Duration
	postKillWait time.Duration
	now          func() time.Time
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
				return nil
			}
		}
		sleep(poll)
	}

	peers, err = d.listPeers()
	if err != nil || len(peers) == 0 {
		return nil
	}
	if d.killPIDs != nil {
		_ = d.killPIDs(peers)
	}
	if d.postKillWait > 0 {
		sleep(d.postKillWait)
	}
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
		selfExe:      exe,
		signalPath:   signalPath,
		listPeers:    listDesktopPeerPIDs,
		writeSignal:  writeExitSignalFile,
		mutexFree:    singleInstanceMutexFree,
		killPIDs:     killProcessIDs,
		sleep:        time.Sleep,
		exeModTime:   fileModTime,
		newExists:    exeNewSiblingExists,
		graceWait:    defaultGraceWait,
		pollEvery:    defaultPollEvery,
		postKillWait: defaultPostKillWait,
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

func exeNewSiblingExists(exe string) bool {
	if exe == "" {
		return false
	}
	_, err := os.Stat(exe + ".new")
	return err == nil
}
