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
	// Broken 为 true：Installed 但没有任何可执行入口（Source=config-dir），
	// 即安装残缺。UI 显示「已损坏」，桌面端可触发自动修复。
	Broken bool
	// Updating 为 true：官方自更新进行中的短暂窗口（Source=updating）。
	// 新建会话时据此提示「稍后重试」而非报残缺/无二进制。
	Updating bool
	// ACP 为 nil 表示该 provider 不支持 ACP；非 nil 时其 Available 表示本机是否可用。
	ACP *providers.ACPDetection
	// Auth 是登录态（见 providers.AuthProber）：in=已登录、out=未登录、
	// unknown=无法稳定探测，调用方必须保守放行。它是实时状态，每次组装都实探，
	// 不得沿用任何缓存/快照里的历史结论（toolcache 只缓存版本指纹；
	// 快照恢复路径见 LoadSnapshot 的归 unknown 处理）。
	Auth string
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
			Broken:    d.Broken,
			Updating:  d.Updating,
		}
		if d.Installed && d.BinPath != "" {
			t.Version = probe(d.BinPath, d.BinArgs)
		}
		// 仅对声明支持 ACP 的 provider 探测，避免无谓开销。
		if ap, ok := p.(providers.ACPProvider); ok {
			det := providers.DetectACP(ap.ACPAdapter())
			t.ACP = &det
		}
		// 登录态是实时状态：对实现 AuthProber 的 provider 实探，未实现的保守填
		// unknown（调用方放行）。缓存只影响版本号，这里每次都会重新执行。
		if ap, ok := p.(providers.AuthProber); ok {
			t.Auth = string(ap.ProbeAuth())
		} else {
			t.Auth = string(providers.AuthUnknown)
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
