package update

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// ApplyOptions 控制下载、校验与替换调度。
type ApplyOptions struct {
	Tag             string
	ZipURL, SumsURL string
	DestExe         string
	CacheDir        string
	Get             GetFunc
	SpawnReplace    func(currentExe, newExe string) error
}

// errUpdatePlatformUnsupported 是 darwin 应用内更新被禁用的兜底（wire key，前端直译）。
// 对齐 errNoDownloadSource 约定：原样返回，不可再包 `err.update.*|%w`，否则嵌套 wire key 无法翻译。
var errUpdatePlatformUnsupported = errors.New("err.update.platform_unsupported")

// Apply 下载 zip、校验 SHA-256、解出桌面二进制到 dest+".new"，再调度退出后替换。
func Apply(ctx context.Context, opts ApplyOptions) error {
	// darwin 门控：替换会破坏 adhoc 签名（exec 即 SIGKILL），防御性兜底，
	// 正常路径已被 Check 的平台跳过挡住（见 check.go）。
	if currentGOOS == "darwin" {
		return errUpdatePlatformUnsupported
	}
	return applyExe(ctx, opts)
}

// applyExe 是 Apply 去掉平台门控后的下载-校验-替换主体；
// 抽出来便于单测绕过 darwin 门控继续覆盖提取管线（未来重签名方案重评时复用）。
func applyExe(ctx context.Context, opts ApplyOptions) error {
	if opts.DestExe == "" {
		return fmt.Errorf("err.update.dest_exe_empty")
	}
	zipName := CurrentPackageZip()
	binName := currentBinaryName()
	zipBody := cachedZipBody(opts)
	if zipBody == nil {
		cli := Client{Get: opts.Get, HTTP: &http.Client{Timeout: applyTimeout}}
		sumsBody, err := cli.getThroughSources(ctx, opts.SumsURL)
		if err != nil {
			if errors.Is(err, errNoDownloadSource) {
				return err
			}
			return fmt.Errorf("err.update.download_sums|%w", err)
		}
		want, err := ParseSHA256SUMS(sumsBody, zipName)
		if err != nil {
			return err
		}
		zipBody, err = cli.getThroughSources(ctx, opts.ZipURL)
		if err != nil {
			if errors.Is(err, errNoDownloadSource) {
				return err
			}
			return fmt.Errorf("err.update.download_package|%w", err)
		}
		sum := sha256.Sum256(zipBody)
		got := hex.EncodeToString(sum[:])
		if got != want {
			return fmt.Errorf("err.update.checksum_mismatch")
		}
		if opts.CacheDir != "" && opts.Tag != "" {
			// 缓存只是加速下次安装；写盘失败不应阻断本次升级。
			_ = storeCache(opts.CacheDir, opts.Tag, zipName, zipBody, got)
		}
	}
	exeBytes, err := extractPackageBinary(zipBody, binName)
	if err != nil {
		return err
	}
	next := opts.DestExe + ".new"
	if err := os.WriteFile(next, exeBytes, 0o755); err != nil {
		return fmt.Errorf("err.update.write_new_version|%w", err)
	}
	spawn := opts.SpawnReplace
	if spawn == nil {
		spawn = spawnReplaceDefault
	}
	if err := spawn(opts.DestExe, next); err != nil {
		return fmt.Errorf("err.update.schedule_replace|%w", err)
	}
	return nil
}

// extractPackageBinary 从发布 zip 取出桌面可执行文件字节。
// darwin 包为整包 .app，取 Contents/MacOS 下主二进制；其它平台取顶层 binName。
func extractPackageBinary(zipBody []byte, binName string) ([]byte, error) {
	if currentGOOS == "darwin" {
		return extractDarwinAppBinary(zipBody)
	}
	return extractNamed(zipBody, binName)
}

func extractNamed(zipBody []byte, base string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipBody), int64(len(zipBody)))
	if err != nil {
		return nil, fmt.Errorf("err.update.unzip_failed|%w", err)
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if !strings.EqualFold(filepath.Base(f.Name), base) {
			continue
		}
		b, err := readZipFile(f)
		if err != nil {
			return nil, err
		}
		if len(b) == 0 {
			return nil, fmt.Errorf("err.update.asset_empty|%s", base)
		}
		return b, nil
	}
	return nil, fmt.Errorf("err.update.asset_missing|%s", base)
}

// extractDarwinAppBinary 取 zip 内 *.app/Contents/MacOS/<exe>（忽略 Resources 等）。
func extractDarwinAppBinary(zipBody []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipBody), int64(len(zipBody)))
	if err != nil {
		return nil, fmt.Errorf("err.update.unzip_failed|%w", err)
	}
	const marker = "/Contents/MacOS/"
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := filepath.ToSlash(f.Name)
		i := strings.Index(name, marker)
		if i < 0 {
			continue
		}
		rest := name[i+len(marker):]
		if rest == "" || strings.Contains(rest, "/") {
			continue
		}
		b, err := readZipFile(f)
		if err != nil {
			return nil, err
		}
		if len(b) == 0 {
			return nil, fmt.Errorf("err.update.asset_empty|%s", rest)
		}
		return b, nil
	}
	return nil, fmt.Errorf("err.update.asset_missing|Contents/MacOS")
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(rc, 200<<20))
	_ = rc.Close()
	return b, err
}
