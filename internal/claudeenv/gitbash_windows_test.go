package claudeenv

import (
	"os"
	"path/filepath"
	"strings"
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
	oldRegistry, oldPath := registryString, bashOnPath
	t.Cleanup(func() { registryString, bashOnPath = oldRegistry, oldPath })
	registryString = func() (string, bool) { t.Fatal("不应读注册表"); return "", false }
	bashOnPath = func() (string, bool) { t.Fatal("不应查 PATH"); return "", false }

	got, ok := GitBashEnv()
	if !ok || got != bash {
		t.Fatalf("GitBashEnv = (%q, %v), want (%q, true)", got, ok, bash)
	}
}

// env 指向不存在的文件视为无效，继续后续分支。
func TestGitBashEnvInvalidEnvFallsThrough(t *testing.T) {
	withEnv(t, "CLAUDE_CODE_GIT_BASH_PATH", `C:\definitely\not\here\bash.exe`)
	oldRegistry, oldPath := registryString, bashOnPath
	t.Cleanup(func() { registryString, bashOnPath = oldRegistry, oldPath })
	bashOnPath = func() (string, bool) { return "", false }
	registryString = func() (string, bool) { return "", false }

	if _, ok := GitBashEnv(); ok {
		t.Fatal("无效 env 应回落且全缺时不注入")
	}
}

// PATH 上解析到 git-bash：CLI 不做 PATH 探测，必须显式注入解析出的路径。
func TestGitBashEnvPathBashInjects(t *testing.T) {
	withEnv(t, "CLAUDE_CODE_GIT_BASH_PATH", "")
	oldRegistry, oldPath := registryString, bashOnPath
	t.Cleanup(func() { registryString, bashOnPath = oldRegistry, oldPath })
	bashOnPath = func() (string, bool) { return `C:\Git\bin\bash.exe`, true }
	registryString = func() (string, bool) { t.Fatal("PATH 命中后不应读注册表"); return "", false }

	got, ok := GitBashEnv()
	if !ok || got != `C:\Git\bin\bash.exe` {
		t.Fatalf("GitBashEnv = (%q, %v), want (%q, true)", got, ok, `C:\Git\bin\bash.exe`)
	}
}

// bashOnPath：SystemRoot 下的 bash.exe 是 WSL 存根，跳过后应命中 PATH 后段的 git-bash。
// 同时覆盖大小写不敏感前缀排除（SystemRoot 故意传小写）与空 PATH 项跳过。
func TestBashOnPathSkipsSystemRootStub(t *testing.T) {
	sysRoot := t.TempDir() // SystemRoot 与 git 目录必须不同根，否则 git-bash 也被当存根跳过
	stubDir := filepath.Join(sysRoot, "System32")
	gitRoot := filepath.Join(t.TempDir(), "git")
	gitDir := filepath.Join(gitRoot, "bin")
	for _, dir := range []string{stubDir, gitDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "bash.exe"), nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// PATH 命中判定要求 git-bash（同目录须有 git.exe），纯 bash.exe 不算。
	if err := os.WriteFile(filepath.Join(gitDir, "git.exe"), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	withEnv(t, "SystemRoot", strings.ToLower(sysRoot))
	withEnv(t, "PATH", stubDir+string(os.PathListSeparator)+""+string(os.PathListSeparator)+gitDir)

	got, ok := bashOnPath()
	if !ok || got != filepath.Join(gitDir, "bash.exe") {
		t.Fatalf("bashOnPath = (%q, %v), want (%q, true)", got, ok, filepath.Join(gitDir, "bash.exe"))
	}
}

// bashOnPath：目录里有 bash.exe 但没有 git.exe（MSYS2/Cygwin 的 bash），
// 不视为 git-bash，继续扫后续目录。
func TestBashOnPathRequiresGitSibling(t *testing.T) {
	plainDir := filepath.Join(t.TempDir(), "msys2", "usr", "bin")
	gitRoot := filepath.Join(t.TempDir(), "git")
	gitDir := filepath.Join(gitRoot, "bin")
	for _, dir := range []string{plainDir, gitDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "bash.exe"), nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(gitRoot, "cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitRoot, "cmd", "git.exe"), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	withEnv(t, "SystemRoot", t.TempDir())
	withEnv(t, "PATH", plainDir+string(os.PathListSeparator)+gitDir)

	got, ok := bashOnPath()
	if !ok || got != filepath.Join(gitDir, "bash.exe") {
		t.Fatalf("bashOnPath = (%q, %v), want (%q, true)", got, ok, filepath.Join(gitDir, "bash.exe"))
	}
}

// bashOnPath：PATH 上没有可用的 bash → 不命中。
func TestBashOnPathNotFound(t *testing.T) {
	withEnv(t, "SystemRoot", t.TempDir())
	withEnv(t, "PATH", t.TempDir()) // 空目录，无 bash.exe

	if _, ok := bashOnPath(); ok {
		t.Fatal("PATH 无 bash 时不应命中")
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
	oldRegistry, oldPath := registryString, bashOnPath
	t.Cleanup(func() { registryString, bashOnPath = oldRegistry, oldPath })
	bashOnPath = func() (string, bool) { return "", false }
	registryString = func() (string, bool) { return root, true }

	got, ok := GitBashEnv()
	if !ok || got != bash {
		t.Fatalf("GitBashEnv = (%q, %v), want (%q, true)", got, ok, bash)
	}
}

// 注册表命中但 bash.exe 缺失（精简安装/卸载残留）→ 不注入。
func TestGitBashEnvRegistryMissingBinary(t *testing.T) {
	withEnv(t, "CLAUDE_CODE_GIT_BASH_PATH", "")
	oldRegistry, oldPath := registryString, bashOnPath
	t.Cleanup(func() { registryString, bashOnPath = oldRegistry, oldPath })
	bashOnPath = func() (string, bool) { return "", false }
	registryString = func() (string, bool) { return t.TempDir(), true }

	if _, ok := GitBashEnv(); ok {
		t.Fatal("注册表命中但 bash.exe 缺失时不应注入")
	}
}
