# build/cli —— 最小 CLI 二进制注入目录

本目录是桌面版构建的编译占位：`build.ps1 -Desktop` 会先编译
`./cmd/kshell-cli` 到这里的 `kshell-cli.exe`（平台对应产物），
随后 `wails build` 经根 `main.go` 的 `//go:embed build/cli` 把它打进桌面端，
运行时由 `internal/clisync` 自我释放到用户 bin 目录（`~/.kshell/bin` /
`~/.local/bin`）。

`*.exe` 已被全局 gitignore，仓库里只保留本 README 保证 embed 始终可解析
（dev 构建只嵌到占位文件，释放步骤会识别并跳过）。
