//go:build !windows

package launcher

// Resolve 在非 Windows 平台没有包装脚本的问题，原样透传。
func Resolve(binPath string, args []string) Spec {
	return Spec{Path: binPath, Args: args}
}
