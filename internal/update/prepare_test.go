package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPrepareWritesCacheAndMeta(t *testing.T) {
	zipName := CurrentPackageZip()
	zipBytes := mustZip(t, map[string][]byte{PackageBinaryName(runtime.GOOS): []byte("NEWBIN")})
	sum := sha256.Sum256(zipBytes)
	hexSum := hex.EncodeToString(sum[:])
	sums := []byte(hexSum + "  " + zipName + "\n")
	dir := t.TempDir()

	err := Prepare(context.Background(), PrepareOptions{
		Tag:      "v0.2.0",
		ZipURL:   "https://example/x/" + zipName,
		SumsURL:  "https://example/x/" + SumsName,
		CacheDir: dir,
		Get: func(_ context.Context, url string) ([]byte, error) {
			if strings.Contains(url, SumsName) {
				return sums, nil
			}
			return zipBytes, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, zipName))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(zipBytes) {
		t.Fatal("缓存 zip 内容不一致")
	}
	var meta cacheMeta
	raw, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Tag != "v0.2.0" || meta.Zip != zipName || meta.SHA256 != hexSum {
		t.Fatalf("meta = %+v", meta)
	}
	if p, ok := CachedZip(dir, "v0.2.0", zipName); !ok || p == "" {
		t.Fatal("准备完成后应命中缓存")
	}
}

func TestPrepareRejectsChecksumMismatch(t *testing.T) {
	zipName := CurrentPackageZip()
	zipBytes := mustZip(t, map[string][]byte{PackageBinaryName(runtime.GOOS): []byte("NEWBIN")})
	sums := []byte(strings.Repeat("0", 64) + "  " + zipName + "\n")
	dir := t.TempDir()

	err := Prepare(context.Background(), PrepareOptions{
		Tag:      "v0.2.0",
		ZipURL:   "https://example/x/" + zipName,
		SumsURL:  "https://example/x/" + SumsName,
		CacheDir: dir,
		Get: func(_ context.Context, url string) ([]byte, error) {
			if strings.Contains(url, SumsName) {
				return sums, nil
			}
			return zipBytes, nil
		},
	})
	if err == nil {
		t.Fatal("期望校验失败")
	}
	if _, ok := CachedZip(dir, "v0.2.0", zipName); ok {
		t.Fatal("校验失败不应留下可用缓存")
	}
}

func TestPrepareCleansOldCache(t *testing.T) {
	zipName := CurrentPackageZip()
	zipBytes := mustZip(t, map[string][]byte{PackageBinaryName(runtime.GOOS): []byte("NEWBIN")})
	sum := sha256.Sum256(zipBytes)
	sums := []byte(hex.EncodeToString(sum[:]) + "  " + zipName + "\n")
	dir := t.TempDir()
	stale := filepath.Join(dir, "kshell-desktop-other-arch.zip")
	if err := os.WriteFile(stale, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Prepare(context.Background(), PrepareOptions{
		Tag:      "v0.2.0",
		ZipURL:   "https://example/x/" + zipName,
		SumsURL:  "https://example/x/" + SumsName,
		CacheDir: dir,
		Get: func(_ context.Context, url string) ([]byte, error) {
			if strings.Contains(url, SumsName) {
				return sums, nil
			}
			return zipBytes, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("旧缓存应被清理")
	}
}

func TestCachedZipOnlyMatchesTag(t *testing.T) {
	zipName := CurrentPackageZip()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, zipName), []byte("ZIP"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("ZIP"))
	meta := cacheMeta{Tag: "v0.2.0", Zip: zipName, SHA256: hex.EncodeToString(sum[:])}
	raw, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, ok := CachedZip(dir, "v0.2.0", zipName); !ok {
		t.Fatal("同 tag 应命中")
	}
	if _, ok := CachedZip(dir, "v0.3.0", zipName); ok {
		t.Fatal("不同 tag 不应命中")
	}
}

func TestCachedZipRejectsCorruptedFile(t *testing.T) {
	zipName := CurrentPackageZip()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, zipName), []byte("CORRUPT"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := cacheMeta{Tag: "v0.2.0", Zip: zipName, SHA256: strings.Repeat("0", 64)}
	raw, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, ok := readCachedZip(dir, "v0.2.0", zipName); ok {
		t.Fatal("损坏的缓存不应命中")
	}
}
