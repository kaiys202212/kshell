package cursoragent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("KSHELL_CURSORAGENT_TEST") == "1" {
		os.Exit(Main())
	}
	os.Exit(m.Run())
}

func TestProxyMainForwardsArgsAndExitCode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("本机无 node")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "index.js")
	body := "const e=process.env; console.log(process.argv.slice(2).join('|')+'|as='+String(e.KSHELL_AS_CURSOR_AGENT||'')+'|exe='+String(e.CURSOR_AGENT_EXECUTABLE||'')); process.exit(0);\n"
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "--print", "hello")
	cmd.Env = append(os.Environ(),
		"KSHELL_CURSORAGENT_TEST=1",
		EnvAsProxy+"=1",
		EnvNode+"="+node,
		EnvScript+"="+script,
		"CURSOR_AGENT_EXECUTABLE=C:\\should\\not\\leak.exe",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("proxy: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if got != "--print|hello|as=|exe=" {
		t.Fatalf("argv/env = %q", got)
	}
}

func TestShouldProxyRequiresAllEnv(t *testing.T) {
	t.Setenv(EnvAsProxy, "1")
	t.Setenv(EnvNode, "")
	t.Setenv(EnvScript, "")
	if ShouldProxy() {
		t.Fatal("仅 AsProxy 不应进入代理，避免桌面闪退")
	}
	t.Setenv(EnvNode, "node")
	t.Setenv(EnvScript, "index.js")
	if !ShouldProxy() {
		t.Fatal("三变量齐全时应代理")
	}
}

func TestProxyMainMissingEnvExitsOne(t *testing.T) {
	cmd := exec.Command(os.Args[0], "--print")
	cmd.Env = append(os.Environ(),
		"KSHELL_CURSORAGENT_TEST=1",
		EnvAsProxy+"=1",
		EnvNode+"=",
		EnvScript+"=",
	)
	err := cmd.Run()
	if err == nil {
		t.Fatal("期望非 0")
	}
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() != 1 {
		t.Fatalf("err = %v", err)
	}
}
