package desktop

import (
	"errors"
	"net/url"
	"strings"
)

// openExternalURL 用系统默认程序打开外链；测试可替换。
var openExternalURL = openExternalURLOS

var errOpenURLInvalid = errors.New("err.open.url_invalid")

// 与前端 openHref 危险协议对齐；另拒 file（本地文件走 OpenInDefaultApp）。
var openURLDeniedSchemes = map[string]struct{}{
	"javascript": {},
	"data":       {},
	"vbscript":   {},
	"blob":       {},
	"file":       {},
}

// OpenExternalURL 用操作系统默认关联打开 http(s)/mailto/自定义协议等外链。
// 不走 Wails BrowserOpenURL：其校验过严（如 URL 含括号会静默失败），
// 且前端若拦截不完整时 WebView2 会弹 kshell 内置窗体。
func (a *App) OpenExternalURL(raw string) error {
	u := strings.TrimSpace(raw)
	if u == "" {
		return errOpenURLInvalid
	}
	parsed, err := url.Parse(u)
	if err != nil || parsed.Scheme == "" {
		return errOpenURLInvalid
	}
	scheme := strings.ToLower(parsed.Scheme)
	// 单字母 scheme 是 Windows 盘符误判（如 D:...），不当外链
	if len(scheme) == 1 {
		return errOpenURLInvalid
	}
	if _, denied := openURLDeniedSchemes[scheme]; denied {
		return errOpenURLInvalid
	}
	if (scheme == "http" || scheme == "https") && parsed.Host == "" {
		return errOpenURLInvalid
	}
	return openExternalURL(u)
}
