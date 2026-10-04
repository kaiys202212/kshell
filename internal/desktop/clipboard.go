package desktop

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"strings"
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
		return "", fmt.Errorf("保存剪贴板图片失败: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(pngBytes); err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("保存剪贴板图片失败: %w", err)
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
