package desktop

import (
	"os"
	"path/filepath"
	"strings"
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
	selfExe    string
	signalPath string
	// listPeers 枚举参与升级清场的 GUI peer（Windows 侧按命令行白名单豁免
	// 助手进程）：单实例互斥锁只有 GUI 持有，改名交换后助手进程（mcp-archive
	// / mcp-ssh / agent-hook / --ssh-askpass）已不阻塞任何替换步骤，杀它们
	// 纯破坏在跑的 agent 会话；助手死活只影响残留（.upgrade-old*）补删时点，
	// 残留由下次升级脚本兜底，故清场一律零打扰。
	listPeers   func() ([]int, error)
	writeSignal func(path string) error
	mutexFree   func() bool
	killPIDs    func(pids []int) error
	sleep       func(d time.Duration)
	exeModTime  func(path string) (time.Time, error)
	// newExists 仅认 exe.new（升级中间产物的无条件信号）；upgrade-old* 残留
	// 不单列信号，是否升级由 detectUpgradeLaunch 的 recent-alone 统一覆盖。
	newExists func(exe string) bool
	// removeUpgradeOld 清场结束后尽力删除 exe.upgrade-old / exe.upgrade-old-*
	// 残留；仅升级拉起路径调用，日常双击绝不删文件。
	removeUpgradeOld func(exe string) error
	graceWait        time.Duration
	pollEvery        time.Duration
	postKillWait     time.Duration
	now              func() time.Time
}

// detectUpgradeLaunch 判定是否升级拉起。--after-update 参数是权威信号；
// exe.new 是升级中间产物的无条件信号。exe.upgrade-old* 残留不单列信号：
// 残留信号由 recent-alone（exe mtime 处于升级窗口内）覆盖，不单列——残留
// ⊆ 窗口内本就由 recent-alone 判真，单列只会重复；超窗残留则必须保持非
// 升级，它是改名交换的常规产物，补删/finally 失败时会永久残留，仅凭残留
// 判定会让之后每次日常双击都被误判成升级而清场误杀在跑实例。
func detectUpgradeLaunch(args []string, now time.Time, newExists bool, modTime time.Time) bool {
	for _, a := range args {
		if a == afterUpdateFlag {
			return true
		}
	}
	if newExists {
		return true
	}
	return !modTime.IsZero() && !now.Before(modTime) && now.Sub(modTime) <= upgradeModTimeWindow
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
	// 凡走升级清场路径，收尾必尽力补删改名交换残留（旧 exe 改名让位后以
	// upgrade-old* 形式存在，此时旧助手进程已被清理或退出，删除通常能成功）；
	// 失败静默忽略，留给下次升级兜底。defer 覆盖包括首次枚举即空/出错在内
	// 的所有出口。
	if d.removeUpgradeOld != nil {
		defer func() { _ = d.removeUpgradeOld(d.selfExe) }()
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

// exeNewSiblingExists 判断 exe 同目录是否存在升级中间产物 exe.new（旧覆盖式
// 脚本产物，存在即刚发生升级）。upgrade-old* 残留不在此判定也不单列信号：
// 它是改名交换的常规产物且可能因清理失败永久残留，是否升级由
// detectUpgradeLaunch 的 recent-alone（exe mtime 窗口）统一覆盖。
func exeNewSiblingExists(exe string) bool {
	if exe == "" {
		return false
	}
	_, err := os.Stat(exe + ".new")
	return err == nil
}

// upgradeOldResiduals 列出 exe 同目录下的 upgrade-old* 残留。用 ReadDir +
// HasPrefix 前缀匹配而非 Glob：exe 安装路径可能含 [ ] ? 等字符，Glob 会把
// 它们当模式元字符导致漏判。前缀语义刻意宽松（upgrade-oldfoo 也命中）——
// 这是 kshell 自己的命名空间，前缀足够独特，不会误伤他物。
func upgradeOldResiduals(exe string) []string {
	if exe == "" {
		return nil
	}
	dir := filepath.Dir(exe)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	prefix := filepath.Base(exe) + ".upgrade-old"
	var out []string
	for _, e := range entries {
		if name := e.Name(); strings.HasPrefix(name, prefix) {
			out = append(out, filepath.Join(dir, name))
		}
	}
	return out
}

// removeUpgradeOldLeftovers 尽力删除升级改名交换留下的旧 exe 残留。调用时机
// 在清场结束之后（旧助手进程已被清理或自然退出），文件不再被占用，删除通常
// 能成功；个别失败静默忽略，由下次升级脚本的 finally 兜底。
func removeUpgradeOldLeftovers(exe string) error {
	var firstErr error
	for _, m := range upgradeOldResiduals(exe) {
		if err := os.Remove(m); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
