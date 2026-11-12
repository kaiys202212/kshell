package desktop

import (
	"encoding/binary"
	"strings"
	"testing"
)

// TestDropfilesBuffer 构造 CF_HDROP 缓冲：20 字节 DROPFILES 头 + UTF-16 路径逐个 \0 + 双 \0 结尾。
func TestDropfilesBuffer(t *testing.T) {
	buf := dropfilesBuffer([]string{`D:\a\b.txt`, `D:\c`})

	// 头部：pFiles=20（路径数据从偏移 20 开始），fWide=1（UTF-16）
	if got := binary.LittleEndian.Uint32(buf[0:4]); got != 20 {
		t.Fatalf("pFiles = %d, want 20", got)
	}
	if got := binary.LittleEndian.Uint16(buf[18:20]); got != 1 {
		t.Fatalf("fWide = %d, want 1", got)
	}
	// pt 与 fNC 保持 0
	if !allZero(buf[4:18]) {
		t.Fatalf("pt/fNC 字段应为 0")
	}

	// 路径段：以 UTF-16LE 解出两个 \0 结尾路径 + 末尾双 \0
	var chars []uint16
	for i := 20; i+1 < len(buf); i += 2 {
		chars = append(chars, binary.LittleEndian.Uint16(buf[i:i+2]))
	}
	rest := stringFromUtf16(chars)
	want := "D:\\a\\b.txt\x00D:\\c\x00\x00"
	if rest != want {
		t.Fatalf("路径段 = %q, want %q", rest, want)
	}
	if len(buf) != 20+len(want)*2 {
		t.Fatalf("缓冲长度 = %d, want %d", len(buf), 20+len(want)*2)
	}
}

func TestDropfilesBuffer_Empty(t *testing.T) {
	buf := dropfilesBuffer(nil)
	if len(buf) != 22 {
		t.Fatalf("空列表缓冲长度 = %d, want 22", len(buf))
	}
	if got := binary.LittleEndian.Uint32(buf[0:4]); got != 20 {
		t.Fatalf("pFiles = %d, want 20", got)
	}
}

func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

func stringFromUtf16(chars []uint16) string {
	var sb strings.Builder
	for _, c := range chars {
		sb.WriteRune(rune(c))
	}
	return sb.String()
}
