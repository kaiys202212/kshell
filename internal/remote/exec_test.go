package remote

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func writeFakeSSH(t *testing.T, dir, body string) string {
	t.Helper()
	var path string
	if runtime.GOOS == "windows" {
		path = filepath.Join(dir, "ssh.cmd")
		body = "@echo off\r\n" + body + "\r\n"
	} else {
		path = filepath.Join(dir, "ssh")
		body = "#!/bin/sh\n" + body + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFindSSHFound(t *testing.T) {
	dir := t.TempDir()
	writeFakeSSH(t, dir, "echo ok")
	t.Setenv("PATH", dir)

	bin, err := FindSSH()
	if err != nil || bin == "" {
		t.Fatalf("FindSSH = %q, %v", bin, err)
	}
}

func TestFindSSHMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := FindSSH()
	if err == nil {
		t.Fatal("missing ssh must return an error")
	}
	if !strings.Contains(err.Error(), "ssh") {
		t.Fatalf("error should mention ssh, got %v", err)
	}
}

func TestBuildArgs(t *testing.T) {
	c := Connection{Name: "prod", Host: "10.0.0.1", User: "root", Port: 2222, IdentityFile: "C:\\keys\\id_ed25519"}
	args := BuildArgs(c, "uname -a", SSHOptions{ConnectTimeout: 5})

	want := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=5",
		"-p", "2222",
		"-i", "C:\\keys\\id_ed25519",
		"root@10.0.0.1",
		"--", "uname -a",
	}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("args = %v, want %v", args, want)
	}
}

func TestBuildArgsSkipsDefaults(t *testing.T) {
	c := Connection{Host: "h"}
	args := BuildArgs(c, "", SSHOptions{ConnectTimeout: 3})
	got := strings.Join(args, " ")
	if strings.Contains(got, "-p") {
		t.Fatalf("default port should be omitted: %s", got)
	}
	if strings.Contains(got, "-i") {
		t.Fatalf("missing identity should be omitted: %s", got)
	}
	if strings.Contains(got, "--") {
		t.Fatalf("empty command should not add --: %s", got)
	}
}

func TestBuildArgsNeverCarriesPasswords(t *testing.T) {
	c := Connection{Host: "h", User: "u"}
	got := strings.Join(BuildArgs(c, "ls", SSHOptions{}), " ")
	if !strings.Contains(got, "BatchMode=yes") {
		t.Fatalf("BatchMode is what prevents password prompts: %s", got)
	}
	for _, forbidden := range []string{"PasswordAuthentication", "sshpass", "-o StrictHostKeyChecking=no"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("unsafe option %q must never be added: %s", forbidden, got)
		}
	}
}

func TestRunCapturesOutputAndExitCode(t *testing.T) {
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		writeFakeSSH(t, dir, "echo hello\nexit /b 3")
	} else {
		writeFakeSSH(t, dir, "echo hello\nexit 3")
	}
	t.Setenv("PATH", dir)

	res, err := Run(context.Background(), Connection{Host: "h"}, "cmd", SSHOptions{ConnectTimeout: 1, CommandTimeoutSeconds: 10})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if res.ExitCode != 3 {
		t.Fatalf("exit code = %d, want 3", res.ExitCode)
	}
	if !strings.Contains(res.Stdout, "hello") {
		t.Fatalf("stdout = %q", res.Stdout)
	}
}

func TestRunTimeout(t *testing.T) {
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		writeFakeSSH(t, dir, "powershell -NoProfile -Command Start-Sleep -Seconds 10")
	} else {
		writeFakeSSH(t, dir, "sleep 10")
	}
	t.Setenv("PATH", dir)

	start := time.Now()
	res, err := Run(context.Background(), Connection{Host: "h"}, "cmd", SSHOptions{CommandTimeoutSeconds: 1})
	if elapsed := time.Since(start); elapsed > 8*time.Second {
		t.Fatalf("run was not cancelled, elapsed %s", elapsed)
	}
	// 超时的进程要么报错，要么给出非 0 退出码，绝不能看起来像执行成功。
	if err == nil && res.ExitCode == 0 {
		t.Fatal("a cancelled run must not look like a successful one")
	}
}

func TestShellArgsRequestsTTY(t *testing.T) {
	args := ShellArgs(Connection{Host: "h", User: "u", Port: 2200}, SSHOptions{ConnectTimeout: 5})
	got := strings.Join(args, " ")
	if !strings.Contains(got, "-t") {
		t.Fatalf("interactive shell needs -t: %s", got)
	}
	if !strings.Contains(got, "u@h") {
		t.Fatalf("target missing: %s", got)
	}
}
