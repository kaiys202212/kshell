package providers

import "testing"

func TestDetectACP_PathHit(t *testing.T) {
	prev := lookPathFn
	lookPathFn = func(name string) (string, error) {
		if name == "claude-agent-acp" {
			return "/usr/bin/claude-agent-acp", nil
		}
		return "", errNotFoundStub
	}
	defer func() { lookPathFn = prev }()

	d := DetectACP(ACPAdapter{BinNames: []string{"claude-agent-acp"}})
	if !d.Available || d.Source != "path" || d.BinPath != "/usr/bin/claude-agent-acp" {
		t.Fatalf("detection = %+v", d)
	}
}

func TestDetectACP_NpxFallback(t *testing.T) {
	prev := lookPathFn
	lookPathFn = func(name string) (string, error) {
		if name == "npx" {
			return "/usr/bin/npx", nil
		}
		return "", errNotFoundStub
	}
	defer func() { lookPathFn = prev }()

	d := DetectACP(ACPAdapter{BinNames: []string{"claude-agent-acp"}, NPMPackage: "@agentclientprotocol/claude-agent-acp"})
	if !d.Available || d.Source != "npx" || d.Package != "@agentclientprotocol/claude-agent-acp" {
		t.Fatalf("detection = %+v", d)
	}
}

func TestDetectACP_None(t *testing.T) {
	prev := lookPathFn
	lookPathFn = func(string) (string, error) { return "", errNotFoundStub }
	defer func() { lookPathFn = prev }()

	if d := DetectACP(ACPAdapter{BinNames: []string{"claude-agent-acp"}}); d.Available {
		t.Fatalf("want unavailable, got %+v", d)
	}
}

func TestDetectACP_CarriesExtraArgs(t *testing.T) {
	prev := lookPathFn
	lookPathFn = func(name string) (string, error) {
		if name == "claude-agent-acp" {
			return "/usr/bin/claude-agent-acp", nil
		}
		return "", errNotFoundStub
	}
	defer func() { lookPathFn = prev }()

	d := DetectACP(ACPAdapter{BinNames: []string{"claude-agent-acp"}, ExtraArgs: []string{"--foo"}})
	if len(d.ExtraArgs) != 1 || d.ExtraArgs[0] != "--foo" {
		t.Fatalf("extra args = %v", d.ExtraArgs)
	}
}

func TestClaudeACPAdapter(t *testing.T) {
	a := Claude{}.ACPAdapter()
	if len(a.BinNames) == 0 || a.NPMPackage == "" {
		t.Fatalf("adapter = %+v", a)
	}
}

func TestCodeBuddyACPAdapter(t *testing.T) {
	// CodeBuddy CLI 自带 --acp（stdio），适配器与 CLI 同二进制，无 npx 兜底。
	a := CodeBuddy{}.ACPAdapter()
	if len(a.BinNames) != 2 || a.BinNames[0] != "codebuddy" || a.BinNames[1] != "cbc" {
		t.Fatalf("bin names = %v", a.BinNames)
	}
	if len(a.ExtraArgs) != 1 || a.ExtraArgs[0] != "--acp" {
		t.Fatalf("extra args = %v", a.ExtraArgs)
	}
	if a.NPMPackage != "" {
		t.Fatalf("want no npm package, got %q", a.NPMPackage)
	}
}

func TestCursorACPAdapter(t *testing.T) {
	// Cursor CLI 无内置 ACP，走第三方适配器 cursor-acp；PATH 未装时 npx 兜底。
	a := Cursor{}.ACPAdapter()
	if len(a.BinNames) != 1 || a.BinNames[0] != "cursor-acp" {
		t.Fatalf("bin names = %v", a.BinNames)
	}
	if a.NPMPackage != "cursor-acp" {
		t.Fatalf("npm package = %q", a.NPMPackage)
	}
}
