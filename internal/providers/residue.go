package providers

import "os"

// HasResidue 判断工具使用残留：优先看 ResidueDirs（会话/登录数据，任一存在即真）；
// 未声明时退化为 ConfigDirs 任一存在且**非空**（空目录更像卸载残留而非活跃使用）。
// 用于自动修复的准入判定，配合 kshell 卸载标记组成「非用户主动卸载」双信号。
func HasResidue(spec DetectSpec, home string) bool {
	if len(spec.ResidueDirs) > 0 {
		for _, dir := range spec.ResidueDirs {
			if isDir(expandHome(dir, home)) {
				return true
			}
		}
		return false
	}
	for _, dir := range spec.ConfigDirs {
		expanded := expandHome(dir, home)
		if !isDir(expanded) {
			continue
		}
		entries, err := os.ReadDir(expanded)
		if err == nil && len(entries) > 0 {
			return true
		}
	}
	return false
}
