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
