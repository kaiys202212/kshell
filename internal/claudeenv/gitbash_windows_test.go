package claudeenv

import (
	"os"
	"path/filepath"
	"testing"
)

// withEnv 设置/还原单个环境变量。
func withEnv(t *testing.T, key, val string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	t.Setenv(key, val)
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func TestGitBashEnvExplicitEnvWins(t *testing.T) {
	bash := filepath.Join(t.TempDir(), "bash.exe")
	if err := os.WriteFile(bash, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	withEnv(t, "CLAUDE_CODE_GIT_BASH_PATH", bash)
	oldRegistry, oldLook := registryString, lookBash
	t.Cleanup(func() { registryString, lookBash = oldRegistry, oldLook })
	registryString = func(string) (string, bool) { t.Fatal("不应读注册表"); return "", false }
	lookBash = func() (string, bool) { t.Fatal("不应查 PATH"); return "", false }

	got, ok := GitBashEnv()
	if !ok || got != bash {
		t.Fatalf("GitBashEnv = (%q, %v), want (%q, true)", got, ok, bash)
	}
}

// env 指向不存在的文件视为无效，继续后续分支。
func TestGitBashEnvInvalidEnvFallsThrough(t *testing.T) {
	withEnv(t, "CLAUDE_CODE_GIT_BASH_PATH", `C:\definitely\not\here\bash.exe`)
	oldRegistry, oldLook := registryString, lookBash
	t.Cleanup(func() { registryString, lookBash = oldRegistry, oldLook })
	lookBash = func() (string, bool) { return "", false }
	registryString = func(string) (string, bool) { return "", false }

	if _, ok := GitBashEnv(); ok {
		t.Fatal("无效 env 应回落且全缺时不注入")
	}
}

// PATH 上有 bash：CLI 自己能找到，不注入。
func TestGitBashEnvPathBashSkips(t *testing.T) {
	withEnv(t, "CLAUDE_CODE_GIT_BASH_PATH", "")
	oldRegistry, oldLook := registryString, lookBash
	t.Cleanup(func() { registryString, lookBash = oldRegistry, oldLook })
	lookBash = func() (string, bool) { return `C:\Git\bin\bash.exe`, true }
	registryString = func(string) (string, bool) { t.Fatal("PATH 命中后不应读注册表"); return "", false }

	if _, ok := GitBashEnv(); ok {
		t.Fatal("PATH 有 bash 时不应注入")
	}
}

// 注册表 InstallPath 拼出真实 bash.exe → 注入该路径。
func TestGitBashEnvRegistryHit(t *testing.T) {
	withEnv(t, "CLAUDE_CODE_GIT_BASH_PATH", "")
	root := t.TempDir()
	bashDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(bashDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bash := filepath.Join(bashDir, "bash.exe")
	if err := os.WriteFile(bash, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	oldRegistry, oldLook := registryString, lookBash
	t.Cleanup(func() { registryString, lookBash = oldRegistry, oldLook })
	lookBash = func() (string, bool) { return "", false }
	registryString = func(string) (string, bool) { return root, true }

	got, ok := GitBashEnv()
	if !ok || got != bash {
		t.Fatalf("GitBashEnv = (%q, %v), want (%q, true)", got, ok, bash)
	}
}

// 注册表命中但 bash.exe 缺失（精简安装/卸载残留）→ 不注入。
func TestGitBashEnvRegistryMissingBinary(t *testing.T) {
	withEnv(t, "CLAUDE_CODE_GIT_BASH_PATH", "")
	oldRegistry, oldLook := registryString, lookBash
	t.Cleanup(func() { registryString, lookBash = oldRegistry, oldLook })
	lookBash = func() (string, bool) { return "", false }
	registryString = func(string) (string, bool) { return t.TempDir(), true }

	if _, ok := GitBashEnv(); ok {
		t.Fatal("注册表命中但 bash.exe 缺失时不应注入")
	}
}
