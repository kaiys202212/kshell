package desktop

import (
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/yangk/kshell/internal/discovery"
)

// HTML 浏览器页签的本地子资源服务通道。
//
// 顶层 HTML 文档由前端经 ReadFileBytes 取字节后以 blob URL 装进 sandbox iframe
// （不经过本通道，杜绝 Wails runtime 注入与应用页被嵌）；本通道只服务该文档的
// 相对资源（css/图片/字体等）。挂载方式：main.go 里 assetserver.Options.Handler，
// Wails 仅在内嵌资产未命中时回调（见 wails v2 assethandler.go），故以专用前缀隔离。
//
// 安全约束：
//   - 仅 GET；
//   - 路径必须为绝对路径、落在当前本地工作区根之内（大小写不敏感前缀 + 分隔符边界，
//     先 filepath.Clean 归一中和 .. 穿越）；
//   - 扩展名白名单（默认拒绝），html/htm/xhtml 一律拒绝——顶层文档不允许经此通道
//     当文档加载，iframe 内点相对链接只会得到 403（已知取舍，见设计文档）；
//   - 响应带 no-store；大小沿用预览读取上限。

// assetFilePrefix 是工作区文件服务通道的 URL 前缀，后接 base64url(绝对路径)。
const assetFilePrefix = "/__kshell-file/"

// assetMimeByExt 是子资源扩展名白名单与 Content-Type 映射（默认拒绝）。
var assetMimeByExt = map[string]string{
	".css":  "text/css; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".mjs":  "text/javascript; charset=utf-8",
	".json": "application/json; charset=utf-8",
	".map":  "application/json; charset=utf-8",
	".txt":  "text/plain; charset=utf-8",
	".md":   "text/plain; charset=utf-8",
	".csv":  "text/plain; charset=utf-8",
	".xml":  "text/xml; charset=utf-8",
	".webmanifest": "application/manifest+json",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
	".bmp":  "image/bmp",
	".ico":  "image/x-icon",
	".avif": "image/avif",
	".woff": "font/woff",
	".woff2": "font/woff2",
	".ttf":  "font/ttf",
	".otf":  "font/otf",
	".eot":  "application/vnd.ms-fontobject",
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".ogg":  "audio/ogg",
	".mp4":  "video/mp4",
	".webm": "video/webm",
}

// assetDeniedExt 是即便能映射到安全类型也拒绝的扩展名（文档类，防止被当页面加载）。
var assetDeniedExt = map[string]bool{
	".html":   true,
	".htm":    true,
	".xhtml":  true,
}

// WorkspaceFileHandler 返回挂进 Wails AssetServer 的受限文件服务。
func (a *App) WorkspaceFileHandler() http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		abs, ok := a.resolveAssetFile(req)
		if !ok {
			http.NotFound(rw, req)
			return
		}
		ext := strings.ToLower(filepath.Ext(abs))
		if assetDeniedExt[ext] {
			http.Error(rw, "forbidden", http.StatusForbidden)
			return
		}
		mime, ok := assetMimeByExt[ext]
		if !ok {
			http.Error(rw, "forbidden", http.StatusForbidden)
			return
		}
		data, err := readAssetLimited(abs, maxPreviewBytes)
		if err != nil {
			http.NotFound(rw, req)
			return
		}
		rw.Header().Set("Content-Type", mime)
		rw.Header().Set("Cache-Control", "no-store")
		rw.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = rw.Write(data)
	})
}

// resolveAssetFile 校验请求并解出允许读取的绝对路径；任何不符返回 false。
//
// URL 形态：前缀 + base64url(目录绝对路径) + "/" + 相对资源名。
// 相对名可为空（此时解出的是目录本身，后续 stat 会拒绝），该形态正好用 HTML
// <base href="…/__kshell-file/<enc>/"> 承载相对资源解析基点。
func (a *App) resolveAssetFile(req *http.Request) (string, bool) {
	if req.Method != http.MethodGet {
		return "", false
	}
	rest := strings.TrimPrefix(req.URL.Path, assetFilePrefix)
	if rest == req.URL.Path || rest == "" {
		return "", false
	}
	var dirEnc, rel string
	if i := strings.Index(rest, "/"); i >= 0 {
		dirEnc, rel = rest[:i], rest[i+1:]
	} else {
		dirEnc = rest
	}
	dir, err := base64.RawURLEncoding.DecodeString(dirEnc)
	if err != nil {
		return "", false
	}
	// 先 Clean 归一中和 .. 穿越，再做白名单边界校验
	abs := filepath.Clean(filepath.Join(string(dir), filepath.FromSlash(rel)))
	if !filepath.IsAbs(abs) {
		return "", false
	}
	if !a.underLocalWorkspaceRoot(abs) {
		return "", false
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return "", false
	}
	return abs, true
}

// underLocalWorkspaceRoot 判断 abs 是否落在任一本地工作区根之内。
func (a *App) underLocalWorkspaceRoot(abs string) bool {
	norm := discovery.NormalizePath(abs)
	for _, ws := range a.GetWorkspaces() {
		if ws.Kind == discovery.KindSSH {
			continue
		}
		root := discovery.NormalizePath(ws.Path)
		if root != "" && strings.HasPrefix(norm, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// readAssetLimited 读取子资源内容，超限拒绝（子资源不应有大到失控的文件）。
func readAssetLimited(abs string, max int64) ([]byte, error) {
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if info.Size() > max {
		return nil, os.ErrInvalid
	}
	return os.ReadFile(abs)
}
