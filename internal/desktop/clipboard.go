package desktop

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// ClipboardPaste 是终端粘贴用的剪贴板快照：Path 优先（资源管理器文件或剪贴板位图落盘），否则 Text。
type ClipboardPaste struct {
	Text string `json:"Text"`
	Path string `json:"Path"`
}

type clipboardSnapshot struct {
	Text  string
	Files []string
	PNG   []byte
}

func formatClipboardPaste(text string, files []string, imagePath string) ClipboardPaste {
	if len(files) > 0 {
		if p := strings.TrimSpace(files[0]); p != "" {
			return ClipboardPaste{Path: p}
		}
	}
	if strings.TrimSpace(text) != "" {
		return ClipboardPaste{Text: text}
	}
	if p := strings.TrimSpace(imagePath); p != "" {
		return ClipboardPaste{Path: p}
	}
	return ClipboardPaste{}
}

// ReadClipboardPaste 读系统剪贴板，供内嵌终端 Ctrl+V 注入。
// WebView2/xterm 经常收不到浏览器 paste，更读不到位图；必须走原生剪贴板。
func (a *App) ReadClipboardPaste() (ClipboardPaste, error) {
	snap, err := readClipboardSnapshot()
	if err != nil {
		return ClipboardPaste{}, err
	}
	imagePath := ""
	if strings.TrimSpace(snap.Text) == "" && len(snap.Files) == 0 && len(snap.PNG) > 0 {
		imagePath, err = writePastePNG(snap.PNG)
		if err != nil {
			return ClipboardPaste{}, err
		}
	}
	return formatClipboardPaste(snap.Text, snap.Files, imagePath), nil
}

func writePastePNG(pngBytes []byte) (string, error) {
	f, err := os.CreateTemp("", "kshell-paste-*.png")
	if err != nil {
		return "", fmt.Errorf("err.clipboard.save_image_failed|%w", err)
	}
	defer f.Close()
	if _, err := f.Write(pngBytes); err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("err.clipboard.save_image_failed|%w", err)
	}
	return f.Name(), nil
}

func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteClipboardFiles 把文件/文件夹写入系统剪贴板（CF_HDROP），供文件树 Ctrl+C 与资源管理器互操作。
// paths 必须是已拼好的绝对路径（前端只传 relPath，由本层 filepath.Join 拼接）。
func (a *App) WriteClipboardFiles(root string, rels []string) error {
	paths := make([]string, 0, len(rels))
	for _, rel := range rels {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		paths = append(paths, filepath.Join(root, filepath.FromSlash(rel)))
	}
	if len(paths) == 0 {
		return fmt.Errorf("err.clipboard.write_empty")
	}
	return writeClipboardFiles(paths)
}

// dropfilesBuffer 构造 CF_HDROP 缓冲：20 字节 DROPFILES 头（pFiles=20、pt=0、fNC=0、fWide=1），
// 后接每个路径的 UTF-16LE 编码（\0 结尾），整体以双 \0 结束，资源管理器按此约定解析。
func dropfilesBuffer(paths []string) []byte {
	var u16 []uint16
	for _, p := range paths {
		u16 = append(u16, utf16.Encode([]rune(p))...)
		u16 = append(u16, 0)
	}
	u16 = append(u16, 0)
	buf := make([]byte, 20, 20+len(u16)*2)
	binary.LittleEndian.PutUint32(buf[0:4], 20)
	binary.LittleEndian.PutUint16(buf[18:20], 1)
	for _, v := range u16 {
		buf = binary.LittleEndian.AppendUint16(buf, v)
	}
	return buf
}
