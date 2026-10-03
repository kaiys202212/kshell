package providers

import "os/exec"

// errNotFoundStub 供测试注入使用。
var errNotFoundStub = exec.ErrNotFound

// lookPathFn 抽象 exec.LookPath，测试可替换。
var lookPathFn = exec.LookPath

// ACPAdapter 声明某工具的 ACP 适配器来源。
type ACPAdapter struct {
	BinNames   []string
	NPMPackage string
	ExtraArgs  []string
}

// ACPProvider 可选接口：实现即表示该工具可用 ACP 交互。
type ACPProvider interface {
	ACPAdapter() ACPAdapter
}

// ACPDetection 是 ACP 适配器可用性探测结果（字段供前端 JSON 读取，PascalCase）。
type ACPDetection struct {
	Available bool     `json:"Available"`
	Source    string   `json:"Source"`
	BinPath   string   `json:"BinPath,omitempty"`
	Package   string   `json:"Package,omitempty"`
	ExtraArgs []string `json:"ExtraArgs,omitempty"`
}

// DetectACP 依次探测 BinNames，全未命中时用 npx 兜底。
func DetectACP(a ACPAdapter) ACPDetection {
	for _, name := range a.BinNames {
		if bin, err := lookPathFn(name); err == nil && bin != "" {
			return ACPDetection{Available: true, Source: "path", BinPath: bin, ExtraArgs: a.ExtraArgs}
		}
	}
	if a.NPMPackage != "" {
		for _, npx := range npxNames() {
			if bin, err := lookPathFn(npx); err == nil && bin != "" {
				return ACPDetection{Available: true, Source: "npx", BinPath: bin, Package: a.NPMPackage, ExtraArgs: a.ExtraArgs}
			}
		}
	}
	return ACPDetection{}
}
