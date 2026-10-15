//go:build !windows

package claudeenv

import "testing"

// 非 Windows 恒不注入：git-bash 检测是 claude CLI 的 Windows 专属行为。
func TestGitBashEnvNonWindows(t *testing.T) {
	if _, ok := GitBashEnv(); ok {
		t.Fatal("非 Windows 不应注入 CLAUDE_CODE_GIT_BASH_PATH")
	}
}
