package discovery

import (
	"sort"

	"github.com/yangk/kshell/internal/providers"
)

// Tool 是 UI 顶栏展示用的工具状态；未安装的也要返回，以便灰显提示。
type Tool struct {
	ID        string
	Name      string
	BinPath   string
	BinArgs   []string // 入口前缀参数：node 入口形态下为 [主脚本名]，启动与版本探测都须先拼上
	Version   string
	Installed bool
	Source    string
	// ACP 为 nil 表示该 provider 不支持 ACP；非 nil 时其 Available 表示本机是否可用。
	ACP *providers.ACPDetection
}

// DetectAll 汇总所有 provider 的安装状态，已安装的排在前面（UI 顶栏按此顺序展示）。
// 每次都实探版本（详见 DetectAllCached：桌面端启动走缓存版）。
func DetectAll(home string, ps []providers.Provider) []Tool {
	return detectAll(home, ps, func(bin string, args []string) string {
		return providers.ProbeVersion(bin, args...)
	})
}

// detectAll 是 DetectAll 的主体，probe 抽象「取某个可执行文件的版本」，
// 便于在 DetectAllCached 里换成带缓存的实现。
func detectAll(home string, ps []providers.Provider, probe func(bin string, args []string) string) []Tool {
	tools := make([]Tool, 0, len(ps))
	for _, p := range ps {
		d := providers.Detect(p.DetectSpec(home), home)

		t := Tool{
			ID:        p.ID(),
			Name:      p.DisplayName(),
			BinPath:   d.BinPath,
			BinArgs:   d.BinArgs,
			Version:   "unknown",
			Installed: d.Installed,
			Source:    d.Source,
		}
		if d.Installed && d.BinPath != "" {
			t.Version = probe(d.BinPath, d.BinArgs)
		}
		// 仅对声明支持 ACP 的 provider 探测，避免无谓开销。
		if ap, ok := p.(providers.ACPProvider); ok {
			det := providers.DetectACP(ap.ACPAdapter())
			t.ACP = &det
		}
		tools = append(tools, t)
	}

	sort.SliceStable(tools, func(i, j int) bool {
		if tools[i].Installed != tools[j].Installed {
			return tools[i].Installed
		}
		return tools[i].ID < tools[j].ID
	})
	return tools
}
