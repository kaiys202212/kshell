package version

// Version 由发布构建通过 -ldflags -X 注入，本地/开发构建保持 dev。
var Version = "dev"

// Current 返回桌面端当前版本字符串。
func Current() string { return Version }
