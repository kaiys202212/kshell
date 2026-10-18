package discovery

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/yangk/kshell/internal/providers"
)

type stubProvider struct {
	id   string
	spec providers.DetectSpec
}

func (s stubProvider) ID() string                             { return s.id }
func (s stubProvider) DisplayName() string                    { return s.id }
func (s stubProvider) DetectSpec(string) providers.DetectSpec { return s.spec }
func (s stubProvider) SessionRoots(string) []string           { return nil }
func (s stubProvider) SessionFilePattern() string             { return "*.jsonl" }
func (s stubProvider) ParseSession(string, []byte) (*providers.Session, error) {
	return nil, nil
}
func (s stubProvider) NewSessionCmd(string, string) providers.Launch {
	return providers.Launch{}
}
func (s stubProvider) ResumeCmd(providers.Session, string) providers.Launch {
	return providers.Launch{}
}

// stubAuthProvider 在 stubProvider 上叠加可控的 ProbeAuth 返回值与调用计数，
// 验证 Tool.Auth 的注入与「每次组装都实探」的实时性。
type stubAuthProvider struct {
	stubProvider
	status providers.AuthStatus
	calls  int
}

func (s *stubAuthProvider) ProbeAuth() providers.AuthStatus {
	s.calls++
	return s.status
}

func TestDetectAllFillsAuth(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	prober := &stubAuthProvider{id: "cursor", status: providers.AuthOut}

	tests := []struct {
		name    string
		p       providers.Provider
		want    providers.AuthStatus
		wantAsk int // ProbeAuth 期望被调用的次数
	}{
		{"实现 AuthProber 的注入实探结论", prober, providers.AuthOut, 1},
		{"未实现 AuthProber 的保守填 unknown", stubProvider{id: "claude"}, providers.AuthUnknown, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := DetectAll(t.TempDir(), []providers.Provider{tt.p})
			if len(tools) != 1 {
				t.Fatalf("got %d tools, want 1", len(tools))
			}
			if tools[0].Auth != string(tt.want) {
				t.Fatalf("Auth = %q, want %q", tools[0].Auth, tt.want)
			}
		})
	}
	if prober.calls != 1 {
		t.Fatalf("ProbeAuth 调用 %d 次, want 1", prober.calls)
	}
}

// TestDetectAllCachedReprobesAuth 锁定「登录态不被缓存冻结」：toolcache 只缓存
// 版本指纹，Tool 每次都重建、Auth 每次都实探——缓存命中的第二轮也必须拿到翻转
// 后的登录态。
func TestDetectAllCachedReprobesAuth(t *testing.T) {
	binDir := t.TempDir()
	writeFakeBin(t, binDir, "cursor")
	t.Setenv("PATH", binDir)
	cachePath := filepath.Join(t.TempDir(), "toolcache.json")

	p := &stubAuthProvider{id: "cursor", spec: providers.DetectSpec{BinName: "cursor"}, status: providers.AuthOut}
	first := DetectAllCached(t.TempDir(), []providers.Provider{p}, cachePath)
	if first[0].Auth != string(providers.AuthOut) || p.calls != 1 {
		t.Fatalf("首轮 Auth = %q calls = %d, want out/1", first[0].Auth, p.calls)
	}

	// 版本不变（缓存命中）但登录态翻转：Auth 必须重新实探
	p.status = providers.AuthIn
	second := DetectAllCached(t.TempDir(), []providers.Provider{p}, cachePath)
	if second[0].Version != "cursor 1.2.3" {
		t.Fatalf("版本应命中缓存, got %q", second[0].Version)
	}
	if second[0].Auth != string(providers.AuthIn) || p.calls != 2 {
		t.Fatalf("缓存命中也必须重探登录态: Auth = %q calls = %d, want in/2", second[0].Auth, p.calls)
	}
}

func writeFakeBin(t *testing.T, dir, name string) {
	t.Helper()
	var path string
	var body string
	if runtime.GOOS == "windows" {
		path = filepath.Join(dir, name+".cmd")
		body = "@echo off\r\necho " + name + " 1.2.3\r\n"
	} else {
		path = filepath.Join(dir, name)
		body = "#!/bin/sh\necho " + name + " 1.2.3\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake bin: %v", err)
	}
}

func TestDetectAllPutsInstalledFirst(t *testing.T) {
	binDir := t.TempDir()
	writeFakeBin(t, binDir, "claude")
	t.Setenv("PATH", binDir)

	tools := DetectAll(t.TempDir(), []providers.Provider{
		stubProvider{id: "zulu"},
		stubProvider{id: "claude", spec: providers.DetectSpec{BinName: "claude"}},
		stubProvider{id: "alpha"},
	})

	if len(tools) != 3 {
		t.Fatalf("got %d tools, want 3", len(tools))
	}
	if tools[0].ID != "claude" || !tools[0].Installed {
		t.Fatalf("installed tool should come first, got %+v", tools[0])
	}
	if tools[1].ID != "alpha" || tools[2].ID != "zulu" {
		t.Fatalf("uninstalled tools should be sorted by id, got %s, %s", tools[1].ID, tools[2].ID)
	}
	if tools[0].Version != "claude 1.2.3" {
		t.Fatalf("version = %q, want %q", tools[0].Version, "claude 1.2.3")
	}
}

func TestDetectAllKeepsUninstalledTools(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	tools := DetectAll(t.TempDir(), []providers.Provider{stubProvider{id: "gemini"}})
	if len(tools) != 1 {
		t.Fatalf("got %d tools, want 1", len(tools))
	}
	if tools[0].Installed {
		t.Fatalf("missing tool must still be reported as uninstalled: %+v", tools[0])
	}
	if tools[0].Version != "unknown" {
		t.Fatalf("version = %q, want unknown", tools[0].Version)
	}
}

func TestDetectAll_FillsACP(t *testing.T) {
	tools := DetectAll("", []providers.Provider{providers.Claude{}})
	if len(tools) != 1 {
		t.Fatalf("tools = %+v", tools)
	}
	if tools[0].ACP == nil {
		t.Fatal("ACP detection not attached")
	}
}

func TestDetectAllCarriesBinArgs(t *testing.T) {
	// 隔离 PATH：本机若有真实/自建 cursor-agent shim，LookPath 会抢先命中导致探测走偏
	t.Setenv("PATH", t.TempDir())
	home := t.TempDir()
	vdir := filepath.Join(home, "va")
	if err := os.MkdirAll(vdir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 精确命名的 node 入口文件（writeFakeBin 会改后缀，不能用）
	if err := os.WriteFile(filepath.Join(vdir, "node.exe"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vdir, "index.js"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := stubProvider{id: "cursor", spec: providers.DetectSpec{
		BinName:         "cursor-agent",
		InstallDirs:     []string{vdir},
		NodeEntryScript: "index.js",
	}}
	probe := func(bin string, args []string) string {
		if len(args) != 1 || filepath.Base(args[0]) != "index.js" || !filepath.IsAbs(args[0]) {
			t.Fatalf("probe 应收到绝对路径 BinArgs, got %v", args)
		}
		return "2026.10.01"
	}
	tools := detectAll(home, []providers.Provider{p}, probe)
	if !tools[0].Installed || tools[0].BinPath == "" {
		t.Fatalf("应检出 node 入口, got %+v", tools[0])
	}
	if len(tools[0].BinArgs) != 1 || filepath.Base(tools[0].BinArgs[0]) != "index.js" || !filepath.IsAbs(tools[0].BinArgs[0]) {
		t.Fatalf("Tool 应携带绝对路径 BinArgs, got %+v", tools[0])
	}
	if tools[0].Version != "2026.10.01" {
		t.Fatalf("version = %q", tools[0].Version)
	}
}
