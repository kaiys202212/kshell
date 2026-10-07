package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/cursoragent"
	"github.com/yangk/kshell/internal/discovery"
	"github.com/yangk/kshell/internal/providers"
	"github.com/yangk/kshell/internal/remote"
	"github.com/yangk/kshell/internal/remote/scanners"
	"github.com/yangk/kshell/internal/ui"
)

func main() {
	// SSH ASKPASS：OpenSSH 再拉起本进程回填密码；必须在一切初始化之前退出。
	if remote.TryAskPassMain(os.Args) {
		return
	}
	if cursoragent.ShouldProxy() {
		os.Exit(cursoragent.Main())
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "kshell: 无法确定用户主目录:", err)
		os.Exit(1)
	}

	paths, err := config.Paths()
	if err != nil {
		fmt.Fprintln(os.Stderr, "kshell:", err)
		os.Exit(1)
	}
	if err := config.EnsureRoot(paths); err != nil {
		fmt.Fprintln(os.Stderr, "kshell: 无法创建配置目录:", err)
		os.Exit(1)
	}

	cfg, cfgErr := config.Load(paths)

	store := remote.NewStore(paths.Connections)
	if err := store.Load(); err != nil {
		fmt.Fprintln(os.Stderr, "kshell: 读取连接文件失败:", err)
	}

	// 自定义工具（D 类）：文件不存在时写入带预置项的模板，存在则按用户配置加载。
	if err := providers.EnsureProvidersFile(paths.Providers); err != nil {
		fmt.Fprintln(os.Stderr, "kshell: 无法写入 providers.yaml:", err)
	}
	// 内置清单与 yaml 自定义定义合并（按 ID 去重、内置优先）。
	specs, _ := providers.LoadGenericSpecs(paths.Providers)
	providerList := providers.MergeProviders(providers.Builtins(), specs, home)

	// 项目表（手动添加 / 逻辑删除）：TUI 只读叠加，剔除已隐藏与目录不存在的项。
	projects := discovery.NewProjectStore(paths.Projects)
	if err := projects.Load(); err != nil {
		fmt.Fprintln(os.Stderr, "kshell: 读取项目表失败:", err)
	}

	model := ui.NewModelWith(ui.Options{
		Home:          home,
		Config:        cfg,
		CachePath:     paths.CacheIndex,
		Providers:     providerList,
		Store:         store,
		Projects:      projects,
		ThemeCacheDir: paths.CacheAppearance,
		Scanners: []remote.Scanner{
			scanners.SSHConfigScanner{},
			scanners.EnvScanner{},
			scanners.SpringScanner{},
			scanners.DeployScanner{},
			scanners.DocsScanner{},
		},
	})

	// 配置/连接文件的读取问题只作为状态栏提示，不阻断启动。
	if cfgErr != nil {
		model = model.WithStatus(cfgErr.Error(), true)
	}

	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "kshell 启动失败:", err)
		os.Exit(1)
	}
}
