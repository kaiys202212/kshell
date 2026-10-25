package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDetectUpgradeLaunch(t *testing.T) {
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)

	if !detectUpgradeLaunch([]string{"exe", "--after-update"}, now, false, false, now.Add(-time.Hour)) {
		t.Fatal("flag 应为升级")
	}
	if !detectUpgradeLaunch([]string{"exe"}, now, true, false, now.Add(-time.Hour)) {
		t.Fatal(".new 应为升级")
	}
	if !detectUpgradeLaunch([]string{"exe"}, now, false, false, now.Add(-30*time.Second)) {
		t.Fatal("近 mtime 应为升级")
	}
	if detectUpgradeLaunch([]string{"exe"}, now, false, false, now.Add(-time.Hour)) {
		t.Fatal("日常双击不应为升级")
	}
	// upgrade-old* 残留只在 mtime 窗口内才采信：补删失败时残留会永久存在，
	// 超窗残留不能把日常双击误判成升级。
	if !detectUpgradeLaunch([]string{"exe"}, now, false, true, now.Add(-30*time.Second)) {
		t.Fatal("残留 + 窗口内 mtime 应为升级")
	}
	if detectUpgradeLaunch([]string{"exe"}, now, false, true, now.Add(-time.Hour)) {
		t.Fatal("残留 + 超窗 mtime 不应为升级")
	}
}

func TestPrepareLaunchDailyNoopEvenWithPeers(t *testing.T) {
	var signals atomic.Int32
	var kills atomic.Int32
	err := prepareLaunchWith(launchPrepDeps{
		selfExe:    `C:\app\kshell-desktop.exe`,
		signalPath: filepath.Join(t.TempDir(), "exit.signal"),
		listPeers:  func() ([]int, error) { return []int{99}, nil },
		writeSignal: func(string) error {
			signals.Add(1)
			return nil
		},
		mutexFree:  func() bool { return false },
		killPIDs:   func([]int) error { kills.Add(1); return nil },
		sleep:      func(time.Duration) {},
		exeModTime: func(string) (time.Time, error) { return time.Now().Add(-time.Hour), nil },
		newExists:  func(string) bool { return false },
	}, []string{`C:\app\kshell-desktop.exe`})
	if err != nil {
		t.Fatal(err)
	}
	if signals.Load() != 0 {
		t.Fatalf("日常双击不应写 signal, got %d", signals.Load())
	}
	if kills.Load() != 0 {
		t.Fatalf("日常双击不应 kill, got %d", kills.Load())
	}
}

func TestPrepareLaunchUpgradeSignalsThenClears(t *testing.T) {
	var signals atomic.Int32
	var kills atomic.Int32
	calls := 0
	err := prepareLaunchWith(launchPrepDeps{
		selfExe:    `C:\app\kshell-desktop.exe`,
		signalPath: "sig",
		listPeers: func() ([]int, error) {
			calls++
			if calls == 1 {
				return []int{99}, nil
			}
			return nil, nil
		},
		writeSignal: func(string) error {
			signals.Add(1)
			return nil
		},
		mutexFree:  func() bool { return true },
		killPIDs:   func([]int) error { kills.Add(1); return nil },
		sleep:      func(time.Duration) {},
		exeModTime: func(string) (time.Time, error) { return time.Now().Add(-time.Hour), nil },
		newExists:  func(string) bool { return false },
		graceWait:  50 * time.Millisecond,
		pollEvery:  time.Millisecond,
	}, []string{`C:\app\kshell-desktop.exe`, "--after-update"})
	if err != nil {
		t.Fatal(err)
	}
	if signals.Load() != 1 {
		t.Fatalf("升级应写一次 signal, got %d", signals.Load())
	}
	if kills.Load() != 0 {
		t.Fatalf("等待成功后不应 kill, got %d", kills.Load())
	}
}

func TestPrepareLaunchUpgradeTimeoutKills(t *testing.T) {
	var kills atomic.Int32
	err := prepareLaunchWith(launchPrepDeps{
		selfExe:     `C:\app\kshell-desktop.exe`,
		signalPath:  "sig",
		listPeers:   func() ([]int, error) { return []int{99}, nil },
		writeSignal: func(string) error { return nil },
		mutexFree:   func() bool { return false },
		killPIDs: func(pids []int) error {
			kills.Add(1)
			if len(pids) != 1 || pids[0] != 99 {
				t.Fatalf("pids=%v", pids)
			}
			return nil
		},
		sleep:        func(time.Duration) {},
		exeModTime:   func(string) (time.Time, error) { return time.Now().Add(-time.Hour), nil },
		newExists:    func(string) bool { return false },
		graceWait:    5 * time.Millisecond,
		pollEvery:    time.Millisecond,
		postKillWait: 0,
	}, []string{`C:\app\kshell-desktop.exe`, "--after-update"})
	if err != nil {
		t.Fatal(err)
	}
	if kills.Load() != 1 {
		t.Fatalf("升级超时应 kill, got %d", kills.Load())
	}
}

func TestExeNewSiblingExists(t *testing.T) {
	// exe.new 是升级中间产物的无条件信号；upgrade-old* 残留不再无条件算数
	// （见 exeUpgradeOldResidualExists 的窗口门控），由独立函数负责。
	tests := []struct {
		name    string
		sibling string // 相对 exe 需创建的残留文件名；空表示不创建任何残留
		want    bool
	}{
		{name: "无残留为假"},
		{name: ".new 残留为真", sibling: ".new", want: true},
		{name: ".upgrade-old 不再无条件为真", sibling: ".upgrade-old"},
		{name: ".upgrade-old-ticks 不再无条件为真", sibling: ".upgrade-old-133900000000000000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			exe := filepath.Join(dir, "app.exe")
			if tt.sibling != "" {
				if err := os.WriteFile(exe+tt.sibling, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if got := exeNewSiblingExists(exe); got != tt.want {
				t.Fatalf("exeNewSiblingExists(%q) = %v, want %v", exe, got, tt.want)
			}
		})
	}
	if exeNewSiblingExists("") {
		t.Fatal("空 exe 应为假")
	}
}

func TestExeUpgradeOldResidualExists(t *testing.T) {
	// 改名交换会把旧 exe 改名 exe.upgrade-old（被占用时回退
	// exe.upgrade-old-<ticks>），前缀扫描需覆盖两种形态；upgrade-oldfoo
	// 也命中——这是我们自己的命名空间，可接受（前缀足够独特）。
	tests := []struct {
		name    string
		sibling string
		want    bool
	}{
		{name: "无残留为假"},
		{name: ".upgrade-old 残留为真", sibling: ".upgrade-old", want: true},
		{name: ".upgrade-old-ticks 残留为真", sibling: ".upgrade-old-133900000000000000", want: true},
		{name: "前缀语义 .upgrade-oldfoo 也为真", sibling: ".upgrade-oldfoo", want: true},
		{name: "无关文件不命中", sibling: "-other.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			exe := filepath.Join(dir, "app.exe")
			if tt.sibling != "" {
				if err := os.WriteFile(exe+tt.sibling, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if got := exeUpgradeOldResidualExists(exe); got != tt.want {
				t.Fatalf("exeUpgradeOldResidualExists(%q) = %v, want %v", exe, got, tt.want)
			}
		})
	}
	if exeUpgradeOldResidualExists("") {
		t.Fatal("空 exe 应为假")
	}
}

func TestIsUpgradeLaunchStaleUpgradeOldResidualNotUpgrade(t *testing.T) {
	// 集成锁定：残留存在时，是否升级取决于 exe mtime 是否在升级窗口内，
	// 防止补删失败的永久残留把日常双击误判成升级、清场误杀在跑实例。
	dir := t.TempDir()
	exe := filepath.Join(dir, "app.exe")
	if err := os.WriteFile(exe+".upgrade-old", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		modTime time.Time
		want    bool
	}{
		{name: "超窗残留不算升级", modTime: now.Add(-time.Hour)},
		{name: "窗口内残留算升级", modTime: now.Add(-30 * time.Second), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isUpgradeLaunch(launchPrepDeps{
				selfExe:          exe,
				exeModTime:       func(string) (time.Time, error) { return tt.modTime, nil },
				newExists:        exeNewSiblingExists,
				upgradeOldExists: exeUpgradeOldResidualExists,
				now:              func() time.Time { return now },
			}, []string{exe})
			if got != tt.want {
				t.Fatalf("isUpgradeLaunch = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPrepareLaunchUpgradeRemovesLeftoversOnEarlyExit(t *testing.T) {
	// defer 语义：升级拉起后即使首次枚举 peer 即失败提前返回，收尾仍补删。
	removeCalls := 0
	err := prepareLaunchWith(launchPrepDeps{
		selfExe:    `C:\app\kshell-desktop.exe`,
		signalPath: "sig",
		listPeers:  func() ([]int, error) { return nil, errors.New("boom") },
		writeSignal: func(string) error {
			t.Fatal("枚举失败不应写 signal")
			return nil
		},
		mutexFree: func() bool { return true },
		killPIDs: func([]int) error {
			t.Fatal("枚举失败不应 kill")
			return nil
		},
		sleep:            func(time.Duration) {},
		exeModTime:       func(string) (time.Time, error) { return time.Now().Add(-time.Hour), nil },
		newExists:        func(string) bool { return false },
		upgradeOldExists: func(string) bool { return false },
		removeUpgradeOld: func(string) error {
			removeCalls++
			return nil
		},
	}, []string{`C:\app\kshell-desktop.exe`, "--after-update"})
	if err != nil {
		t.Fatal(err)
	}
	if removeCalls != 1 {
		t.Fatalf("提前返回也应补删一次, got %d", removeCalls)
	}
}

func TestRemoveUpgradeOldLeftovers(t *testing.T) {
	countResiduals := func(t *testing.T, exe string) int {
		t.Helper()
		entries, err := os.ReadDir(filepath.Dir(exe))
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), filepath.Base(exe)+".upgrade-old") {
				n++
			}
		}
		return n
	}
	tests := []struct {
		name     string
		setup    func(t *testing.T, exe string)
		wantErr  bool
		wantLeft int
	}{
		{name: "无残留不报错"},
		{name: "多残留全删",
			setup: func(t *testing.T, exe string) {
				// 固化前缀语义：upgrade-old / 带时间戳 / 前缀粘连均删除
				for _, s := range []string{".upgrade-old", ".upgrade-old-123", ".upgrade-oldfoo"} {
					if err := os.WriteFile(exe+s, []byte("x"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			},
			wantLeft: 0,
		},
		{name: "部分失败返回错误",
			setup: func(t *testing.T, exe string) {
				if err := os.WriteFile(exe+".upgrade-old", []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				// 非空目录无法被 os.Remove 删除，用于制造失败项
				if err := os.MkdirAll(filepath.Join(exe+".upgrade-old-1", "inner"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
			wantErr:  true,
			wantLeft: 1, // 失败的非空目录保留
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exe := filepath.Join(t.TempDir(), "app.exe")
			if tt.setup != nil {
				tt.setup(t, exe)
			}
			err := removeUpgradeOldLeftovers(exe)
			if (err != nil) != tt.wantErr {
				t.Fatalf("removeUpgradeOldLeftovers err = %v, wantErr %v", err, tt.wantErr)
			}
			if got := countResiduals(t, exe); got != tt.wantLeft {
				t.Fatalf("剩余残留 %d, want %d", got, tt.wantLeft)
			}
		})
	}
	if err := removeUpgradeOldLeftovers(""); err != nil {
		t.Fatalf("空 exe 应安全返回: %v", err)
	}
}

func TestPrepareLaunchUpgradeRemovesLeftoversOnGraceClear(t *testing.T) {
	removeCalls := 0
	removedExe := ""
	killCalls := 0
	peerCalls := 0
	err := prepareLaunchWith(launchPrepDeps{
		selfExe:    `C:\app\kshell-desktop.exe`,
		signalPath: "sig",
		// 首次枚举仍有 peer，grace 轮询第二次即清空，让循环真正走
		// 「grace 内清空」分支退出，而不是落到超时强杀。
		listPeers: func() ([]int, error) {
			peerCalls++
			if peerCalls == 1 {
				return []int{99}, nil
			}
			return nil, nil
		},
		writeSignal: func(string) error { return nil },
		mutexFree:   func() bool { return true },
		killPIDs: func([]int) error {
			killCalls++
			return nil
		},
		sleep:      func(time.Duration) {},
		exeModTime: func(string) (time.Time, error) { return time.Now().Add(-time.Hour), nil },
		newExists:  func(string) bool { return false },
		removeUpgradeOld: func(exe string) error {
			removeCalls++
			removedExe = exe
			return nil
		},
		graceWait: 50 * time.Millisecond,
		pollEvery: time.Millisecond,
	}, []string{`C:\app\kshell-desktop.exe`, "--after-update"})
	if err != nil {
		t.Fatal(err)
	}
	if removeCalls != 1 {
		t.Fatalf("grace 清空后应补删一次残留, got %d", removeCalls)
	}
	if removedExe != `C:\app\kshell-desktop.exe` {
		t.Fatalf("补删应按 selfExe 定位, got %q", removedExe)
	}
	if killCalls != 0 {
		t.Fatalf("grace 内清空不应走到强杀, got %d", killCalls)
	}
}

func TestPrepareLaunchUpgradeRemovesLeftoversOnRecheckCleared(t *testing.T) {
	// 超时后最后一次枚举发现 peer 已自行消失：应走「复查清空」分支补删，
	// 而不是强杀。now 每次调用推进 1ms，保证循环次数与最终枚举时点确定。
	const graceMs = 5
	nowMs := 0
	removeCalls := 0
	killCalls := 0
	err := prepareLaunchWith(launchPrepDeps{
		selfExe:    `C:\app\kshell-desktop.exe`,
		signalPath: "sig",
		listPeers: func() ([]int, error) {
			// 截止前一直有 peer；超时后的复查枚举返回空。
			if nowMs < graceMs {
				return []int{99}, nil
			}
			return nil, nil
		},
		writeSignal: func(string) error { return nil },
		mutexFree:   func() bool { return false },
		killPIDs: func([]int) error {
			killCalls++
			return nil
		},
		sleep:      func(time.Duration) {},
		exeModTime: func(string) (time.Time, error) { return time.Now().Add(-time.Hour), nil },
		newExists:  func(string) bool { return false },
		removeUpgradeOld: func(string) error {
			removeCalls++
			return nil
		},
		graceWait: time.Duration(graceMs) * time.Millisecond,
		pollEvery: time.Millisecond,
		now: func() time.Time {
			nowMs++
			return time.Unix(0, 0).Add(time.Duration(nowMs) * time.Millisecond)
		},
	}, []string{`C:\app\kshell-desktop.exe`, "--after-update"})
	if err != nil {
		t.Fatal(err)
	}
	if removeCalls != 1 {
		t.Fatalf("超时复查 peer 已消失后应补删一次残留, got %d", removeCalls)
	}
	if killCalls != 0 {
		t.Fatalf("复查已清空不应强杀, got %d", killCalls)
	}
}

func TestPrepareLaunchUpgradeRemovesLeftoversOnTimeoutKill(t *testing.T) {
	removeCalls := 0
	err := prepareLaunchWith(launchPrepDeps{
		selfExe:     `C:\app\kshell-desktop.exe`,
		signalPath:  "sig",
		listPeers:   func() ([]int, error) { return []int{99}, nil },
		writeSignal: func(string) error { return nil },
		mutexFree:   func() bool { return false },
		killPIDs:    func([]int) error { return nil },
		sleep:       func(time.Duration) {},
		exeModTime:  func(string) (time.Time, error) { return time.Now().Add(-time.Hour), nil },
		newExists:   func(string) bool { return false },
		removeUpgradeOld: func(string) error {
			removeCalls++
			return nil
		},
		graceWait:    5 * time.Millisecond,
		pollEvery:    time.Millisecond,
		postKillWait: 0,
	}, []string{`C:\app\kshell-desktop.exe`, "--after-update"})
	if err != nil {
		t.Fatal(err)
	}
	if removeCalls != 1 {
		t.Fatalf("超时强杀后也应补删一次残留, got %d", removeCalls)
	}
}

func TestPrepareLaunchDailyDoesNotRemoveLeftovers(t *testing.T) {
	removeCalls := 0
	err := prepareLaunchWith(launchPrepDeps{
		selfExe:    `C:\app\kshell-desktop.exe`,
		signalPath: filepath.Join(t.TempDir(), "exit.signal"),
		listPeers:  func() ([]int, error) { return []int{99}, nil },
		writeSignal: func(string) error {
			t.Fatal("日常双击不应写 signal")
			return nil
		},
		mutexFree: func() bool { return false },
		killPIDs: func([]int) error {
			t.Fatal("日常双击不应 kill")
			return nil
		},
		sleep:      func(time.Duration) {},
		exeModTime: func(string) (time.Time, error) { return time.Now().Add(-time.Hour), nil },
		newExists:  func(string) bool { return false },
		removeUpgradeOld: func(string) error {
			removeCalls++
			return nil
		},
	}, []string{`C:\app\kshell-desktop.exe`})
	if err != nil {
		t.Fatal(err)
	}
	if removeCalls != 0 {
		// 日常双击绝不删文件：只有升级拉起路径才允许清残留。
		t.Fatalf("日常双击不应补删残留, got %d", removeCalls)
	}
}

func TestPrepareLaunchLeftoverRemovalFailureSwallowed(t *testing.T) {
	err := prepareLaunchWith(launchPrepDeps{
		selfExe:     `C:\app\kshell-desktop.exe`,
		signalPath:  "sig",
		listPeers:   func() ([]int, error) { return []int{99}, nil },
		writeSignal: func(string) error { return nil },
		mutexFree:   func() bool { return true },
		killPIDs:    func([]int) error { return nil },
		sleep:       func(time.Duration) {},
		exeModTime:  func(string) (time.Time, error) { return time.Now().Add(-time.Hour), nil },
		newExists:   func(string) bool { return false },
		removeUpgradeOld: func(string) error {
			return errors.New("simulate locked file")
		},
		graceWait: 50 * time.Millisecond,
		pollEvery: time.Millisecond,
	}, []string{`C:\app\kshell-desktop.exe`, "--after-update"})
	if err != nil {
		t.Fatalf("补删失败应静默忽略, got %v", err)
	}
}

func TestPrepareLaunchListPeersErrorDoesNotBlock(t *testing.T) {
	err := prepareLaunchWith(launchPrepDeps{
		selfExe:     "x",
		signalPath:  "sig",
		listPeers:   func() ([]int, error) { return nil, errors.New("boom") },
		writeSignal: func(string) error { return errors.New("不应调用") },
		mutexFree:   func() bool { return true },
		killPIDs:    func([]int) error { return errors.New("不应调用") },
		sleep:       func(time.Duration) {},
		exeModTime:  func(string) (time.Time, error) { return time.Time{}, os.ErrNotExist },
		newExists:   func(string) bool { return false },
	}, []string{"x", "--after-update"})
	if err != nil {
		t.Fatalf("枚举失败不应阻断启动: %v", err)
	}
}
