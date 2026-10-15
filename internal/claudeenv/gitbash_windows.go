//go:build windows

package claudeenv

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// 环境探测抽象成变量，测试可替换。
var (
	registryString = func(key string) (string, bool) {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\GitForWindows`, registry.QUERY_VALUE)
		if err != nil {
			return "", false
		}
		defer k.Close()
		v, _, err := k.GetStringValue("InstallPath")
		if err != nil || v == "" {
			return "", false
		}
		return v, true
	}
	lookBash = func() (string, bool) {
		bin, err := exec.LookPath("bash")
		if err != nil || bin == "" {
			return "", false
		}
		return bin, true
	}
	isFile = func(p string) bool {
		st, err := os.Stat(p)
		return err == nil && !st.IsDir()
	}
)

// errNoGitBash 表示所有候选都未命中，调用方不注入。
var errNoGitBash = errors.New("claudeenv: git-bash not found")

// gitBashEnvPlatform 按 claude CLI 同款语义解析 bash.exe：
// 1) 用户已设置 CLAUDE_CODE_GIT_BASH_PATH 且指向真实文件 → 原样放行（CLI 直接用）；
// 2) PATH 上有 bash → CLI 自己能找到，不注入；
// 3) 注册表 GitForWindows InstallPath 拼出 <p>\bin\bash.exe → 注入；
// 4) 全缺 → 不注入。
func gitBashEnvPlatform() (string, bool) {
	const envKey = "CLAUDE_CODE_GIT_BASH_PATH"
	if v := os.Getenv(envKey); v != "" && isFile(v) {
		return v, true
	}
	if _, ok := lookBash(); ok {
		return "", false
	}
	if root, ok := registryString(`SOFTWARE\GitForWindows`); ok {
		p := filepath.Join(root, "bin", "bash.exe")
		if isFile(p) {
			return p, true
		}
	}
	return "", false
}

var _ = errNoGitBash // 预留：未来把解析拆成返回 error 的形态时复用
