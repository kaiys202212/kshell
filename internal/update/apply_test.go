package update

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyExtractsUnixBinary(t *testing.T) {
	prevOS, prevArch := currentGOOS, currentGOARCH
	currentGOOS, currentGOARCH = "linux", "amd64"
	t.Cleanup(func() { currentGOOS, currentGOARCH = prevOS, prevArch })

	dir := t.TempDir()
	dest := filepath.Join(dir, "kshell-desktop")
	if err := os.WriteFile(dest, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	zipName := PackageZipName("linux", "amd64")
	zipBytes := mustZip(t, map[string][]byte{"kshell-desktop": []byte("NEWBIN")})
	sum := sha256.Sum256(zipBytes)
	hexSum := hex.EncodeToString(sum[:])
	sums := []byte(hexSum + "  " + zipName + "\n")

	err := Apply(context.Background(), ApplyOptions{
		ZipURL:  "https://example/x/" + zipName,
		SumsURL: "https://example/x/" + SumsName,
		DestExe: dest,
		Get: func(_ context.Context, url string) ([]byte, error) {
			if strings.Contains(url, SumsName) {
				return sums, nil
			}
			return zipBytes, nil
		},
		SpawnReplace: func(string, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest + ".new")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "NEWBIN" {
		t.Fatalf("new exe = %q", got)
	}
}

func TestApplyExtractsDarwinAppBinary(t *testing.T) {
	prevOS, prevArch := currentGOOS, currentGOARCH
	currentGOOS, currentGOARCH = "darwin", "arm64"
	t.Cleanup(func() { currentGOOS, currentGOARCH = prevOS, prevArch })

	dir := t.TempDir()
	dest := filepath.Join(dir, "kshell")
	if err := os.WriteFile(dest, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	zipName := PackageZipName("darwin", "arm64")
	zipBytes := mustZip(t, map[string][]byte{
		"kshell-desktop.app/Contents/Info.plist":          []byte("<plist/>"),
		"kshell-desktop.app/Contents/MacOS/kshell":        []byte("NEWBIN"),
		"kshell-desktop.app/Contents/Resources/icon.icns": []byte("ICNS"),
	})
	sum := sha256.Sum256(zipBytes)
	hexSum := hex.EncodeToString(sum[:])
	sums := []byte(hexSum + "  " + zipName + "\n")

	err := applyExe(context.Background(), ApplyOptions{
		ZipURL:  "https://example/x/" + zipName,
		SumsURL: "https://example/x/" + SumsName,
		DestExe: dest,
		Get: func(_ context.Context, url string) ([]byte, error) {
			if strings.Contains(url, SumsName) {
				return sums, nil
			}
			return zipBytes, nil
		},
		SpawnReplace: func(string, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest + ".new")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "NEWBIN" {
		t.Fatalf("new exe = %q", got)
	}
}

func pinWindowsPackage(t *testing.T) {
	t.Helper()
	prevOS, prevArch := currentGOOS, currentGOARCH
	currentGOOS, currentGOARCH = "windows", "amd64"
	t.Cleanup(func() { currentGOOS, currentGOARCH = prevOS, prevArch })
}

func TestApplyWritesNewAndSchedulesReplace(t *testing.T) {
	pinWindowsPackage(t)
	dir := t.TempDir()
	dest := filepath.Join(dir, "kshell-desktop.exe")
	if err := os.WriteFile(dest, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	zipBytes := mustZip(t, map[string][]byte{"kshell-desktop.exe": []byte("NEWBIN")})
	sum := sha256.Sum256(zipBytes)
	hexSum := hex.EncodeToString(sum[:])
	sums := []byte(hexSum + "  " + ZipName + "\n")

	var spawned [][2]string
	err := Apply(context.Background(), ApplyOptions{
		ZipURL:  "https://example/x/" + ZipName,
		SumsURL: "https://example/x/" + SumsName,
		DestExe: dest,
		Get: func(_ context.Context, url string) ([]byte, error) {
			if strings.Contains(url, SumsName) {
				return sums, nil
			}
			return zipBytes, nil
		},
		SpawnReplace: func(current, next string) error {
			spawned = append(spawned, [2]string{current, next})
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	next := dest + ".new"
	got, err := os.ReadFile(next)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "NEWBIN" {
		t.Fatalf("new exe = %q", got)
	}
	if len(spawned) != 1 || spawned[0][0] != dest || spawned[0][1] != next {
		t.Fatalf("spawn = %#v", spawned)
	}
}

func TestApplyDownloadFallsBackToLaterProxy(t *testing.T) {
	pinWindowsPackage(t)
	dir := t.TempDir()
	dest := filepath.Join(dir, "kshell-desktop.exe")
	if err := os.WriteFile(dest, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	zipBytes := mustZip(t, map[string][]byte{"kshell-desktop.exe": []byte("NEWBIN")})
	sum := sha256.Sum256(zipBytes)
	hexSum := hex.EncodeToString(sum[:])
	sums := []byte(hexSum + "  " + ZipName + "\n")
	officialZip := "https://github.com/kaiys202212/kshell/releases/download/v0.2.0/" + ZipName
	officialSums := "https://github.com/kaiys202212/kshell/releases/download/v0.2.0/" + SumsName

	err := Apply(context.Background(), ApplyOptions{
		ZipURL:  officialZip,
		SumsURL: officialSums,
		DestExe: dest,
		Get: func(_ context.Context, url string) ([]byte, error) {
			if strings.Contains(url, "ghfast.top") {
				return nil, errors.New("代理超时")
			}
			if strings.Contains(url, SumsName) {
				return sums, nil
			}
			if strings.Contains(url, ZipName) {
				return zipBytes, nil
			}
			return nil, errors.New(url)
		},
		SpawnReplace: func(string, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestApplyDownloadsGitCodeURLDirectly(t *testing.T) {
	pinWindowsPackage(t)
	dir := t.TempDir()
	dest := filepath.Join(dir, "kshell-desktop.exe")
	if err := os.WriteFile(dest, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	zipBytes := mustZip(t, map[string][]byte{"kshell-desktop.exe": []byte("NEWBIN")})
	sum := sha256.Sum256(zipBytes)
	hexSum := hex.EncodeToString(sum[:])
	sums := []byte(hexSum + "  " + ZipName + "\n")
	gcZip := GitCodeAttachURL("v0.2.0", ZipName)
	gcSums := GitCodeAttachURL("v0.2.0", SumsName)
	err := Apply(context.Background(), ApplyOptions{
		ZipURL:  gcZip,
		SumsURL: gcSums,
		DestExe: dest,
		Get: func(_ context.Context, url string) ([]byte, error) {
			if strings.Contains(url, "ghfast") || strings.Contains(url, "github.com") {
				t.Fatalf("GitCode 直链不应再套代理: %s", url)
			}
			if url == gcSums {
				return sums, nil
			}
			if url == gcZip {
				return zipBytes, nil
			}
			return nil, errors.New(url)
		},
		SpawnReplace: func(string, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestApplyRejectsBadHash(t *testing.T) {
	pinWindowsPackage(t)
	dir := t.TempDir()
	dest := filepath.Join(dir, "kshell-desktop.exe")
	if err := os.WriteFile(dest, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	zipBytes := mustZip(t, map[string][]byte{"kshell-desktop.exe": []byte("NEWBIN")})
	err := Apply(context.Background(), ApplyOptions{
		ZipURL:  "https://example/" + ZipName,
		SumsURL: "https://example/" + SumsName,
		DestExe: dest,
		Get: func(_ context.Context, url string) ([]byte, error) {
			if strings.Contains(url, SumsName) {
				return []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  " + ZipName + "\n"), nil
			}
			return zipBytes, nil
		},
		SpawnReplace: func(string, string) error {
			t.Fatal("哈希失败不应替换")
			return nil
		},
	})
	if err == nil {
		t.Fatal("期望哈希失败")
	}
}

func TestNoDownloadSourceIsWireKey(t *testing.T) {
	// apply.go 依赖该哨兵是单层 wire key 并原样返回，嵌套会被 translateBackend 拆坏
	if got := errNoDownloadSource.Error(); got != "err.update.download_source_unavailable" {
		t.Fatalf("errNoDownloadSource = %q", got)
	}
}

func mustZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestApplyRefusesDarwin(t *testing.T) {
	prevOS, prevArch := currentGOOS, currentGOARCH
	currentGOOS, currentGOARCH = "darwin", "arm64"
	t.Cleanup(func() { currentGOOS, currentGOARCH = prevOS, prevArch })

	called := false
	err := Apply(context.Background(), ApplyOptions{
		ZipURL:  "https://example.com/kshell-desktop-darwin-arm64.zip",
		SumsURL: "https://example.com/SHA256SUMS",
		DestExe: "/Applications/kshell-desktop.app/Contents/MacOS/kshell",
		Get: func(context.Context, string) ([]byte, error) {
			called = true
			return nil, errors.New("darwin 门控不应发起下载")
		},
	})
	if err == nil {
		t.Fatal("darwin 上 Apply 应直接拒绝")
	}
	if called {
		t.Fatal("darwin 门控不应发起任何下载请求")
	}
	if err.Error() != "err.update.platform_unsupported" {
		t.Fatalf("应原样返回单层 wire key, got %q", err.Error())
	}
}

func TestApplyUsesCachedZipWithoutDownload(t *testing.T) {
	pinWindowsPackage(t)
	dir := t.TempDir()
	cacheDir := t.TempDir()
	dest := filepath.Join(dir, "kshell-desktop.exe")
	if err := os.WriteFile(dest, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	zipBytes := mustZip(t, map[string][]byte{"kshell-desktop.exe": []byte("NEWBIN")})
	if err := writeCacheForTest(cacheDir, "v0.2.0", zipBytes); err != nil {
		t.Fatal(err)
	}

	err := Apply(context.Background(), ApplyOptions{
		Tag:      "v0.2.0",
		ZipURL:   "https://example/" + ZipName,
		SumsURL:  "https://example/" + SumsName,
		DestExe:  dest,
		CacheDir: cacheDir,
		Get: func(context.Context, string) ([]byte, error) {
			t.Fatal("缓存命中时不应下载")
			return nil, nil
		},
		SpawnReplace: func(string, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest + ".new")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "NEWBIN" {
		t.Fatalf("new exe = %q", got)
	}
}

func TestApplyRedownloadsWhenCacheCorrupt(t *testing.T) {
	pinWindowsPackage(t)
	dir := t.TempDir()
	cacheDir := t.TempDir()
	dest := filepath.Join(dir, "kshell-desktop.exe")
	if err := os.WriteFile(dest, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	zipBytes := mustZip(t, map[string][]byte{"kshell-desktop.exe": []byte("NEWBIN")})
	if err := writeCacheForTest(cacheDir, "v0.2.0", zipBytes); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, ZipName), []byte("BROKEN"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(zipBytes)
	sums := []byte(hex.EncodeToString(sum[:]) + "  " + ZipName + "\n")

	downloads := 0
	err := Apply(context.Background(), ApplyOptions{
		Tag:      "v0.2.0",
		ZipURL:   "https://example/" + ZipName,
		SumsURL:  "https://example/" + SumsName,
		DestExe:  dest,
		CacheDir: cacheDir,
		Get: func(_ context.Context, url string) ([]byte, error) {
			if strings.Contains(url, SumsName) {
				return sums, nil
			}
			downloads++
			return zipBytes, nil
		},
		SpawnReplace: func(string, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if downloads != 1 {
		t.Fatalf("应重新下载一次，got %d", downloads)
	}
}

func writeCacheForTest(cacheDir, tag string, zipBytes []byte) error {
	zipName := CurrentPackageZip()
	if err := os.WriteFile(filepath.Join(cacheDir, zipName), zipBytes, 0o644); err != nil {
		return err
	}
	sum := sha256.Sum256(zipBytes)
	meta := cacheMeta{Tag: tag, Zip: zipName, SHA256: hex.EncodeToString(sum[:])}
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(cacheDir, "meta.json"), raw, 0o644)
}
