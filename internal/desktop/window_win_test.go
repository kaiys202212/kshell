//go:build windows

package desktop

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestPsEncodeRoundTrip(t *testing.T) {
	script := "Set-Location -LiteralPath 'D:\\tmp'; Write-Output '中文 ok' | Out-File -LiteralPath 'C:\\tmp\\a.txt'"

	raw, err := base64.StdEncoding.DecodeString(psEncode(script))
	if err != nil {
		t.Fatalf("psEncode 结果不是合法 base64: %v", err)
	}
	if len(raw)%2 != 0 {
		t.Fatalf("psEncode 结果不是 UTF-16LE（长度 %d 为奇数）", len(raw))
	}
	units := make([]uint16, 0, len(raw)/2)
	for i := 0; i < len(raw); i += 2 {
		units = append(units, uint16(raw[i])|uint16(raw[i+1])<<8)
	}
	if got := string(utf16.Decode(units)); got != script {
		t.Fatalf("psEncode 往返失真:\n got=%q\nwant=%q", got, script)
	}
}

func TestPsQuoteEscapes(t *testing.T) {
	if got := psQuote(`D:\a'b`); got != `'D:\a''b'` {
		t.Fatalf("psQuote 未双写内嵌单引号, got %q", got)
	}
}

func TestStartPowershellCmdLineQuotesSpacedTitle(t *testing.T) {
	got := startPowershellCmdLine(`C:\Windows\System32\cmd.exe`, `kshell · my session`, "ENCODED")
	want := `C:\Windows\System32\cmd.exe /S /C "start "kshell · my session" powershell -NoExit -EncodedCommand ENCODED"`
	if got != want {
		t.Fatalf("startPowershellCmdLine =\n %q\nwant\n %q", got, want)
	}
	// 无空格标题也必须带引号，避免 start 把标题当成命令。
	got2 := startPowershellCmdLine(`C:\Windows\System32\cmd.exe`, `kshell`, "E")
	want2 := `C:\Windows\System32\cmd.exe /S /C "start "kshell" powershell -NoExit -EncodedCommand E"`
	if got2 != want2 {
		t.Fatalf("plain title =\n %q\nwant\n %q", got2, want2)
	}
}

func TestWindowsLauncherFocusUnknownTitle(t *testing.T) {
	l := &windowsLauncher{}
	never := "kshell · 绝无此窗口__" + strings.Repeat("x", 40)
	if l.Focus(never) {
		t.Fatal("不存在的窗口标题 Focus 应返回 false")
	}
	if windowsProcAlive(never) {
		t.Fatal("不存在的窗口标题 windowsProcAlive 应返回 false")
	}
}
