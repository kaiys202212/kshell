//go:build windows

package providers

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEnumerateSessionsSpacedBinPath：opencode CLI 落在含空格目录
// （机器 B 的 Program Files）时，枚举必须成功。
func TestEnumerateSessionsSpacedBinPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Program Files Fake")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bat := filepath.Join(dir, "opencode.cmd")
	body := "@echo off\r\n" +
		"echo [{\"id\":\"ses_1\",\"cwd\":\"C:\\\\wk\",\"title\":\"t\",\"created\":1,\"updated\":2}]\r\n" +
		"exit /b 0\r\n"
	if err := os.WriteFile(bat, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	sessions, err := Opencode{}.EnumerateSessions(t.TempDir(), bat)
	if err != nil {
		t.Fatalf("枚举失败: %v", err)
	}
	if len(sessions) != 1 || sessions[0].ID != "ses_1" {
		t.Fatalf("sessions = %+v", sessions)
	}
}
