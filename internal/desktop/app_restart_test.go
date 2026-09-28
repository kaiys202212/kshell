package desktop

import (
	"context"
	"errors"
	"os"
	"testing"
)

// 注入桩统一收口：替换 spawnSelf / quitRuntime 并用 t.Cleanup 还原。
// RestartApp 的测试严禁真 spawn 进程——spawnSelf 必须先于调用被替换。
func stubRestart(t *testing.T, spawnErr error) (*[]string, *int) {
	t.Helper()
	spawned := []string{}
	quitCalled := 0

	origSpawn := spawnSelf
	spawnSelf = func(exe string) error {
		spawned = append(spawned, exe)
		return spawnErr
	}
	t.Cleanup(func() { spawnSelf = origSpawn })

	origQuit := quitRuntime
	quitRuntime = func(context.Context) { quitCalled++ }
	t.Cleanup(func() { quitRuntime = origQuit })

	return &spawned, &quitCalled
}

// TestRestartAppSpawnsAndQuits 验证重启链路：spawnSelf 收到自身可执行路径，
// 退出标记落位（BeforeClose 放行，否则重启后进程退不出去）。
func TestRestartAppSpawnsAndQuits(t *testing.T) {
	app, _, _ := newTestApp(t)
	spawned, quitCalled := stubRestart(t, nil)

	if err := app.RestartApp(context.Background()); err != nil {
		t.Fatalf("RestartApp error: %v", err)
	}

	if len(*spawned) != 1 {
		t.Fatalf("spawnSelf 调用次数 = %d, 期望 1", len(*spawned))
	}
	wantExe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable error: %v", err)
	}
	if (*spawned)[0] != wantExe {
		t.Fatalf("spawnSelf exe = %q, 期望自身可执行文件 %q", (*spawned)[0], wantExe)
	}
	if *quitCalled != 1 {
		t.Fatalf("quitRuntime 调用次数 = %d, 期望 1", *quitCalled)
	}
	app.mu.Lock()
	quitting := app.quitting
	app.mu.Unlock()
	if !quitting {
		t.Fatal("重启应置位 quitting 退出标记")
	}
	if app.BeforeClose(context.Background()) {
		t.Fatal("RestartApp 后 BeforeClose 应放行退出（返回 false），而不是拦截成隐藏窗口")
	}
}

// TestRestartAppSpawnFailure 验证失败路径：spawnSelf 失败时 RestartApp 上抛错误，
// 不进入退出流程（quitting 不置位，避免「没启起来却被关掉」）。
func TestRestartAppSpawnFailure(t *testing.T) {
	app, _, _ := newTestApp(t)
	_, quitCalled := stubRestart(t, errors.New("启动失败"))

	if err := app.RestartApp(context.Background()); err == nil {
		t.Fatal("spawnSelf 失败时 RestartApp 应返回错误")
	}
	if *quitCalled != 0 {
		t.Fatal("spawnSelf 失败不应进入退出流程")
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.quitting {
		t.Fatal("spawnSelf 失败不应置位 quitting")
	}
}
