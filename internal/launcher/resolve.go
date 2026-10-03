package launcher

import "github.com/yangk/kshell/internal/executil"

// Resolve 处理命令包装脚本（Windows 上 npm 安装的 CLI 常是 .ps1，不能直接 exec）：
// 优先用同目录 .cmd 兄弟文件，否则套一层 powershell -Command。
// 具体规则与 providers 共用，实现在 executil.ResolveShim。
func Resolve(binPath string, args []string) Spec {
	path, resolved := executil.ResolveShim(binPath, args)
	return Spec{Path: path, Args: resolved}
}
