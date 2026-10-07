// Package applang 提供 Go 侧直接显示给用户的少量文案（托盘、原生对话框、
// 系统 toast、终端标题、markdown role 标题）的双语 catalog。大多数后端文案走
// 「返 key、前端翻译」，此处只覆盖不经前端的直显点；外部语言包不扩展本
// catalog，未知语言回落英文。
package applang

import (
	"os"
	"strings"
	"sync"
)

var (
	mu      sync.RWMutex
	current = "en"
)

// Set 设置当前语言（仅 en / zh-CN；其他值按 en 处理）。
func Set(lang string) {
	mu.Lock()
	defer mu.Unlock()
	if lang == "zh-CN" {
		current = lang
		return
	}
	current = "en"
}

// T 返回 key 的本地化文案；未知 key 原样返回，当前语言缺条目时回落英文。
func T(key string) string {
	mu.RLock()
	lang := current
	mu.RUnlock()
	if lang == "zh-CN" {
		if v, ok := zhCN[key]; ok {
			return v
		}
	}
	if v, ok := en[key]; ok {
		return v
	}
	return key
}

// Resolve 把配置语言解析为实际语言：非 system 原样返回；
// system 按 Windows 用户区域/POSIX LANG 判定，解析不出回落 en。
func Resolve(configured string) string {
	if configured != "system" {
		return configured
	}
	return resolveSystem()
}

func resolveSystem() string {
	if v := windowsUserLocale(); v != "" {
		if strings.HasPrefix(strings.ToLower(v), "zh") {
			return "zh-CN"
		}
		return "en"
	}
	for _, e := range []string{"LC_ALL", "LANG"} {
		if v := os.Getenv(e); v != "" && strings.HasPrefix(strings.ToLower(v), "zh") {
			return "zh-CN"
		}
	}
	return "en"
}
