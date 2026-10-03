package launcher

import (
	"context"
	"strings"
	"testing"

	"github.com/yangk/kshell/internal/providers"
)

// TestBuildCarriesEnv 验证 Build 会把 Launch.Env 经 providers.EnvList 展开成确定性列表；
// 未提供额外环境变量时必须保持 nil（由 Spec.Cmd 区分“仅继承”与“叠加”）。
func TestBuildCarriesEnv(t *testing.T) {
	dir := t.TempDir()
	bin := exitScript(t, dir, "tool", 0, "hi")

	spec, err := Build(providers.Launch{Path: bin, Env: map[string]string{"B": "2", "A": "1"}})
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	want := []string{"A=1", "B=2"}
	if len(spec.Env) != len(want) {
		t.Fatalf("env = %v, want %v", spec.Env, want)
	}
	for i := range want {
		if spec.Env[i] != want[i] {
			t.Fatalf("env = %v, want %v（应按 key 排序）", spec.Env, want)
		}
	}

	empty, err := Build(providers.Launch{Path: bin})
	if err != nil {
		t.Fatalf("Build error: %v", err)
	}
	if empty.Env != nil {
		t.Fatalf("无额外环境变量时 Env 应为 nil，实际 %v", empty.Env)
	}
}

// TestSpecCmdMergesParentEnvWithOverride 验证 Spec.Cmd 在存在额外变量时继承父进程环境，
// 且额外变量覆盖同名父变量。
func TestSpecCmdMergesParentEnvWithOverride(t *testing.T) {
	dir := t.TempDir()
	bin := exitScript(t, dir, "tool", 0, "hi")

	t.Setenv("KSHELL_PARENT_ONLY", "p")
	t.Setenv("KSHELL_OVERRIDE", "parent")

	cmd := Spec{Path: bin, Env: []string{"KSHELL_OVERRIDE=child", "KSHELL_EXTRA=e"}}.Cmd(context.Background())

	env := make(map[string]string, len(cmd.Env))
	for _, kv := range cmd.Env {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			env[kv[:i]] = kv[i+1:]
		}
	}

	if got := env["KSHELL_PARENT_ONLY"]; got != "p" {
		t.Fatalf("父进程变量未继承：KSHELL_PARENT_ONLY=%q，want %q", got, "p")
	}
	if got := env["KSHELL_EXTRA"]; got != "e" {
		t.Fatalf("额外变量缺失：KSHELL_EXTRA=%q，want %q", got, "e")
	}
	if got := env["KSHELL_OVERRIDE"]; got != "child" {
		t.Fatalf("额外变量未覆盖父变量：KSHELL_OVERRIDE=%q，want %q", got, "child")
	}
}
