package asrlocal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// partSuffix 是下载半成品后缀：保留未完成的字节，下次请求用 Range 续传。
// 只按源重试不删 part——同一字节流换哪个镜像都一致（sha256 最终把关）。
const partSuffix = ".part"

// progressInterval 是进度回调的最小间隔（大文件逐块回调会刷爆日志/事件）。
const progressInterval = 500 * time.Millisecond

// fetchFile 按序从 urls 下载到 dst，校验 sha256：
//   - 已存在且哈希匹配直接返回（幂等）；
//   - 有半成品走 Range 续传，服务器不支持（200）则重写；
//   - 每个源完成传输后校验哈希，不符删半成品换下一个源；
//   - 全部失败返回聚合错误（含各源原因），并清理半成品。
//
// 返回实际成功的地址，便于日志定位用户镜像是否生效。
func fetchFile(ctx context.Context, client *http.Client, urls []string, dst, sha256Hex string, sizeHint int64,
	progress func(done, total int64), logf func(string)) (string, error) {
	if logf == nil {
		logf = func(string) {}
	}
	if ok, _ := fileHashEqual(dst, sha256Hex); ok {
		return "", nil
	}
	part := dst + partSuffix
	var failures []string
	for _, u := range urls {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		if err := downloadOne(ctx, client, u, part, sizeHint, progress); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", u, err))
			logf(fmt.Sprintf("下载失败，换下一个源：%s（%v）", u, err))
			continue
		}
		ok, err := fileHashEqual(part, sha256Hex)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: 校验读取失败: %v", u, err))
			_ = os.Remove(part)
			continue
		}
		if !ok {
			failures = append(failures, fmt.Sprintf("%s: SHA256 校验不匹配", u))
			logf("校验不匹配，丢弃该源文件：" + u)
			_ = os.Remove(part)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", err
		}
		if err := os.Rename(part, dst); err != nil {
			_ = os.Remove(part)
			return "", fmt.Errorf("移动下载文件: %w", err)
		}
		return u, nil
	}
	_ = os.Remove(part)
	return "", fmt.Errorf("全部下载源失败：%s", strings.Join(failures, "；"))
}

// downloadOne 从单个地址把内容写入 part（存在则续传）。
func downloadOne(ctx context.Context, client *http.Client, url, part string, sizeHint int64, progress func(int64, int64)) error {
	var offset int64
	if st, err := os.Stat(part); err == nil {
		offset = st.Size()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusPartialContent && offset > 0:
		// 续传：追加。
	case resp.StatusCode == http.StatusOK:
		// 服务器忽略 Range 或首次下载：从头写。
		offset = 0
	case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable && offset > 0:
		// 半成品已完整（上一轮校验前中断）：交给哈希校验裁决。
		return nil
	default:
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if offset == 0 {
		if err := f.Truncate(0); err != nil {
			f.Close()
			return err
		}
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		f.Close()
		return err
	}

	total := sizeHint
	if resp.ContentLength > 0 {
		total = offset + resp.ContentLength
	}
	done := offset
	lastEmit := time.Now()
	buf := make([]byte, 256<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return werr
			}
			done += int64(n)
			if progress != nil && time.Since(lastEmit) >= progressInterval {
				lastEmit = time.Now()
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if progress != nil {
		progress(done, total)
	}
	return nil
}

// fileHashEqual 判断 path 的 sha256 是否等于 want（空 want 视为不校验）。
func fileHashEqual(path, want string) (bool, error) {
	if want == "" {
		return false, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	return hex.EncodeToString(h.Sum(nil)) == want, nil
}