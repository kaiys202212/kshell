//go:build windows

package claudeenv

import (
	"os"
	"path/filepath"
	"strings"

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
	// bashOnPath 按 PATH 顺序逐目录解析 bash.exe，跳过 SystemRoot 下的
	// WSL bash 存根与非 Git for Windows 的 bash（MSYS2/Cygwin）。不用
	// exec.LookPath：它只返回首个命中，System32 存根排在前面时会把真正
	// 的 git-bash 挡住。
	bashOnPath = func() (string, bool) {
		sysRoot := strings.TrimRight(strings.ToLower(os.Getenv("SystemRoot")), `\`)
		for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
			if dir == "" {
				continue
			}
			p := filepath.Join(dir, "bash.exe")
			if sysRoot != "" && strings.HasPrefix(strings.ToLower(p), sysRoot+string(os.PathSeparator)) {
				continue
			}
			if isFile(p) && isGitBashDir(dir) {
				return p, true
			}
		}
		return "", false
	}
	// isGitBashDir 判断 bash.exe 所在目录是否属于 Git for Windows 安装：
	// bash 同级或其父级的 bin/cmd 下存在 git.exe。MSYS2/Cygwin 自带的
	// bash 不满足，注入给 CLI 会引发另一类难排查的失败。
	isGitBashDir = func(dir string) bool {
		if isFile(filepath.Join(dir, "git.exe")) {
			return true
		}
		parent := filepath.Dir(dir)
		return isFile(filepath.Join(parent, "bin", "git.exe")) ||
			isFile(filepath.Join(parent, "cmd", "git.exe"))
	}
	isFile = func(p string) bool {
		st, err := os.Stat(p)
		return err == nil && !st.IsDir()
	}
)

// gitBashEnvPlatform 按 claude CLI 同款语义解析 bash.exe：
// 1) 用户已设置 CLAUDE_CODE_GIT_BASH_PATH 且指向真实文件 → 原样放行（CLI 直接用）；
// 2) PATH 逐目录解析 bash.exe（跳过 WSL 存根与非 Git 的 bash）→ 注入解析出的路径。
//    注意：CLI 自身不做 PATH 探测，只认本环境变量与标准安装路径，
//    因此 PATH 命中也必须显式注入，不能假设「CLI 自己能找到」；
// 3) 注册表 GitForWindows InstallPath 拼出 <p>\bin\bash.exe → 注入；
// 4) 全缺 → 不注入。
func gitBashEnvPlatform() (string, bool) {
	const envKey = "CLAUDE_CODE_GIT_BASH_PATH"
	if v := os.Getenv(envKey); v != "" && isFile(v) {
		return v, true
	}
	if p, ok := bashOnPath(); ok {
		return p, true
	}
	if root, ok := registryString(); ok {
		p := filepath.Join(root, "bin", "bash.exe")
		if isFile(p) {
			return p, true
		}
	}
	return "", false
}
