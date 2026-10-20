package weixin

import (
	"context"
	"crypto/aes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ecbEncrypt 用 AES-128-ECB + PKCS7 加密（测试对偶实现，与 DownloadMedia 的解密互为镜像）。
func ecbEncrypt(t *testing.T, key, plain []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("构造 cipher: %v", err)
	}
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append(append([]byte{}, plain...), make([]byte, pad)...)
	for i := len(plain); i < len(padded); i++ {
		padded[i] = byte(pad) // PKCS7：填充字节值 = 填充长度
	}
	out := make([]byte, len(padded))
	for i := 0; i < len(padded); i += aes.BlockSize {
		block.Encrypt(out[i:i+aes.BlockSize], padded[i:i+aes.BlockSize])
	}
	return out
}

func TestDownloadMediaFileHexKey(t *testing.T) {
	// 文件类 key：base64(hex 32 字符) → hex 解码 → 16 字节。
	raw := []byte("0123456789abcdef")
	keyB64 := base64.StdEncoding.EncodeToString([]byte(hex.EncodeToString(raw)))
	plain := []byte("PDF 内容明文，含中文")
	cipher := ecbEncrypt(t, raw, plain)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/download" {
			t.Errorf("路径 = %q, 期望 /download", r.URL.Path)
		}
		if r.URL.Query().Get("encrypted_query_param") != "eqp-1" {
			t.Errorf("download 参数 = %q", r.URL.Query().Get("encrypted_query_param"))
		}
		w.Write(cipher)
	}))
	defer srv.Close()

	c := NewClient("tok", "")
	c.MediaBase = srv.URL
	data, err := c.DownloadMedia(context.Background(), InboundAttachment{
		Kind: "file", Name: "需求.pdf", EncryptQueryParam: "eqp-1", AESKey: keyB64,
	})
	if err != nil {
		t.Fatalf("DownloadMedia: %v", err)
	}
	if string(data) != string(plain) {
		t.Errorf("解密结果 = %q, 期望 %q", data, plain)
	}
}

func TestDownloadMediaImageRawKey(t *testing.T) {
	// 图片类 key：base64(原始 16 字节)。
	raw := []byte("raw16key12345678")
	keyB64 := base64.StdEncoding.EncodeToString(raw)
	plain := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	cipher := ecbEncrypt(t, raw, plain)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Write(cipher)
	}))
	defer srv.Close()

	c := NewClient("tok", "")
	c.MediaBase = srv.URL
	data, err := c.DownloadMedia(context.Background(), InboundAttachment{
		Kind: "image", EncryptQueryParam: "eqp-2", AESKey: keyB64,
	})
	if err != nil {
		t.Fatalf("DownloadMedia: %v", err)
	}
	if string(data) != string(plain) {
		t.Errorf("解密结果错误: %q", data)
	}
}

func TestDownloadMediaErrors(t *testing.T) {
	raw := []byte("0123456789abcdef")
	keyB64 := base64.StdEncoding.EncodeToString([]byte(hex.EncodeToString(raw)))

	// key 解码失败。
	c := NewClient("tok", "")
	if _, err := c.DownloadMedia(context.Background(), InboundAttachment{Kind: "file", AESKey: "!!不是base64"}); err == nil {
		t.Error("非法 key 应报错")
	}
	// 图片 key 解码后长度不是 16 字节。
	badLen := base64.StdEncoding.EncodeToString([]byte("short"))
	if _, err := c.DownloadMedia(context.Background(), InboundAttachment{Kind: "image", AESKey: badLen}); err == nil {
		t.Error("图片 key 非 16 字节应报错")
	}
	// 密文长度不是块大小的整数倍。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("短密文"))
	}))
	defer srv.Close()
	c.MediaBase = srv.URL
	if _, err := c.DownloadMedia(context.Background(), InboundAttachment{Kind: "file", AESKey: keyB64}); err == nil {
		t.Error("非整块密文应报错")
	}
	// HTTP 失败。
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv2.Close()
	c.MediaBase = srv2.URL
	if _, err := c.DownloadMedia(context.Background(), InboundAttachment{Kind: "file", AESKey: keyB64}); err == nil {
		t.Error("HTTP 403 应报错")
	}
}

func TestDownloadMediaMD5Check(t *testing.T) {
	raw := []byte("0123456789abcdef")
	keyB64 := base64.StdEncoding.EncodeToString([]byte(hex.EncodeToString(raw)))
	plain := []byte("content")
	cipher := ecbEncrypt(t, raw, plain)
	sum := md5.Sum(plain)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(cipher)
	}))
	defer srv.Close()
	c := NewClient("tok", "")
	c.MediaBase = srv.URL

	// MD5 匹配：正常返回。
	data, err := c.DownloadMedia(context.Background(), InboundAttachment{
		Kind: "file", EncryptQueryParam: "e", AESKey: keyB64, MD5: hex.EncodeToString(sum[:]),
	})
	if err != nil {
		t.Fatalf("MD5 匹配不应报错: %v", err)
	}
	if string(data) != "content" {
		t.Errorf("内容错误: %q", data)
	}
	// MD5 不匹配：仍返回数据（宁可交也不丢），仅日志。
	data, err = c.DownloadMedia(context.Background(), InboundAttachment{
		Kind: "file", EncryptQueryParam: "e", AESKey: keyB64, MD5: "deadbeef",
	})
	if err != nil {
		t.Fatalf("MD5 不匹配应降级为日志而非错误: %v", err)
	}
	if string(data) != "content" {
		t.Errorf("内容错误: %q", data)
	}
}

func TestSaveAttachment(t *testing.T) {
	dir := t.TempDir()
	// 路径穿越与非法字符清洗。
	p1, err := saveAttachment(dir, `..\..\evil\name<>:"|?*.pdf`, []byte("a"))
	if err != nil {
		t.Fatalf("saveAttachment: %v", err)
	}
	if filepath.Dir(p1) != dir {
		t.Errorf("落盘越出目标目录: %q", p1)
	}
	base1 := filepath.Base(p1)
	if strings.ContainsAny(base1, `<>:"|?*`) {
		t.Errorf("文件名未清洗非法字符: %q", base1)
	}
	if !strings.Contains(base1, "name") {
		t.Errorf("文件名丢失基名: %q", base1)
	}
	if !strings.HasSuffix(base1, ".pdf") {
		t.Errorf("扩展名丢失: %q", base1)
	}
	if !strings.Contains(base1, "-") {
		t.Errorf("缺少时间戳前缀: %q", base1)
	}
	// 同秒重名不覆盖：加序号后缀。
	p2, err := saveAttachment(dir, filepath.Base(p1), []byte("b"))
	if err != nil {
		t.Fatalf("重名落盘: %v", err)
	}
	if p2 == p1 {
		t.Errorf("重名未去冲突: %q", p2)
	}
	// 内容校验。
	if data, err := os.ReadFile(p1); err != nil || string(data) != "a" {
		t.Errorf("内容错误: %q %v", data, err)
	}
}
