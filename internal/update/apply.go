package update

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// ApplyOptions 控制下载、校验与替换调度。
type ApplyOptions struct {
	ZipURL, SumsURL string
	DestExe         string
	Get             GetFunc
	SpawnReplace    func(currentExe, newExe string) error
}

// Apply 下载 zip、校验 SHA-256、解出桌面二进制到 dest+".new"，再调度退出后替换。
func Apply(ctx context.Context, opts ApplyOptions) error {
	if opts.DestExe == "" {
		return fmt.Errorf("目标可执行文件为空")
	}
	zipName := CurrentPackageZip()
	binName := currentBinaryName()
	cli := Client{Get: opts.Get, HTTP: &http.Client{Timeout: applyTimeout}}
	sumsBody, err := cli.getThroughSources(ctx, opts.SumsURL)
	if err != nil {
		return fmt.Errorf("下载校验文件: %w", err)
	}
	want, err := ParseSHA256SUMS(sumsBody, zipName)
	if err != nil {
		return err
	}
	zipBody, err := cli.getThroughSources(ctx, opts.ZipURL)
	if err != nil {
		return fmt.Errorf("下载安装包: %w", err)
	}
	sum := sha256.Sum256(zipBody)
	got := hex.EncodeToString(sum[:])
	if got != want {
		return fmt.Errorf("安装包校验失败")
	}
	exeBytes, err := extractNamed(zipBody, binName)
	if err != nil {
		return err
	}
	next := opts.DestExe + ".new"
	if err := os.WriteFile(next, exeBytes, 0o755); err != nil {
		return fmt.Errorf("写出新版本: %w", err)
	}
	spawn := opts.SpawnReplace
	if spawn == nil {
		spawn = spawnReplaceDefault
	}
	if err := spawn(opts.DestExe, next); err != nil {
		return fmt.Errorf("调度替换: %w", err)
	}
	return nil
}

func extractNamed(zipBody []byte, base string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipBody), int64(len(zipBody)))
	if err != nil {
		return nil, fmt.Errorf("解压安装包: %w", err)
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if !strings.EqualFold(filepath.Base(f.Name), base) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(rc, 200<<20))
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
		if len(b) == 0 {
			return nil, fmt.Errorf("%s 为空", base)
		}
		return b, nil
	}
	return nil, fmt.Errorf("安装包中没有 %s", base)
}
