package remote

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestAskPassEnvironSetsRequiredVars(t *testing.T) {
	env, err := AskPassEnviron("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	m := envMap(env)
	if m["SSH_ASKPASS"] == "" {
		t.Fatalf("SSH_ASKPASS missing: %v", env)
	}
	if m["SSH_ASKPASS_REQUIRE"] != "force" {
		t.Fatalf("SSH_ASKPASS_REQUIRE=%q", m["SSH_ASKPASS_REQUIRE"])
	}
	if m["KSHELL_SSH_PASSWORD"] != "s3cret" {
		t.Fatalf("KSHELL_SSH_PASSWORD=%q", m["KSHELL_SSH_PASSWORD"])
	}
	if m["KSHELL_SSH_ASKPASS"] != "1" {
		t.Fatalf("KSHELL_SSH_ASKPASS=%q", m["KSHELL_SSH_ASKPASS"])
	}
	if m["DISPLAY"] == "" {
		t.Fatalf("DISPLAY placeholder missing: %v", env)
	}
	if strings.Contains(m["SSH_ASKPASS"], "s3cret") {
		t.Fatal("password must not appear in SSH_ASKPASS path")
	}
}

func TestAskPassOutputReadsEnv(t *testing.T) {
	t.Setenv("KSHELL_SSH_PASSWORD", "from-env")
	if got := AskPassOutput(); got != "from-env" {
		t.Fatalf("AskPassOutput = %q", got)
	}
}

func TestTryAskPassMainPrintsAndSignalsHandled(t *testing.T) {
	t.Setenv("KSHELL_SSH_PASSWORD", "pw-via-flag")
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	handled := tryAskPassMainNoExit([]string{"kshell", "--ssh-askpass"})
	_ = w.Close()
	os.Stdout = old
	if !handled {
		t.Fatal("expected handled")
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "pw-via-flag" {
		t.Fatalf("stdout = %q", buf.String())
	}
}

func TestTryAskPassMainIgnoresNormalArgs(t *testing.T) {
	t.Setenv("KSHELL_SSH_PASSWORD", "")
	t.Setenv("KSHELL_SSH_ASKPASS", "")
	if tryAskPassMainNoExit([]string{"kshell", "mcp-archive"}) {
		t.Fatal("normal args must not be treated as askpass")
	}
}

func TestTryAskPassMainRequiresSentinelWithoutFlag(t *testing.T) {
	t.Setenv("KSHELL_SSH_PASSWORD", "pw")
	t.Setenv("SSH_ASKPASS_REQUIRE", "force")
	t.Setenv("KSHELL_SSH_ASKPASS", "")
	if tryAskPassMainNoExit([]string{"kshell", "Enter passphrase for key"}) {
		t.Fatal("without KSHELL_SSH_ASKPASS=1 must not treat as askpass")
	}
	t.Setenv("KSHELL_SSH_ASKPASS", "1")
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	handled := tryAskPassMainNoExit([]string{"kshell", "Enter passphrase for key"})
	_ = w.Close()
	os.Stdout = old
	if !handled {
		t.Fatal("expected handled with sentinel")
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "pw" {
		t.Fatalf("stdout = %q", buf.String())
	}
}

func envMap(kvs []string) map[string]string {
	m := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			continue
		}
		m[kv[:i]] = kv[i+1:]
	}
	return m
}
