package version

import "strings"

// Version 由发布构建通过 -ldflags -X 注入，本地/开发构建保持 dev。
var Version = "dev"

// Current 返回桌面端当前版本字符串。
func Current() string { return Version }

// IsRelease 报告当前构建是否为发布版：Version 为空 / dev / unknown（大小写
// 不敏感、可含首尾空白）视为非发布，与 update.ShouldSkip 同口径。
func IsRelease() bool {
	switch strings.ToLower(strings.TrimSpace(Version)) {
	case "", "dev", "unknown":
		return false
	default:
		return true
	}
}
