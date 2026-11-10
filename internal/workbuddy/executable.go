package workbuddy

import (
	"os"
	"path/filepath"
)

// EnvExecutable 允许用户/诊断显式指定 WorkBuddy 桌面端 exe 路径，
// 与 dsh-connect-workbuddy 的 WORKBUDDY_APP_EXECUTABLE 同名同义。
const EnvExecutable = "WORKBUDDY_APP_EXECUTABLE"

// windowsAppSubdirs 与 exe 名照抄 dsh 插件的候选清单：桌面端可能装在
// Programs 子目录或直接装在 Local/Program Files 根下，产品名两种拼写并存。
var windowsAppSubdirs = [][]string{
	{"Programs", "WorkBuddy"},
	{"WorkBuddy"},
	{"Programs", "WorkBuddyAI"},
	{"WorkBuddyAI"},
}

var appExecutableNames = []string{"WorkBuddy.exe", "WorkBuddyAI.exe"}

// appExecutableCandidates 按优先级拼出候选路径（env 候选由调用方前置）。
// 空基础目录跳过，不产生空路径段。
func appExecutableCandidates(localAppData, programFiles, programFilesX86 string) []string {
	bases := []string{localAppData, programFiles, programFilesX86}
	var out []string
	for _, base := range bases {
		if base == "" {
			continue
		}
		for _, sub := range windowsAppSubdirs {
			for _, name := range appExecutableNames {
				parts := append([]string{base}, sub...)
				out = append(out, filepath.Join(append(parts, name)...))
			}
		}
	}
	return out
}

// FindAppExecutable 定位 WorkBuddy 桌面端可执行文件：env 显式指定优先，
// 其次标准安装目录候选，最后注册表卸载项兜底（用户可能装在任意盘符）。
// 找不到返回 false；只做存在性检查，不启动进程。
func FindAppExecutable() (string, bool) {
	if p := os.Getenv(EnvExecutable); p != "" {
		if fileExists(p) {
			return p, true
		}
	}
	for _, c := range appExecutableCandidates(os.Getenv("LOCALAPPDATA"), os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")) {
		if fileExists(c) {
			return c, true
		}
	}
	return registryExecutable()
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
