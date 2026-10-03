package terminal

import (
	"strings"
	"testing"
)

func TestMergedEnvNilWhenEmpty(t *testing.T) {
	if got := mergedEnv(nil); got != nil {
		t.Fatalf("mergedEnv(nil) = %v, want nil", got)
	}
}

func TestMergedEnvIncludesParent(t *testing.T) {
	t.Setenv("KSHELL_TEST_PARENT", "1")
	got := mergedEnv([]string{"COLORFGBG=15;0"})
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "COLORFGBG=15;0") {
		t.Fatalf("missing extra env: %v", got)
	}
	if !strings.Contains(joined, "KSHELL_TEST_PARENT=1") {
		t.Fatalf("missing parent env: %v", got)
	}
}

// TestMergedEnvOverridesParent 验证同名额外变量排在父环境之后，从而覆盖父进程取值。
func TestMergedEnvOverridesParent(t *testing.T) {
	t.Setenv("KSHELL_OVERRIDE", "parent")
	got := mergedEnv([]string{"KSHELL_OVERRIDE=child"})

	env := make(map[string]string, len(got))
	for _, kv := range got {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			env[kv[:i]] = kv[i+1:]
		}
	}
	if v := env["KSHELL_OVERRIDE"]; v != "child" {
		t.Fatalf("KSHELL_OVERRIDE = %q, want %q（额外变量应覆盖父进程）", v, "child")
	}
}
