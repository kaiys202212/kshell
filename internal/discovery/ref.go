package discovery

import (
	"fmt"
	"path"
	"strings"
)

const (
	KindLocal = "local"
	KindSSH   = "ssh"

	sshRefPrefix = "ssh://"
)

// FormatSSHRef 生成远程工作区稳定引用：ssh://<connID><cleanedPath>。
func FormatSSHRef(connID, remotePath string) string {
	return sshRefPrefix + connID + NormalizeRemotePath(remotePath)
}

// ParseWorkspaceRef 解析工作区引用。非 ssh:// 前缀一律视为本地路径。
func ParseWorkspaceRef(ref string) (kind, connID, pathStr string, err error) {
	if !strings.HasPrefix(ref, sshRefPrefix) {
		return KindLocal, "", ref, nil
	}
	rest := ref[len(sshRefPrefix):]
	slash := strings.Index(rest, "/")
	if slash < 0 {
		return "", "", "", fmt.Errorf("invalid ssh workspace ref: %q", ref)
	}
	connID = rest[:slash]
	if connID == "" {
		return "", "", "", fmt.Errorf("invalid ssh workspace ref: empty conn id in %q", ref)
	}
	return KindSSH, connID, NormalizeRemotePath(rest[slash:]), nil
}

// NormalizeRemotePath 按 POSIX 规则清理远端绝对路径，并保留 leading `/`。
func NormalizeRemotePath(p string) string {
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
