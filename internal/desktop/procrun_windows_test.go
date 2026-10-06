//go:build windows

package desktop

import "testing"

// PowerShell 命令行构造：目录内单引号转义防注入，OrdinalIgnoreCase 匹配。
func TestPowershellProcScanCmd(t *testing.T) {
	cmd := powershellProcScanCmd(`C:\Users\me\.cursor's dir`)
	want := `Get-Process | Where-Object { $_.Path -and $_.Path.StartsWith('C:\Users\me\.cursor''s dir', [StringComparison]::OrdinalIgnoreCase) } | Select-Object -First 1 -ExpandProperty Path`
	if cmd != want {
		t.Fatalf("cmd = %q\nwant %q", cmd, want)
	}
}
