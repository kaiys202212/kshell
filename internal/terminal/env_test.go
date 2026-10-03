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
