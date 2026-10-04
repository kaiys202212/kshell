package update

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
