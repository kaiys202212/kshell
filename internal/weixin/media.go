package weixin

import (
	"context"
	"crypto/aes"
	"crypto/md5" //nolint:gosec // 与服务端 file_item.md5 对账用的 MD5，非安全用途
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// mediaBaseDefault 是微信媒体 CDN 的默认基础地址（参考实现 openclaw-weixin；与
// ilinkai 主 API 不同域）。真机联调若发现变更，改这里。
const mediaBaseDefault = "https://novac2c.cdn.weixin.qq.com/c2c"

// timeoutDownloadMedia 是单次媒体下载的超时。
const timeoutDownloadMedia = 60 * time.Second

// maxMediaSize 是下载密文的读入上限（微信附件上限的宽松余量）：CDN 响应异常
// 或字段真机证伪时防无界读内存（长轮询 goroutine 内同步执行）。
const maxMediaSize = 256 << 20

// cdnDownloadParam 是 CDN 下载 URL 的查询参数名。真机未证实（同 getupdates
// 附件字段名，证伪时与 ilink.go 一并修）。
const cdnDownloadParam = "encrypted_query_param"

// DownloadMedia 下载并解密一个附件：GET CDN download（带 encrypt_query_param）
// → AES-128-ECB 解密 → PKCS7 去填充。MD5/明文长度有值时对账，不符只记日志仍
// 返回数据（宁可把文件交给 agent 也不因对账失败丢弃）。
func (c *Client) DownloadMedia(ctx context.Context, att InboundAttachment) ([]byte, error) {
	key, err := decodeAESKey(att.Kind, att.AESKey)
	if err != nil {
		return nil, fmt.Errorf("解码附件密钥: %w", err)
	}
	if att.EncryptQueryParam == "" {
		return nil, fmt.Errorf("附件 %q 缺少 CDN 下载参数", att.Name)
	}
	base := c.MediaBase
	if base == "" {
		base = mediaBaseDefault
	}
	dctx, cancel := context.WithTimeout(ctx, timeoutDownloadMedia)
	defer cancel()
	req, err := http.NewRequestWithContext(dctx, http.MethodGet,
		base+"/download?"+cdnDownloadParam+"="+urlQueryEscape(att.EncryptQueryParam), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载附件 %q: %w", att.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载附件 %q: HTTP %d", att.Name, resp.StatusCode)
	}
	cipher, err := io.ReadAll(io.LimitReader(resp.Body, maxMediaSize+1))
	if err != nil {
		return nil, fmt.Errorf("读取附件 %q: %w", att.Name, err)
	}
	if len(cipher) > maxMediaSize {
		return nil, fmt.Errorf("附件 %q 密文超过上限 %d 字节", att.Name, maxMediaSize)
	}
	plain, err := aesEcbDecrypt(key, cipher)
	if err != nil {
		return nil, fmt.Errorf("解密附件 %q: %w", att.Name, err)
	}
	if att.MD5 != "" {
		if sum := md5.Sum(plain); hex.EncodeToString(sum[:]) != strings.ToLower(att.MD5) {
			log.Printf("[weixin] 附件 %q MD5 不匹配（期望 %s），仍按解密结果投递", att.Name, att.MD5)
		}
	}
	if att.Size > 0 && att.Size != int64(len(plain)) {
		log.Printf("[weixin] 附件 %q 明文长度 %d 与服务端 len %d 不符，仍按解密结果投递", att.Name, len(plain), att.Size)
	}
	return plain, nil
}

// decodeAESKey 解码服务端下发的 base64 AES-128 密钥。参考实现证实两类媒体编码
// 不一致：图片 = base64(原始 16 字节)；文件/语音/视频 = base64(hex 字符串 32 字符)。
func decodeAESKey(kind, keyB64 string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return nil, fmt.Errorf("base64 解码失败: %w", err)
	}
	if kind == "image" {
		if len(raw) != 16 {
			return nil, fmt.Errorf("图片密钥解码后 %d 字节, 期望 16", len(raw))
		}
		return raw, nil
	}
	// file（及语音/视频，v1 未用）：hex 字符串再解一层。
	key, err := hex.DecodeString(string(raw))
	if err != nil {
		return nil, fmt.Errorf("hex 解码失败: %w", err)
	}
	if len(key) != 16 {
		return nil, fmt.Errorf("文件密钥解码后 %d 字节, 期望 16", len(key))
	}
	return key, nil
}

// aesEcbDecrypt 做 AES-128-ECB 逐块解密并去 PKCS7 填充（Go 标准库不提供 ECB，
// 逐块手写；密钥由服务端一次性下发，ECB 在此场景可接受）。
func aesEcbDecrypt(key, cipher []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(cipher) == 0 || len(cipher)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("密文长度 %d 不是 %d 的整数倍", len(cipher), aes.BlockSize)
	}
	plain := make([]byte, len(cipher))
	for i := 0; i < len(cipher); i += aes.BlockSize {
		block.Decrypt(plain[i:i+aes.BlockSize], cipher[i:i+aes.BlockSize])
	}
	return pkcs7Unpad(plain, aes.BlockSize)
}

// pkcs7Unpad 校验并去掉 PKCS7 填充。
func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("空明文")
	}
	pad := int(data[len(data)-1])
	if pad == 0 || pad > blockSize || pad > len(data) {
		return nil, fmt.Errorf("PKCS7 填充非法: %d", pad)
	}
	for _, b := range data[len(data)-pad:] {
		if int(b) != pad {
			return nil, fmt.Errorf("PKCS7 填充内容非法")
		}
	}
	return data[:len(data)-pad], nil
}

// saveAttachment 把附件明文落盘到 dir，返回绝对路径。文件名做安全清洗（取基名、
// 去非法字符），加时间戳前缀防重名；同秒重名再加序号，绝不覆盖已落盘文件。
func saveAttachment(dir, name string, data []byte) (string, error) {
	base := filepath.Base(name)
	var sb strings.Builder
	for _, r := range base {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 0x20 || r == 0x7f {
			sb.WriteByte('_')
			continue
		}
		sb.WriteRune(r)
	}
	clean := strings.Trim(sb.String(), ". ")
	if clean == "" {
		clean = "unnamed"
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	stem, ext := clean, ""
	if i := strings.LastIndex(clean, "."); i > 0 {
		stem, ext = clean[:i], clean[i:]
	}
	prefix := time.Now().Format("20060102-150405") + "-"
	for n := 0; ; n++ {
		name := prefix + stem + ext
		if n > 0 {
			name = prefix + stem + "-" + strconv.Itoa(n) + ext
		}
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			continue // 已存在：换序号重试，不覆盖
		} else if !os.IsNotExist(err) {
			return "", err
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return "", err
		}
		return path, nil
	}
}
