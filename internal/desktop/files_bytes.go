package desktop

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yangk/kshell/internal/discovery"
)

// 图片/PDF 预览读取上限（20MiB）。
const maxPreviewBytes = 20 << 20

// FileBytes 是桌面端二进制预览载荷（base64 + mime）。
// AbsPath 为本地工作区文件的绝对路径（SSH 远程文件为空），
// 供 HTML 浏览页签计算 <base> 服务地址用。
type FileBytes struct {
	Base64  string `json:"Base64"`
	Mime    string `json:"Mime"`
	Size    int64  `json:"Size"`
	AbsPath string `json:"AbsPath"`
}

// ReadFileBytes 读取工作区内文件字节供图片/PDF 预览；走路径穿越校验，超限报错。
func (a *App) ReadFileBytes(wsPath, path string) (FileBytes, error) {
	kind, local, remote, err := a.parseWSRef(wsPath)
	if err != nil {
		return FileBytes{}, err
	}
	if kind == discovery.KindSSH {
		return a.readFileBytesRemote(remote, path)
	}
	resolved, err := a.resolveWorkspaceFile(local, path)
	if err != nil {
		return FileBytes{}, err
	}
	return readFileBytesLimited(resolved, maxPreviewBytes)
}

func readFileBytesLimited(abs string, max int64) (FileBytes, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return FileBytes{}, err
	}
	if info.IsDir() {
		return FileBytes{}, fmt.Errorf("err.files.dir_not_previewable")
	}
	if info.Size() > max {
		return FileBytes{}, fmt.Errorf("err.files.too_big_preview|%d|%d", info.Size(), max)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return FileBytes{}, err
	}
	return FileBytes{
		Base64:  base64.StdEncoding.EncodeToString(data),
		Mime:    mimeByExt(abs),
		Size:    info.Size(),
		AbsPath: abs,
	}, nil
}

func mimeByExt(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".bmp":
		return "image/bmp"
	case ".ico":
		return "image/x-icon"
	case ".pdf":
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}
