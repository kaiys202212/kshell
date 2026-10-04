package desktop

import (
	"context"
	"errors"
	"os"
	"testing"
)

func stubRestart(t *testing.T, spawnErr error) (*[]string, *int) {
	t.Helper()
	spawned := []string{}
	quitCalled := 0

	origSpawn := spawnSelfDelayed
	spawnSelfDelayed = func(exe string) error {
		spawned = append(spawned, exe)
		return spawnErr
	}
	t.Cleanup(func() { spawnSelfDelayed = origSpawn })

	origQuit := quitRuntime
	quitRuntime = func(context.Context) { quitCalled++ }
	t.Cleanup(func() { quitRuntime = origQuit })

	return &spawned, &quitCalled
}

func TestRestartAppSpawnsAndQuits(t *testing.T) {
	app, _, _ := newTestApp(t)
	spawned, quitCalled := stubRestart(t, nil)

	if err := app.RestartApp(context.Background()); err != nil {
		t.Fatalf("RestartApp error: %v", err)
	}
	if len(*spawned) != 1 {
		t.Fatalf("spawnSelfDelayed 调用次数 = %d, 期望 1", len(*spawned))
	}
	wantExe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	if (*spawned)[0] != wantExe {
		t.Fatalf("exe = %q, want %q", (*spawned)[0], wantExe)
	}
	if *quitCalled != 1 {
		t.Fatalf("quitRuntime = %d, want 1", *quitCalled)
	}
	if app.BeforeClose(context.Background()) {
		t.Fatal("RestartApp 后 BeforeClose 应放行")
	}
}

func TestRestartAppSpawnFailure(t *testing.T) {
	app, _, _ := newTestApp(t)
	_, quitCalled := stubRestart(t, errors.New("启动失败"))

	if err := app.RestartApp(context.Background()); err == nil {
		t.Fatal("spawn 失败应返回错误")
	}
	if *quitCalled != 0 {
		t.Fatal("spawn 失败不应 quitRuntime")
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.quitting {
		t.Fatal("spawn 失败不应置 quitting")
	}
}
