package providers

import "testing"

func TestBuiltinsCoverKnownTools(t *testing.T) {
	ids := map[string]bool{}
	for _, p := range Builtins() {
		if ids[p.ID()] {
			t.Fatalf("duplicated builtin id %q", p.ID())
		}
		ids[p.ID()] = true
	}
	for _, want := range []string{"claude", "codex", "cursor", "codebuddy", "gemini", "opencode"} {
		if !ids[want] {
			t.Fatalf("builtin %q missing: %v", want, ids)
		}
	}
}

func TestMergeProvidersSkipsBuiltinIDs(t *testing.T) {
	specs := []GenericSpec{
		{ID: "opencode", Name: "OpenCode（用户旧配置）"},
		{ID: "codebuddy", Name: "CodeBuddy"},
	}

	ps := MergeProviders(Builtins(), specs, `C:\home`)

	var ids []string
	for _, p := range ps {
		ids = append(ids, p.ID())
	}
	want := []string{"claude", "codex", "cursor", "codebuddy", "gemini", "opencode"}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %v, want %v", ids, want)
		}
	}

	// opencode / codebuddy 必须是内置实现，而不是 yaml 里的 Generic：内置优先。
	if _, ok := ps[5].(Opencode); !ok {
		t.Fatalf("opencode entry is %T, want providers.Opencode", ps[5])
	}
	if _, ok := ps[3].(CodeBuddy); !ok {
		t.Fatalf("codebuddy entry is %T, want providers.CodeBuddy", ps[3])
	}
}

func TestMergeProvidersEmptySpecs(t *testing.T) {
	if got := len(MergeProviders(Builtins(), nil, "/home")); got != len(Builtins()) {
		t.Fatalf("len = %d, want %d", got, len(Builtins()))
	}
}
