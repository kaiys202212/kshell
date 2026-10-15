//go:build !windows

package claudeenv

// gitBashEnvPlatform 非 Windows 恒不注入：git-bash 检测是 claude CLI 的
// Windows 专属行为，其他平台 CLI 直接可用。
func gitBashEnvPlatform() (string, bool) { return "", false }
