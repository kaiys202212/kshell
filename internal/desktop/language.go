package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/yangk/kshell/internal/applang"
	"github.com/yangk/kshell/internal/config"
)

// LanguageInfo 是语言设置的绑定形态：Configured 为配置原值
// （en/zh-CN/system），Resolved 为解析后的实际语言（en/zh-CN）。
type LanguageInfo struct {
	Configured string `json:"configured"`
	Resolved   string `json:"resolved"`
}

// normalizeLanguage 把任意值收敛到 en/zh-CN/system 之一，口径与
// config.normalized 一致：非法值（含空串）一律回落 en。
// Get/Set 两侧同用，保证「读到什么就能写回什么」。
func normalizeLanguage(lang string) string {
	switch lang {
	case "en", "zh-CN", "system":
		return lang
	}
	return "en"
}

// GetLanguage 返回当前语言配置与解析结果。
func (a *App) GetLanguage() LanguageInfo {
	lang := normalizeLanguage(a.snapshot().Config.Language)
	return LanguageInfo{Configured: lang, Resolved: applang.Resolve(lang)}
}

// SetLanguage 校验并持久化语言，更新 applang 运行态，广播事件并刷新托盘文案。
// 先落盘成功再更新内存与运行态（与 appearance 的 setter 同口径）：
// Save 失败时不改现状，避免界面与磁盘不一致。
func (a *App) SetLanguage(lang string) error {
	lang = normalizeLanguage(lang)
	if err := a.saveConfig(func(cfg *config.Config) { cfg.Language = lang }); err != nil {
		return err
	}
	applang.Set(applang.Resolve(lang))
	a.emitLanguage()
	a.rebuildTray()
	return nil
}

// emitLanguage 把语言快照推给前端（language:changed）。
func (a *App) emitLanguage() {
	a.Emit("language:changed", a.GetLanguage())
}

// rebuildTray 刷新托盘菜单文案（语言切换后调用）。
// 就地 SetTitle 而非销毁重建：systray.Quit 走 sync.Once，托盘消息循环退出后
// 无法再起，销毁重建会永久失去「退出」能力；菜单项句柄由库保证可跨 goroutine 调用。
// 托盘未启动（句柄为空）时无操作。
func (a *App) rebuildTray() {
	a.mu.Lock()
	active := a.trayActive
	a.mu.Unlock()
	if !active {
		return
	}
	refreshTrayTextFn()
}

// LoadExternalLocales 读取 ~/.kshell/locales/*.json，返回 文件名 → 内容原文。
// 目录不存在（含未装配 Layout）返回空 map 且无错误；
// 非法 JSON、非 .json 后缀、子目录一律跳过；单个文件读失败也跳过不致命。
func (a *App) LoadExternalLocales() (map[string]string, error) {
	a.mu.Lock()
	dir := a.opts.Layout.Locales
	a.mu.Unlock()

	out := map[string]string{}
	if dir == "" {
		return out, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil || !json.Valid(b) {
			continue
		}
		out[e.Name()] = string(b)
	}
	return out, nil
}
