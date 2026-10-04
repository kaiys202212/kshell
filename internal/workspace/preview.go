package workspace

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// Preview 是右栏要展示的文件预览；二进制文件不输出内容，只给元信息。
type Preview struct {
	Lines     []string
	Text      string // 无行号原文（\n 归一）；Binary 时为空，供桌面端渲染
	Truncated bool
	Binary    bool
	Info      string
	Err       error
}

const previewHeadBytes = 512 * 1024

// PreviewFile 只读前 maxBytes 字节，避免把大文件整读进内存。
func PreviewFile(path string, maxBytes int, maxLines int) Preview {
	if maxBytes <= 0 {
		maxBytes = previewHeadBytes
	}
	if maxLines <= 0 {
		maxLines = 500
	}

	info, err := os.Stat(path)
	if err != nil {
		return Preview{Err: err}
	}
	if info.IsDir() {
		return Preview{Info: fmt.Sprintf("目录 · %d 项", dirEntries(path))}
	}

	f, err := os.Open(path)
	if err != nil {
		return Preview{Err: err}
	}
	defer f.Close()

	buf := make([]byte, maxBytes)
	n, err := io.ReadFull(f, buf)
	buf = buf[:n]
	truncated := n == maxBytes && err == nil

	if isBinary(buf) {
		return Preview{
			Binary: true,
			Info:   fmt.Sprintf("二进制文件 · %d 字节 · %s", info.Size(), info.ModTime().Format("2006-01-02 15:04")),
		}
	}

	text := strings.ReplaceAll(string(buf), "\r\n", "\n")
	if !utf8.ValidString(text) {
		text = strings.ToValidUTF8(text, "�")
	}

	lines := strings.Split(text, "\n")
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		truncated = true
	}

	out := make([]string, 0, len(lines))
	for i, line := range lines {
		out = append(out, fmt.Sprintf("%4d │ %s", i+1, line))
	}

	return Preview{
		Lines:     out,
		Text:      text,
		Truncated: truncated,
		Info:      fmt.Sprintf("%d 字节 · %s", info.Size(), info.ModTime().Format("2006-01-02 15:04")),
	}
}

func isBinary(buf []byte) bool {
	if len(buf) == 0 {
		return false
	}
	// 前 8KB 内出现 NUL 基本可以断定是二进制
	limit := len(buf)
	if limit > 8000 {
		limit = 8000
	}
	return bytes.IndexByte(buf[:limit], 0) >= 0
}

func dirEntries(path string) int {
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0
	}
	return len(entries)
}
