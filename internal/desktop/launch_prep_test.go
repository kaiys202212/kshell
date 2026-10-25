package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestDetectUpgradeLaunch(t *testing.T) {
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)

	if !detectUpgradeLaunch([]string{"exe", "--after-update"}, now, false, now.Add(-time.Hour)) {
		t.Fatal("flag 应为升级")
	}
	if !detectUpgradeLaunch([]string{"exe"}, now, true, now.Add(-time.Hour)) {
		t.Fatal(".new 应为升级")
	}
	if !detectUpgradeLaunch([]string{"exe"}, now, false, now.Add(-30*time.Second)) {
		t.Fatal("近 mtime 应为升级")
	}
	if detectUpgradeLaunch([]string{"exe"}, now, false, now.Add(-time.Hour)) {
		t.Fatal("日常双击不应为升级")
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

func TestExeNewSiblingExistsDetectsUpgradeOldLeftovers(t *testing.T) {
	// 升级脚本已改为「改名交换」：旧 exe 改名 exe.upgrade-old（被占用时回退
	// exe.upgrade-old-<ticks>），exe.new 不再长期存在，故 upgrade-old 前缀
	// 残留同样要视为升级拉起。
	tests := []struct {
		name    string
		sibling string // 相对 exe 需创建的残留文件名；空表示不创建任何残留
		want    bool
	}{
		{name: "无残留为假"},
		{name: ".new 残留为真", sibling: ".new", want: true},
		{name: ".upgrade-old 残留为真", sibling: ".upgrade-old", want: true},
		{name: ".upgrade-old-ticks 残留为真", sibling: ".upgrade-old-133900000000000000", want: true},
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

func TestPrepareLaunchUpgradeRemovesLeftoversOnGraceClear(t *testing.T) {
	removeCalls := 0
	removedExe := ""
	err := prepareLaunchWith(launchPrepDeps{
		selfExe:    `C:\app\kshell-desktop.exe`,
		signalPath: "sig",
		listPeers: func() ([]int, error) {
			return []int{99}, nil
		},
		writeSignal: func(string) error { return nil },
		mutexFree: func() bool {
			return true
		},
		killPIDs:  func([]int) error { return errors.New("等待成功后不应 kill") },
		sleep:     func(time.Duration) {},
		exeModTime: func(string) (time.Time, error) { return time.Now().Add(-time.Hour), nil },
		newExists: func(string) bool { return false },
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
		t.Fatalf("升级清场后应补删一次残留, got %d", removeCalls)
	}
	if removedExe != `C:\app\kshell-desktop.exe` {
		t.Fatalf("补删应按 selfExe 定位, got %q", removedExe)
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
