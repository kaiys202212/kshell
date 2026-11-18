package desktop

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/update"
	"github.com/yangk/kshell/internal/version"
)

// TestApplyUpdateBindingSignature 锁定前端契约：api.ts 以 0 参调用 ApplyUpdate。
// Wails v2 绑定不会注入 context.Context，签名里带 ctx 会被计成一个入参，
// 运行时报 "received 0 arguments, expected 1"。
func TestApplyUpdateBindingSignature(t *testing.T) {
	m, ok := reflect.TypeOf(&App{}).MethodByName("ApplyUpdate")
	if !ok {
		t.Fatal("缺少 ApplyUpdate 绑定方法")
	}
	// reflect.Type.Method 的类型把 receiver 计作第一个入参，故为 1（仅 receiver）。
	if got := m.Type.NumIn(); got != 1 {
		t.Fatalf("ApplyUpdate 绑定入参（含 receiver）= %d，前端以 0 参调用", got)
	}
}

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
		return update.CheckResult{Current: "dev", Skipped: true, Reason: "update.reason.dev_build"}, nil
	}
	t.Cleanup(func() { checkUpdateFn = orig })

	info, err := app.CheckForUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if !info.Skipped || info.Available {
		t.Fatalf("%+v", info)
	}
	for _, e := range events.names() {
		if e == "update:available" {
			t.Fatal("跳过时不应发 update:available")
		}
	}
}

func TestCheckForUpdateDoesNotEmit(t *testing.T) {
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
	for _, e := range events.names() {
		if e == "update:available" {
			t.Fatal("手动/纯检查不应弹升级提示")
		}
	}
}

func TestCheckAndPrepareEmitsAfterDownload(t *testing.T) {
	app, _, events := newTestApp(t)
	origC := checkUpdateFn
	checkUpdateFn = func() (update.CheckResult, error) {
		return update.CheckResult{Current: "v0.1.0", Latest: "v0.2.0", Available: true}, nil
	}
	t.Cleanup(func() { checkUpdateFn = origC })

	prepared := 0
	origP := prepareUpdateFn
	prepareUpdateFn = func(_ context.Context, r update.CheckResult, dir string) error {
		prepared++
		seedUpdateCache(t, dir, r.Latest)
		return nil
	}
	t.Cleanup(func() { prepareUpdateFn = origP })

	app.checkAndPrepare()
	if prepared != 1 {
		t.Fatalf("prepare = %d", prepared)
	}
	found := false
	for _, e := range events.names() {
		if e == "update:available" {
			found = true
		}
	}
	if !found {
		t.Fatalf("下载成功后应发 update:available，events=%v", events.names())
	}
}

func TestCheckAndPrepareSilentWhenDownloadFails(t *testing.T) {
	app, _, events := newTestApp(t)
	origC := checkUpdateFn
	checkUpdateFn = func() (update.CheckResult, error) {
		return update.CheckResult{Current: "v0.1.0", Latest: "v0.2.0", Available: true}, nil
	}
	t.Cleanup(func() { checkUpdateFn = origC })

	origP := prepareUpdateFn
	prepareUpdateFn = func(context.Context, update.CheckResult, string) error {
		return errors.New("网络断了")
	}
	t.Cleanup(func() { prepareUpdateFn = origP })

	app.checkAndPrepare()
	for _, e := range events.names() {
		if e == "update:available" {
			t.Fatal("下载失败不应弹升级提示")
		}
	}
}

func TestPrepareUpdateRejectsWhenNoUpdate(t *testing.T) {
	app, _, _ := newTestApp(t)
	orig := checkUpdateFn
	checkUpdateFn = func() (update.CheckResult, error) {
		return update.CheckResult{Current: "v0.2.0", Latest: "v0.2.0", Available: false}, nil
	}
	t.Cleanup(func() { checkUpdateFn = orig })

	if err := app.PrepareUpdate(); err == nil {
		t.Fatal("无更新应失败")
	}
}

func TestPrepareUpdateDownloadsWhenAvailable(t *testing.T) {
	app, _, _ := newTestApp(t)
	origC := checkUpdateFn
	checkUpdateFn = func() (update.CheckResult, error) {
		return update.CheckResult{Available: true, Latest: "v0.2.0"}, nil
	}
	t.Cleanup(func() { checkUpdateFn = origC })

	prepared := 0
	origP := prepareUpdateFn
	prepareUpdateFn = func(context.Context, update.CheckResult, string) error {
		prepared++
		return nil
	}
	t.Cleanup(func() { prepareUpdateFn = origP })

	if err := app.PrepareUpdate(); err != nil {
		t.Fatal(err)
	}
	if prepared != 1 {
		t.Fatalf("prepare = %d", prepared)
	}
}

func TestApplyUpdateRequiresAvailable(t *testing.T) {
	app, _, _ := newTestApp(t)
	orig := checkUpdateFn
	checkUpdateFn = func() (update.CheckResult, error) {
		return update.CheckResult{Current: "v0.2.0", Latest: "v0.2.0", Available: false}, nil
	}
	t.Cleanup(func() { checkUpdateFn = orig })

	if err := app.ApplyUpdate(); err == nil {
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
	applyUpdateFn = func(context.Context, string, update.CheckResult, string) error { return nil }
	t.Cleanup(func() { applyUpdateFn = origA })

	origQ := quitRuntime
	quitCalled := 0
	quitRuntime = func(context.Context) { quitCalled++ }
	t.Cleanup(func() { quitRuntime = origQ })

	if err := app.ApplyUpdate(); err != nil {
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
	applyUpdateFn = func(context.Context, string, update.CheckResult, string) error { return errors.New("校验失败") }
	t.Cleanup(func() { applyUpdateFn = origA })
	origQ := quitRuntime
	quitRuntime = func(context.Context) { t.Fatal("失败不应退出") }
	t.Cleanup(func() { quitRuntime = origQ })

	if err := app.ApplyUpdate(); err == nil {
		t.Fatal("期望错误")
	}
}

func TestScheduleUpdateCheckRepeatsUntilCancel(t *testing.T) {
	app, _, _ := newTestApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	app.mu.Lock()
	app.ctx = ctx
	app.mu.Unlock()

	origD, origI := updateCheckDelay, updateCheckInterval
	updateCheckDelay = 0
	updateCheckInterval = 15 * time.Millisecond
	t.Cleanup(func() {
		updateCheckDelay, updateCheckInterval = origD, origI
	})

	var n atomic.Int32
	origC := checkUpdateFn
	checkUpdateFn = func() (update.CheckResult, error) {
		n.Add(1)
		return update.CheckResult{Skipped: true}, nil
	}
	t.Cleanup(func() { checkUpdateFn = origC })

	done := make(chan struct{})
	go func() {
		app.scheduleUpdateCheck()
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for n.Load() < 3 {
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("检查次数 = %d，期望至少 3 次", n.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ctx 取消后 scheduleUpdateCheck 未返回")
	}
}

func TestCheckForUpdateReadyFromCache(t *testing.T) {
	app, _, _ := newTestApp(t)

	zipName := update.CurrentPackageZip()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(update.PackageBinaryName(runtime.GOOS))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("BIN")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zipBytes := buf.Bytes()
	sum := sha256.Sum256(zipBytes)
	sums := []byte(hex.EncodeToString(sum[:]) + "  " + zipName + "\n")
	if err := update.Prepare(context.Background(), update.PrepareOptions{
		Tag:      "v0.2.0",
		ZipURL:   "https://x/" + zipName,
		SumsURL:  "https://x/" + update.SumsName,
		CacheDir: filepath.Join(app.opts.Home, ".kshell", "update"),
		Get: func(_ context.Context, url string) ([]byte, error) {
			if strings.Contains(url, update.SumsName) {
				return sums, nil
			}
			return zipBytes, nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	orig := checkUpdateFn
	checkUpdateFn = func() (update.CheckResult, error) {
		return update.CheckResult{Current: "v0.1.0", Latest: "v0.2.0", Available: true}, nil
	}
	t.Cleanup(func() { checkUpdateFn = orig })

	info, err := app.CheckForUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if !info.Ready {
		t.Fatal("缓存已就绪时 Ready 应为 true")
	}
}

func TestApplyUpdateUsesCacheWhenCheckFails(t *testing.T) {
	app, _, _ := newTestApp(t)

	zipName := update.CurrentPackageZip()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(update.PackageBinaryName(runtime.GOOS))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("BIN")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zipBytes := buf.Bytes()
	sum := sha256.Sum256(zipBytes)
	sums := []byte(hex.EncodeToString(sum[:]) + "  " + zipName + "\n")
	if err := update.Prepare(context.Background(), update.PrepareOptions{
		Tag:      "v0.2.0",
		ZipURL:   "https://x/" + zipName,
		SumsURL:  "https://x/" + update.SumsName,
		CacheDir: filepath.Join(app.opts.Home, ".kshell", "update"),
		Get: func(_ context.Context, url string) ([]byte, error) {
			if strings.Contains(url, update.SumsName) {
				return sums, nil
			}
			return zipBytes, nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	origC := checkUpdateFn
	checkUpdateFn = func() (update.CheckResult, error) {
		return update.CheckResult{}, errors.New("网络断了")
	}
	t.Cleanup(func() { checkUpdateFn = origC })

	var applied string
	origA := applyUpdateFn
	applyUpdateFn = func(_ context.Context, _ string, r update.CheckResult, _ string) error {
		applied = r.Latest
		return nil
	}
	t.Cleanup(func() { applyUpdateFn = origA })
	origQ := quitRuntime
	quitRuntime = func(context.Context) {}
	t.Cleanup(func() { quitRuntime = origQ })

	if err := app.ApplyUpdate(); err != nil {
		t.Fatal(err)
	}
	if applied != "v0.2.0" {
		t.Fatalf("applied tag = %q", applied)
	}
}

// seedUpdateCache 写入一份当前平台的可安装缓存包，供自动预下载成功后 Ready 判定。
func seedUpdateCache(t *testing.T, dir, tag string) {
	t.Helper()
	zipName := update.CurrentPackageZip()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(update.PackageBinaryName(runtime.GOOS))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("BIN")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zipBytes := buf.Bytes()
	sum := sha256.Sum256(zipBytes)
	sums := []byte(hex.EncodeToString(sum[:]) + "  " + zipName + "\n")
	if err := update.Prepare(context.Background(), update.PrepareOptions{
		Tag:      tag,
		ZipURL:   "https://x/" + zipName,
		SumsURL:  "https://x/" + update.SumsName,
		CacheDir: dir,
		Get: func(_ context.Context, url string) ([]byte, error) {
			if strings.Contains(url, update.SumsName) {
				return sums, nil
			}
			return zipBytes, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestApplyUpdatePrefersCachedPreparedTag(t *testing.T) {
	app, _, _ := newTestApp(t)
	seedUpdateCache(t, app.updateCacheDir(), "v0.2.0")

	origC := checkUpdateFn
	checkUpdateFn = func() (update.CheckResult, error) {
		return update.CheckResult{Current: "v0.1.0", Latest: "v0.3.0", Available: true}, nil
	}
	t.Cleanup(func() { checkUpdateFn = origC })

	var applied string
	origA := applyUpdateFn
	applyUpdateFn = func(_ context.Context, _ string, r update.CheckResult, _ string) error {
		applied = r.Latest
		return nil
	}
	t.Cleanup(func() { applyUpdateFn = origA })
	origQ := quitRuntime
	quitRuntime = func(context.Context) {}
	t.Cleanup(func() { quitRuntime = origQ })

	if err := app.ApplyUpdate(); err != nil {
		t.Fatal(err)
	}
	if applied != "v0.2.0" {
		t.Fatalf("应安装弹窗对应的已就绪版本 v0.2.0，实际 %q", applied)
	}
}
