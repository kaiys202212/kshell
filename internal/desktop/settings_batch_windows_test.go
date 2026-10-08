//go:build windows

package desktop

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/yangk/kshell/internal/executil"
	"github.com/yangk/kshell/internal/launcher"
)

// TestMaterializedSettingsSurvivesCmdBatch 真机证明：落盘后的 --settings 路径
// 经 BatchCommandLine（含空格 bat + cmd /S /C）后，node 仍能读到合法 JSON。
func TestMaterializedSettingsSurvivesCmdBatch(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Program Files Fake")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	checker := filepath.Join(t.TempDir(), "check.js")
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("需要 node 做接收端校验")
	}
	js := `
const fs = require("fs");
const i = process.argv.indexOf("--settings");
if (i < 0) { console.log("NO_SETTINGS"); process.exit(2); }
const p = process.argv[i+1];
if (p.startsWith("{")) { console.log("STILL_INLINE"); process.exit(3); }
JSON.parse(fs.readFileSync(p, "utf8"));
console.log("JSON_OK");
`
	if err := os.WriteFile(checker, []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
	bat := filepath.Join(dir, "claude.cmd")
	body := "@echo off\r\n\"" + node + "\" \"" + checker + "\" %*\r\n"
	if err := os.WriteFile(bat, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	spec := &launcher.Spec{Args: []string{"--settings", `{"theme":"dark"}`}}
	applyNotifyInject(spec, "claude", "new:1", `D:\ws`)
	settings := settingsPath(t, spec.Args)
	t.Cleanup(func() { _ = os.Remove(settings) })

	comspec := os.Getenv("COMSPEC")
	if comspec == "" {
		comspec = "cmd.exe"
	}
	cmdline := executil.BatchCommandLine(comspec, bat, spec.Args)
	cmd := exec.Command(comspec)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: cmdline, HideWindow: true}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("Run error: %v\ncmdline=%s\nstdout=%s\nstderr=%s", err, cmdline, stdout.String(), stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("JSON_OK")) {
		t.Fatalf("期望 JSON_OK, stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}
