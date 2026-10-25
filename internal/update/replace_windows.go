//go:build windows

package update

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/yangk/kshell/internal/executil"
)

func psLiteral(p string) string {
	return strings.ReplaceAll(p, "'", "''")
}

// updateLogPath 返回升级日志的绝对路径；拿不到用户目录时回退 USERPROFILE
// 环境变量，仍拿不到则返回空串（脚本侧再用 $env:USERPROFILE 兜底）。
func updateLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.Getenv("USERPROFILE")
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".kshell", "logs", "update.log")
}

func replaceWaitStartPowerShell(pid int, currentExe, newExe, logPath string) string {
	cur := psLiteral(currentExe)
	nxt := psLiteral(newExe)
	logLit := psLiteral(logPath)
	leaf := psLiteral(filepath.Base(currentExe))
	// Windows 允许对运行中的映像文件改名、只禁止删除/写。旧脚本用
	// Move-Item -Force 覆盖 = 删除目标 + 改名，而 kshell 会拉起同映像的
	// 长效助手子进程（mcp-archive / mcp-ssh / askpass / agent-hook），只要
	// 有一个存活覆盖必失败 → exe 没换、新进程没起，且无任何日志。故改为：
	// 旧 exe 改名让位（改名不动文件本体）→ exe.new 纯改名落位 → 拉起。
	// 每步 Add-Content 写 ASCII 日志 + 整体 try/catch，失败不再静默。
	// --after-update：新进程 PrepareLaunch 识别升级拉起，可在超时后强杀残留实例。
	return fmt.Sprintf(`$ErrorActionPreference='Stop'; $cur='%s'; $new='%s'; $log='%s'; if(-not $log){ $log=Join-Path $env:USERPROFILE '.kshell/logs/update.log' }; function Log([string]$m){ try{ Add-Content -LiteralPath $log -Value $m -Encoding UTF8 } catch{} }; try{ New-Item -ItemType Directory -Force -Path (Split-Path -LiteralPath $log -Parent) | Out-Null }catch{}; try{ Log 'wait old process exit'; Wait-Process -Id %d -ErrorAction SilentlyContinue; Start-Sleep -Seconds 1; Log 'rename old exe aside'; $old=$cur+'.upgrade-old'; if(Test-Path -LiteralPath $old){ try{ Remove-Item -LiteralPath $old -Force -ErrorAction Stop; Log 'remove stale upgrade-old ok' }catch{ $old=$cur+'.upgrade-old-'+[DateTime]::Now.Ticks; Log 'remove stale failed, use unique name' } }; Rename-Item -LiteralPath $cur -NewName (Split-Path -LiteralPath $old -Leaf); Log 'rename new exe into place'; Rename-Item -LiteralPath $new -NewName (Split-Path -LiteralPath $cur -Leaf); Start-Sleep -Milliseconds 500; Log 'start new process'; Start-Process -LiteralPath $cur -ArgumentList '--after-update' -WindowStyle Normal; Log 'update done' }catch{ Log ('update failed: ' + $_.Exception.Message); throw } finally{ Log 'cleanup upgrade-old leftovers'; Get-ChildItem -LiteralPath (Split-Path -LiteralPath $cur -Parent) -Filter '%s.upgrade-old*' -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue }`,
		cur, nxt, logLit, pid, leaf)
}

func spawnReplaceCmd(currentExe, newExe string) *exec.Cmd {
	logPath := updateLogPath()
	// 日志目录尽力预建；失败交给脚本内 New-Item 再兜底。
	if logPath != "" {
		_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	}
	ps := replaceWaitStartPowerShell(os.Getpid(), currentExe, newExe, logPath)
	cmd := exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command", ps)
	executil.DetachFromParent(cmd)
	return cmd
}

func spawnReplaceDefault(currentExe, newExe string) error {
	return spawnReplaceCmd(currentExe, newExe).Start()
}
