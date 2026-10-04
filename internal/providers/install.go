package providers

import "runtime"

// Installer 可选接口：内置工具声明一键安装/卸载配方。自定义 Generic 不要实现。
type Installer interface {
	InstallRecipe() InstallRecipe
}

// InstallRecipe 是交给桌面绑定执行的完整 shell 命令（前端不得改写）。
type InstallRecipe struct {
	InstallCmd   string
	UninstallCmd string
	PurgeDirs    []string
	Shell        string // cmd | powershell | sh
}

func RecipeOf(p Provider) (InstallRecipe, bool) {
	ins, ok := p.(Installer)
	if !ok {
		return InstallRecipe{}, false
	}
	return ins.InstallRecipe(), true
}

func npmShell() string {
	if runtime.GOOS == "windows" {
		return "cmd"
	}
	return "sh"
}

func npmInstall(pkg string) InstallRecipe {
	return InstallRecipe{
		InstallCmd:   "npm install -g " + pkg,
		UninstallCmd: "npm uninstall -g " + pkg,
		Shell:        npmShell(),
	}
}
