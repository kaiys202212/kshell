//go:build windows

package claudeenv

import (
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// 环境探测抽象成变量，测试可替换。
var (
	// registryString 依次查 HKLM / HKCU 的 SOFTWARE\GitForWindows\InstallPath
	//（Git 可机器级也可用户级安装）。
	registryString = func() (string, bool) {
		for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
			k, err := registry.OpenKey(root, `SOFTWARE\GitForWindows`, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			v, _, err := k.GetStringValue("InstallPath")
			k.Close()
			if err != nil || v == "" {
				continue
			}
			return v, true
		}
		return "", false
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
	if root, ok := registryString(); ok {
		p := filepath.Join(root, "bin", "bash.exe")
		if isFile(p) {
			return p, true
		}
	}
	return "", false
}
