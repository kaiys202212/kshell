//go:build !windows

package executil

// ResolveShim 在非 Windows 平台没有包装脚本的问题，原样透传。
func ResolveShim(binPath string, args []string) (string, []string) {
	return binPath, args
}
