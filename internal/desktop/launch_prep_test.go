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
