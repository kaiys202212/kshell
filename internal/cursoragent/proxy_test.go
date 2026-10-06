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
	body := "console.log(process.argv.slice(2).join('|')); process.exit(0);\n"
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "--print", "hello")
	cmd.Env = append(os.Environ(),
		"KSHELL_CURSORAGENT_TEST=1",
		EnvAsProxy+"=1",
		EnvNode+"="+node,
		EnvScript+"="+script,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("proxy: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if got != "--print|hello" {
		t.Fatalf("argv = %q", got)
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
