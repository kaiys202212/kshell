package desktop

import (
	"context"
	"errors"
	"testing"

	"github.com/yangk/kshell/internal/update"
	"github.com/yangk/kshell/internal/version"
)

func TestGetAppVersion(t *testing.T) {
	app, _, _ := newTestApp(t)
	if got := app.GetAppVersion(); got != version.Current() {
		t.Fatalf("GetAppVersion = %q", got)
	}
}

func TestCheckForUpdateSkipsDev(t *testing.T) {
	app, _, events := newTestApp(t)
	orig := checkUpdateFn
	checkUpdateFn = func() (update.CheckResult, error) {
		return update.CheckResult{Current: "dev", Skipped: true, Reason: "开发构建不检查更新"}, nil
	}
	t.Cleanup(func() { checkUpdateFn = orig })

	info, err := app.CheckForUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if !info.Skipped || info.Available {
		t.Fatalf("%+v", info)
	}
	for _, e := range *events {
		if e == "update:available" {
			t.Fatal("跳过时不应发 update:available")
		}
	}
}

func TestCheckForUpdateEmitsWhenAvailable(t *testing.T) {
	app, _, events := newTestApp(t)
	orig := checkUpdateFn
	checkUpdateFn = func() (update.CheckResult, error) {
		return update.CheckResult{
			Current: "v0.1.0", Latest: "v0.2.0", Notes: "修复",
			Available: true, Source: "ghfast",
			ZipURL: "https://x/" + update.ZipName, SumsURL: "https://x/" + update.SumsName,
		}, nil
	}
	t.Cleanup(func() { checkUpdateFn = orig })

	info, err := app.CheckForUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if !info.Available || info.Latest != "v0.2.0" {
		t.Fatalf("%+v", info)
	}
	found := false
	for _, e := range *events {
		if e == "update:available" {
			found = true
		}
	}
	if !found {
		t.Fatalf("events = %v", *events)
	}
}

func TestApplyUpdateRequiresAvailable(t *testing.T) {
	app, _, _ := newTestApp(t)
	orig := checkUpdateFn
	checkUpdateFn = func() (update.CheckResult, error) {
		return update.CheckResult{Current: "v0.2.0", Latest: "v0.2.0", Available: false}, nil
	}
	t.Cleanup(func() { checkUpdateFn = orig })

	if err := app.ApplyUpdate(context.Background()); err == nil {
		t.Fatal("无更新应失败")
	}
}

func TestApplyUpdateQuitsAfterSpawn(t *testing.T) {
	app, _, _ := newTestApp(t)
	origC := checkUpdateFn
	checkUpdateFn = func() (update.CheckResult, error) {
		return update.CheckResult{Available: true, ZipURL: "z", SumsURL: "s"}, nil
	}
	t.Cleanup(func() { checkUpdateFn = origC })

	origA := applyUpdateFn
	applyUpdateFn = func(context.Context, string, update.CheckResult) error { return nil }
	t.Cleanup(func() { applyUpdateFn = origA })

	origQ := quitRuntime
	quitCalled := 0
	quitRuntime = func(context.Context) { quitCalled++ }
	t.Cleanup(func() { quitRuntime = origQ })

	if err := app.ApplyUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if quitCalled != 1 {
		t.Fatalf("quit = %d", quitCalled)
	}
}

func TestApplyUpdatePropagatesError(t *testing.T) {
	app, _, _ := newTestApp(t)
	origC := checkUpdateFn
	checkUpdateFn = func() (update.CheckResult, error) {
		return update.CheckResult{Available: true}, nil
	}
	t.Cleanup(func() { checkUpdateFn = origC })
	origA := applyUpdateFn
	applyUpdateFn = func(context.Context, string, update.CheckResult) error { return errors.New("校验失败") }
	t.Cleanup(func() { applyUpdateFn = origA })
	origQ := quitRuntime
	quitRuntime = func(context.Context) { t.Fatal("失败不应退出") }
	t.Cleanup(func() { quitRuntime = origQ })

	if err := app.ApplyUpdate(context.Background()); err == nil {
		t.Fatal("期望错误")
	}
}
