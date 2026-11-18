package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
	zipBody, got, err := downloadPackage(ctx, opts.Get, tag, opts.ZipURL, opts.SumsURL, zipName)
	if err != nil {
		return err
	}
	// 哈希正确不等于可安装：先确认 zip 内确实有当前平台的入口二进制，
	// 否则会把「已就绪」的假象推给前端，安装永远失败且每次轮询重复提示。
	if _, err := extractPackageBinary(zipBody, currentBinaryName()); err != nil {
		return err
	}
	if err := storeCache(dir, tag, zipName, zipBody, got); err != nil {
		return fmt.Errorf("err.update.write_cache|%w", err)
	}
	return nil
}

// dropCache 删除缓存 zip 与 meta（尽力而为）；用于清理不可安装的坏包。
func dropCache(dir string) {
	if dir == "" {
		return
	}
	_ = os.Remove(filepath.Join(dir, CurrentPackageZip()))
	_ = os.Remove(filepath.Join(dir, cacheMetaName))
}

// downloadCandidate 是同版本的一种下载地址组合。
type downloadCandidate struct {
	zipURL, sumsURL string
}

// githubReleaseURL 按 tag 构造 GitHub Release 官方附件直链。
// 发布产物命名固定，不依赖先拉到 assets 列表。
func githubReleaseURL(tag, fileName string) string {
	return "https://github.com/" + Owner + "/" + Repo + "/releases/download/" + tag + "/" + fileName
}

// packageCandidates 列出同版本可尝试的下载地址：调用方给的、GitCode 附件、GitHub 官方。
// GitCode 同步偶发漏附件，不能因为主源缺 zip 就让用户永远升不了级。
func packageCandidates(tag, zipURL, sumsURL, zipName string) []downloadCandidate {
	var out []downloadCandidate
	add := func(zip, sums string) {
		if zip == "" {
			return
		}
		for _, c := range out {
			if c.zipURL == zip {
				return
			}
		}
		out = append(out, downloadCandidate{zipURL: zip, sumsURL: sums})
	}
	add(zipURL, sumsURL)
	if tag != "" {
		add(GitCodeAttachURL(tag, zipName), GitCodeAttachURL(tag, SumsName))
		add(githubReleaseURL(tag, zipName), githubReleaseURL(tag, SumsName))
	}
	return out
}

// downloadPackage 依次尝试候选地址，返回校验通过的 zip 内容与 sha256。
func downloadPackage(ctx context.Context, get GetFunc, tag, zipURL, sumsURL, zipName string) ([]byte, string, error) {
	// 大文件用 applyTimeout（3 分钟），避免慢镜像触发 check 的 12s 短超时。
	cli := Client{Get: get, HTTP: &http.Client{Timeout: applyTimeout}}
	candidates := packageCandidates(tag, zipURL, sumsURL, zipName)
	var last error
	stage := ""
	for _, cand := range candidates {
		sumsBody, err := cli.getThroughSources(ctx, cand.sumsURL)
		if err != nil {
			last, stage = err, "sums"
			continue
		}
		want, err := ParseSHA256SUMS(sumsBody, zipName)
		if err != nil {
			last, stage = err, "parse"
			continue
		}
		zipBody, err := cli.getThroughSources(ctx, cand.zipURL)
		if err != nil {
			last, stage = err, "zip"
			continue
		}
		sum := sha256.Sum256(zipBody)
		got := hex.EncodeToString(sum[:])
		if got != want {
			last, stage = fmt.Errorf("err.update.checksum_mismatch"), "checksum"
			continue
		}
		return zipBody, got, nil
	}
	if last == nil {
		return nil, "", errNoDownloadSource
	}
	// 保持既有 wire key 单层约定：哨兵与 sums/checksum 类 key 原样返回。
	if errors.Is(last, errNoDownloadSource) || stage == "parse" || stage == "checksum" {
		return nil, "", last
	}
	if stage == "sums" {
		return nil, "", fmt.Errorf("err.update.download_sums|%w", last)
	}
	return nil, "", fmt.Errorf("err.update.download_package|%w", last)
}

// storeCache 原子写入 zip 与 meta，并清理旧发布包。
func storeCache(dir, tag, zipName string, body []byte, sha string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
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
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
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
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || e.Name() == keepZip || e.Name() == cacheMetaName {
			continue
		}
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
}
