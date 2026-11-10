//go:build !windows

package workbuddy

// registryExecutable 仅 Windows 有注册表兜底；其他平台无此候选。
func registryExecutable() (string, bool) {
	return "", false
}
