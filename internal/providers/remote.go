package providers

import (
	"errors"
	"fmt"
	"strings"
)

// errRemoteToolNotFound 是远端未探测到工具 CLI 时的哨兵；对外文案带工具 ID。
var errRemoteToolNotFound = errors.New("err.remote.tool_not_found")

// ErrRemoteToolNotFound 返回 err.remote.tool_not_found|<toolID>。
func ErrRemoteToolNotFound(toolID string) error {
	return fmt.Errorf("%w|%s", errRemoteToolNotFound, toolID)
}

// RemoteLauncher：有官方远程协议时由本地二进制启动（不经 ssh 包一层 CLI）。
type RemoteLauncher interface {
	NewRemoteSessionCmd(hostTarget, remotePath, bin string) (Launch, error)
	ResumeRemoteCmd(hostTarget, remotePath string, s Session, bin string) (Launch, error)
}

// RemoteSSHRunner：无官方协议（或协议路径不可用）时在远端执行 CLI。
// 返回值是远端 bin 之后的参数片段；工作目录由调用方用 RemoteShellCommand 拼进 shell。
type RemoteSSHRunner interface {
	RemoteNewArgs(remotePath string) []string
	RemoteResumeArgs(s Session) []string
}

// SSHRemoteFolderURI 拼装 VS Code / Cursor Remote-SSH folder URI。
// 形如 vscode-remote://ssh-remote+user@host/abs/path（权威段与路径之间无多余斜杠）。
// 非 22 端口依赖本机 ~/.ssh/config Host 别名；此处只用 user@host（或 host）。
func SSHRemoteFolderURI(hostTarget, remotePath string) string {
	hostTarget = strings.TrimSpace(hostTarget)
	p := strings.TrimSpace(remotePath)
	if p == "" {
		p = "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "vscode-remote://ssh-remote+" + hostTarget + p
}

// shellSingleQuotePOSIX 按 POSIX 单引号规则转义，供拼进 ssh 远端 shell。
func shellSingleQuotePOSIX(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// RemoteShellCommand 生成 `cd '<path>' && '<bin>' [args…]` 单行远端命令。
func RemoteShellCommand(remotePath, bin string, args []string) string {
	var b strings.Builder
	b.WriteString("cd ")
	b.WriteString(shellSingleQuotePOSIX(remotePath))
	b.WriteString(" && ")
	b.WriteString(shellSingleQuotePOSIX(bin))
	for _, a := range args {
		b.WriteByte(' ')
		b.WriteString(shellSingleQuotePOSIX(a))
	}
	return b.String()
}
