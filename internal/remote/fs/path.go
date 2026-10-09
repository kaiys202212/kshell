package fs

import (
	"fmt"
	"path"
	"strings"
)

// WithinRoot 判断 candidate 是否落在工作区根 root 内（远端按 Unix/POSIX）。
// 先 path.Clean 再比较，拒绝经 .. 逃出根的路径；也拒绝仅前缀相似（/ws vs /wsmore）。
func WithinRoot(root, candidate string) error {
	r := normalizeRemote(root)
	c := normalizeRemote(candidate)
	if c == r || strings.HasPrefix(c, r+"/") {
		return nil
	}
	return fmt.Errorf("err.remote.path_escape")
}

// normalizeRemote 清理远端绝对路径并保留 leading /（与 discovery.NormalizeRemotePath 同规则，
// 本包不依赖 discovery，避免 remote→discovery 反向耦合）。
func normalizeRemote(p string) string {
	if p == "" {
		return "/"
	}
	cleaned := path.Clean(p)
	if cleaned == "." {
		return "/"
	}
	if !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	return cleaned
}
