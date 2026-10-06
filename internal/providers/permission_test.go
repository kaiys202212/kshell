package providers

import (
	"reflect"
	"testing"
)

func TestInjectPermissionBypass(t *testing.T) {
	args, env := Claude{}.InjectPermission(true)
	if !reflect.DeepEqual(args, []string{"--dangerously-skip-permissions"}) || len(env) != 0 {
		t.Fatalf("claude bypass = %v %v", args, env)
	}
	args, env = Codex{}.InjectPermission(true)
	if !reflect.DeepEqual(args, []string{"--ask-for-approval", "never"}) || len(env) != 0 {
		t.Fatalf("codex bypass = %v %v", args, env)
	}
	args, env = Opencode{}.InjectPermission(true)
	if !reflect.DeepEqual(args, []string{"--auto"}) || len(env) != 0 {
		t.Fatalf("opencode bypass = %v %v", args, env)
	}
	for _, p := range []interface {
		InjectPermission(bool) ([]string, map[string]string)
	}{Gemini{}} {
		args, env = p.InjectPermission(true)
		if len(args) != 0 || len(env) != 0 {
			t.Fatalf("%T 应 no-op，got %v %v", p, args, env)
		}
	}
}

func TestInjectPermissionDefaultNoop(t *testing.T) {
	for _, p := range []interface {
		InjectPermission(bool) ([]string, map[string]string)
	}{Claude{}, Codex{}, Gemini{}, Opencode{}} {
		args, env := p.InjectPermission(false)
		if len(args) != 0 || len(env) != 0 {
			t.Fatalf("%T default 不应注入：%v %v", p, args, env)
		}
	}
}
