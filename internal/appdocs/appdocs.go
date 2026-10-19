// Package appdocs 内嵌应用级文档（帮助使用说明与升级日志）。
// 文档随二进制一起发布，桌面端设置页与升级弹窗直接读取，避免再从磁盘找文件；
// 文案变更只改 docs/ 下的 markdown，不需动 Go 逻辑。
package appdocs

import (
	"embed"
	"fmt"
)

// docs 内嵌全部双语文档，文件名规则 <kind>.<lang>.md。
//
//go:embed docs/*.md
var docs embed.FS

// Get 按类型与语言返回文档内容。
// kind 目前支持 "help"（使用说明）与 "changelog"（升级日志）；
// 非法 kind 返回空串而不是 panic——调用方（绑定层/前端）拿到空串自行降级展示即可，
// 崩掉整个桌面端去惩罚一个文案取值不值得。
// lang 仅支持 zh-CN / en，其他值（含空串）回退英文版。
func Get(kind, lang string) string {
	if lang != "zh-CN" {
		// 回退口径：除 zh-CN 外一律给英文，避免为每个近似写法（zh/zh_cn）单独维护映射。
		lang = "en"
	}
	b, err := docs.ReadFile(fmt.Sprintf("docs/%s.%s.md", kind, lang))
	if err != nil {
		return ""
	}
	return string(b)
}
