package remote

import (
	"fmt"
	"os"
	"strings"
)

const askPassFlag = "--ssh-askpass"

// AskPassEnviron 为带密码的 ssh 子进程拼装 ASKPASS 相关环境变量。
// 密码只经 KSHELL_SSH_PASSWORD 传递，不进入 argv。
func AskPassEnviron(password string) ([]string, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("err.ssh.askpass_unavailable|%w", err)
	}
	return []string{
		"SSH_ASKPASS=" + exe,
		"SSH_ASKPASS_REQUIRE=force",
		"KSHELL_SSH_ASKPASS=1", // 哨兵：仅我们注入时才认作 askpass 启动
		"KSHELL_SSH_PASSWORD=" + password,
		// OpenSSH 在无 DISPLAY 时可能跳过 ASKPASS；占位即可触发。
		"DISPLAY=:0",
	}, nil
}

// AskPassOutput 返回 ASKPASS 助手应打印到 stdout 的密码。
func AskPassOutput() string {
	return os.Getenv("KSHELL_SSH_PASSWORD")
}

// TryAskPassMain 若本次启动是 ASKPASS 助手则打印密码并 os.Exit(0)，返回 true。
// 调用方应放在进程入口最前，避免拉起 Wails / TUI。
func TryAskPassMain(args []string) bool {
	if !isAskPassInvocation(args) {
		return false
	}
	fmt.Fprint(os.Stdout, AskPassOutput())
	os.Exit(0)
	return true
}

// tryAskPassMainNoExit 供测试：匹配 ASKPASS 时写 stdout 并返回 true，不 Exit。
func tryAskPassMainNoExit(args []string) bool {
	if !isAskPassInvocation(args) {
		return false
	}
	fmt.Fprint(os.Stdout, AskPassOutput())
	return true
}

func isAskPassInvocation(args []string) bool {
	if len(args) >= 2 && args[1] == askPassFlag {
		return true
	}
	// Windows OpenSSH 常直接执行 SSH_ASKPASS 路径，argv[1] 为提示文案。
	// 必须同时有我们注入的哨兵，避免用户环境误带密码相关变量时正常启动被劫持。
	if os.Getenv("KSHELL_SSH_ASKPASS") != "1" {
		return false
	}
	if strings.TrimSpace(os.Getenv("KSHELL_SSH_PASSWORD")) == "" {
		return false
	}
	if os.Getenv("SSH_ASKPASS_REQUIRE") != "force" {
		return false
	}
	// 排除已知子命令，避免带密码 env 时误入 askpass。
	if len(args) >= 2 {
		switch args[1] {
		case "agent-hook", "mcp-archive", askPassFlag:
			return args[1] == askPassFlag
		}
	}
	return true
}

// AskPassEnvMap 把 AskPassEnviron 转成 providers.Launch / 测试用的 map；无密码返回 nil。
func AskPassEnvMap(password string) (map[string]string, error) {
	if strings.TrimSpace(password) == "" {
		return nil, nil
	}
	kvs, err := AskPassEnviron(password)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			continue
		}
		m[kv[:i]] = kv[i+1:]
	}
	return m, nil
}

// AskPassEnvSlice 无密码返回 nil；有密码返回可并入 cmd.Env / terminal.Spec.Env 的切片。
func AskPassEnvSlice(password string) ([]string, error) {
	if strings.TrimSpace(password) == "" {
		return nil, nil
	}
	return AskPassEnviron(password)
}
