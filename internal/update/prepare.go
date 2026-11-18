package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const cacheMetaName = "meta.json"

// PrepareOptions 描述一次预下载：把当前平台发布包校验后写入 CacheDir。
type PrepareOptions struct {
	Tag      string
	ZipURL   string
	SumsURL  string
	CacheDir string
	Get      GetFunc
}

type cacheMeta struct {
	Tag    string `json:"tag"`
	Zip    string `json:"zip"`
	SHA256 string `json:"sha256"`
}

// Prepare 下载并校验当前平台发布包，校验通过后原子写入缓存并记录 meta。
func Prepare(ctx context.Context, opts PrepareOptions) error {
	dir := strings.TrimSpace(opts.CacheDir)
	if dir == "" {
		return fmt.Errorf("err.update.cache_dir_empty")
	}
	tag := strings.TrimSpace(opts.Tag)
	if tag == "" {
		return fmt.Errorf("err.update.tag_empty")
	}
	zipName := CurrentPackageZip()
	cli := Client{Get: opts.Get}
	sumsBody, err := cli.getThroughSources(ctx, opts.SumsURL)
	if err != nil {
		if err == errNoDownloadSource {
			return err
		}
		return fmt.Errorf("err.update.download_sums|%w", err)
	}
	want, err := ParseSHA256SUMS(sumsBody, zipName)
	if err != nil {
		return err
	}
	zipBody, err := cli.getThroughSources(ctx, opts.ZipURL)
	if err != nil {
		if err == errNoDownloadSource {
			return err
		}
		return fmt.Errorf("err.update.download_package|%w", err)
	}
	sum := sha256.Sum256(zipBody)
	got := hex.EncodeToString(sum[:])
	if got != want {
		return fmt.Errorf("err.update.checksum_mismatch")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("err.update.write_cache|%w", err)
	}
	if err := storeCache(dir, tag, zipName, zipBody, got); err != nil {
		return fmt.Errorf("err.update.write_cache|%w", err)
	}
	return nil
}

// storeCache 原子写入 zip 与 meta，并清理旧发布包。
func storeCache(dir, tag, zipName string, body []byte, sha string) error {
	if err := writeFileAtomic(filepath.Join(dir, zipName), body); err != nil {
		return err
	}
	meta := cacheMeta{Tag: tag, Zip: zipName, SHA256: sha}
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(dir, cacheMetaName), raw); err != nil {
		return err
	}
	cleanupCache(dir, zipName)
	return nil
}

// cachedZipBody 返回命中缓存的 zip 内容；未配置缓存或未命中返回 nil。
func cachedZipBody(opts ApplyOptions) []byte {
	if opts.CacheDir == "" || opts.Tag == "" {
		return nil
	}
	body, _, ok := readCachedZip(opts.CacheDir, opts.Tag, CurrentPackageZip())
	if !ok {
		return nil
	}
	return body
}

// CachedZip 返回缓存 zip 路径；meta 的 tag/文件名匹配且文件存在才算命中。
func CachedZip(dir, tag, zipName string) (string, bool) {
	if _, _, ok := readCachedZip(dir, tag, zipName); !ok {
		return "", false
	}
	return filepath.Join(dir, zipName), true
}

// IsCached 报告当前平台的指定版本发布包是否已预下载且校验通过。
func IsCached(dir, tag string) bool {
	if dir == "" || tag == "" {
		return false
	}
	_, ok := CachedZip(dir, tag, CurrentPackageZip())
	return ok
}

// CachedTag 返回缓存 meta 记录的版本 tag；无缓存或 meta 损坏返回空串。
func CachedTag(dir string) string {
	if dir == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(dir, cacheMetaName))
	if err != nil {
		return ""
	}
	var meta cacheMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return ""
	}
	return meta.Tag
}

// readCachedZip 读取缓存并重新校验 SHA-256；损坏按未命中处理。
func readCachedZip(dir, tag, zipName string) ([]byte, cacheMeta, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, cacheMetaName))
	if err != nil {
		return nil, cacheMeta{}, false
	}
	var meta cacheMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, cacheMeta{}, false
	}
	if meta.Tag != tag || meta.Zip != zipName || meta.SHA256 == "" {
		return nil, cacheMeta{}, false
	}
	body, err := os.ReadFile(filepath.Join(dir, zipName))
	if err != nil {
		return nil, cacheMeta{}, false
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != meta.SHA256 {
		return nil, cacheMeta{}, false
	}
	return body, meta, true
}

func writeFileAtomic(path string, body []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// cleanupCache 清掉除当前 zip 与 meta 外的旧发布包。
func cleanupCache(dir, keepZip string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() == keepZip || e.Name() == cacheMetaName {
			continue
		}
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
}
