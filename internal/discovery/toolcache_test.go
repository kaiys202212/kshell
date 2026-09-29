package discovery

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/yangk/kshell/internal/providers"
)

// fakeBinPath 返回 writeFakeBin 实际写出的可执行文件路径（Windows 上带 .cmd 后缀）。
func fakeBinPath(dir, name string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(dir, name+".cmd")
	}
	return filepath.Join(dir, name)
}

func TestDetectAllCachedSkipsProbeWhenBinUnchanged(t *testing.T) {
	binDir := t.TempDir()
	writeFakeBin(t, binDir, "claude")
	t.Setenv("PATH", binDir)

	ps := []providers.Provider{stubProvider{id: "claude", spec: providers.DetectSpec{BinName: "claude"}}}
	cachePath := filepath.Join(t.TempDir(), "tools.json")

	calls := 0
	probe := func(string) string { calls++; return "claude 1.2.3" }

	first := detectAllCached(t.TempDir(), ps, cachePath, probe)
	if calls != 1 {
		t.Fatalf("首次应探测 1 次, got %d", calls)
	}
	if first[0].Version != "claude 1.2.3" {
		t.Fatalf("version = %q", first[0].Version)
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("应写出工具版本缓存: %v", err)
	}

	// 可执行文件指纹未变：第二次不再拉子进程，直接复用缓存版本
	second := detectAllCached(t.TempDir(), ps, cachePath, probe)
	if calls != 1 {
		t.Fatalf("指纹未变不应重新探测, got %d 次", calls)
	}
	if second[0].Version != "claude 1.2.3" {
		t.Fatalf("应复用缓存版本, got %q", second[0].Version)
	}
}

func TestDetectAllCachedReprobesWhenBinChanged(t *testing.T) {
	binDir := t.TempDir()
	writeFakeBin(t, binDir, "claude")
	t.Setenv("PATH", binDir)

	ps := []providers.Provider{stubProvider{id: "claude", spec: providers.DetectSpec{BinName: "claude"}}}
	cachePath := filepath.Join(t.TempDir(), "tools.json")

	if got := detectAllCached(t.TempDir(), ps, cachePath, func(string) string { return "v1" }); got[0].Version != "v1" {
		t.Fatalf("首次版本 = %q", got[0].Version)
	}

	// 模拟 CLI 升级：改写文件内容并推进 mtime，指纹变化后必须重新探测
	bin := fakeBinPath(binDir, "claude")
	if err := os.WriteFile(bin, []byte("@echo off\r\necho claude 2.0.0\r\n"), 0o755); err != nil {
		t.Fatalf("rewrite bin: %v", err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(bin, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	calls := 0
	got := detectAllCached(t.TempDir(), ps, cachePath, func(string) string { calls++; return "v2" })
	if calls != 1 || got[0].Version != "v2" {
		t.Fatalf("文件指纹变化后应重新探测: calls=%d version=%q", calls, got[0].Version)
	}
}

func TestDetectAllCachedDoesNotCacheUnknown(t *testing.T) {
	binDir := t.TempDir()
	writeFakeBin(t, binDir, "claude")
	t.Setenv("PATH", binDir)

	ps := []providers.Provider{stubProvider{id: "claude", spec: providers.DetectSpec{BinName: "claude"}}}
	cachePath := filepath.Join(t.TempDir(), "tools.json")

	// 探测失败（unknown）：不落缓存，下次启动重试
	calls := 0
	probe := func(string) string { calls++; return "unknown" }
	detectAllCached(t.TempDir(), ps, cachePath, probe)
	detectAllCached(t.TempDir(), ps, cachePath, probe)
	if calls != 2 {
		t.Fatalf("unknown 不应被缓存, probe 次数 = %d, want 2", calls)
	}
}

func TestDetectAllCachedWithoutCachePath(t *testing.T) {
	binDir := t.TempDir()
	writeFakeBin(t, binDir, "claude")
	t.Setenv("PATH", binDir)

	ps := []providers.Provider{stubProvider{id: "claude", spec: providers.DetectSpec{BinName: "claude"}}}
	calls := 0
	got := detectAllCached(t.TempDir(), ps, "", func(string) string { calls++; return "v1" })
	if calls != 1 || got[0].Version != "v1" {
		t.Fatalf("cachePath 为空应退化为实探: calls=%d got=%+v", calls, got)
	}
}

func TestLoadToolCacheToleratesCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tools.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := loadToolCache(path); len(got) != 0 {
		t.Fatalf("损坏缓存应视为空, got %+v", got)
	}
	if got := loadToolCache(filepath.Join(t.TempDir(), "missing.json")); len(got) != 0 {
		t.Fatalf("缺失缓存应视为空, got %+v", got)
	}
}
