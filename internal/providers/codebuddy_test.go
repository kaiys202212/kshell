package providers

import (
	"strings"
	"testing"
)

func TestCodeBuddyIdentityAndDetect(t *testing.T) {
	p := CodeBuddy{}
	if p.ID() != "codebuddy" || p.DisplayName() != "CodeBuddy" {
		t.Fatalf("id/name = %s %s", p.ID(), p.DisplayName())
	}
	spec := p.DetectSpec("")
	if spec.BinName != "codebuddy" {
		t.Fatalf("bin = %q", spec.BinName)
	}
	if len(spec.AltBinNames) != 1 || spec.AltBinNames[0] != "cbc" {
		t.Fatalf("alt = %v", spec.AltBinNames)
	}
	if len(spec.ConfigDirs) != 1 || spec.ConfigDirs[0] != "~/.codebuddy" {
		t.Fatalf("dirs = %v", spec.ConfigDirs)
	}
}

func TestCodeBuddyResumeAndMatch(t *testing.T) {
	p := CodeBuddy{}
	launch := p.ResumeCmd(Session{ID: "cb-1", Workspace: `D:\ws`}, "codebuddy")
	if len(launch.Args) != 2 || launch.Args[0] != "--resume" || launch.Args[1] != "cb-1" {
		t.Fatalf("args = %v", launch.Args)
	}
	if !p.MatchSessionRel(`ws-1/01a0.jsonl`) {
		t.Fatal("session file must match")
	}
	if p.MatchSessionRel(`ws-1/01a0.jsonl/subagents/agent-1.jsonl`) {
		t.Fatal("subagent must not match")
	}
}

func TestCodeBuddyParsesSummaryTitle(t *testing.T) {
	content := `{"type":"session-meta","id":"m-1","sessionId":"cb-9","timestamp":1790242904766,"cwd":"D:\\ws\\demo"}` + "\n" +
		`{"id":"m-3","timestamp":1790243000000,"type":"summary","summary":"设计 RPC 任务架构","providerData":{"source":"initial-user-message"}}` + "\n"
	got, err := (CodeBuddy{}).ParseSession("cb.jsonl", []byte(content))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "cb-9" || got.Title != "设计 RPC 任务架构" {
		t.Fatalf("got %+v", got)
	}
}

func TestCodeBuddyRecipeNpm(t *testing.T) {
	r, ok := RecipeOf(CodeBuddy{})
	if !ok {
		t.Fatal("missing recipe")
	}
	if !strings.Contains(r.InstallCmd, "@tencent-ai/codebuddy-code") {
		t.Fatalf("install = %q", r.InstallCmd)
	}
	if !strings.Contains(r.UninstallCmd, "@tencent-ai/codebuddy-code") {
		t.Fatalf("uninstall = %q", r.UninstallCmd)
	}
	if len(r.PurgeDirs) != 1 || r.PurgeDirs[0] != "~/.codebuddy" {
		t.Fatalf("purge = %v", r.PurgeDirs)
	}
}

func TestDefaultYAMLOmitsCodeBuddyID(t *testing.T) {
	if strings.Contains(DefaultProvidersYAML(), "id: codebuddy") {
		t.Fatal("default yaml must not declare builtin codebuddy")
	}
	if !strings.Contains(DefaultProvidersYAML(), "id: cline") {
		t.Fatal("cline preset must remain")
	}
}

func TestMergeProvidersPrefersBuiltinCodeBuddy(t *testing.T) {
	specs := []GenericSpec{{ID: "codebuddy", Name: "YAML 覆盖名"}}
	ps := MergeProviders(Builtins(), specs, `C:\home`)
	var p Provider
	for _, x := range ps {
		if x.ID() == "codebuddy" {
			p = x
		}
	}
	if p == nil {
		t.Fatal("missing codebuddy")
	}
	if _, ok := p.(CodeBuddy); !ok {
		t.Fatalf("got %T", p)
	}
	if p.DisplayName() != "CodeBuddy" {
		t.Fatalf("name = %q", p.DisplayName())
	}
}
