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
	dir := psLiteral(filepath.Dir(currentExe))
	logLit := psLiteral(logPath)
	leaf := psLiteral(filepath.Base(currentExe))
	// Windows 允许对运行中的映像文件改名、只禁止删除/写。旧脚本用
	// Move-Item -Force 覆盖 = 删除目标 + 改名，而 kshell 会拉起同映像的
	// 长效助手子进程（mcp-archive / mcp-ssh / askpass / agent-hook），只要
	// 有一个存活覆盖必失败 → exe 没换、新进程没起，且无任何日志。故改为：
	// 旧 exe 改名让位（改名不动文件本体）→ exe.new 纯改名落位 → 拉起。
	// 每步 Add-Content 写 ASCII 日志 + 整体 try/catch，失败不再静默。
	// 脚本禁用 Split-Path：Windows PowerShell 5.1 的 Split-Path 参数集里
	// -Leaf/-Parent 只配 -Path，唯一接受 -LiteralPath 的参数集只支持
	// -Resolve，`Split-Path -LiteralPath x -Leaf/-Parent` 在 5.1 下必抛
	// AmbiguousParameterSet（v0.1.21 升级必失败的根因）。叶名/父目录改由
	// Go 侧传入字面量（$leaf/$dir），动态叶名用 [IO.Path] 方法（5.1/7 通用）。
	// Start-Process 同理：5.1 没有 -LiteralPath 参数，用 -FilePath。
	// --after-update：新进程 PrepareLaunch 识别升级拉起，可在超时后强杀残留实例。
	// catch rethrow 前尽力回滚：若旧 exe 已改名让位而新 exe 落位失败（安装目录
	// 暂无可执行文件），先把实际让位名 $old（Ticks 回退分支里才定名）改回原
	// leaf 名，保证失败路径上安装目录始终留有可运行 exe；回滚失败静默，交给
	// 用户重装兜底。
	// finally 的残留清理加前置守卫：仅当新 exe 已在位（Test-Path $cur）才删
	// .upgrade-old*；否则绝不动让位文件（否则回滚前的 finally 会把唯一可执行
	// 文件也删掉）。守卫为假时残留保留，由下次升级脚本兜底。
	return fmt.Sprintf(`$ErrorActionPreference='Stop'; $cur='%s'; $new='%s'; $dir='%s'; $log='%s'; $leaf='%s'; if(-not $log -and $env:USERPROFILE){ $log=Join-Path $env:USERPROFILE '.kshell/logs/update.log' }; function Log([string]$m){ try{ Add-Content -LiteralPath $log -Value $m -Encoding UTF8 } catch{} }; try{ New-Item -ItemType Directory -Force -Path ([IO.Path]::GetDirectoryName($log)) | Out-Null }catch{}; try{ Log 'wait old process exit'; Wait-Process -Id %d -ErrorAction SilentlyContinue; Start-Sleep -Seconds 1; Log 'rename old exe aside'; $old=$cur+'.upgrade-old'; if(Test-Path -LiteralPath $old){ try{ Remove-Item -LiteralPath $old -Force -ErrorAction Stop; Log 'remove stale upgrade-old ok' }catch{ $old=$cur+'.upgrade-old-'+[DateTime]::Now.Ticks; Log 'remove stale failed, use unique name' } }; Rename-Item -LiteralPath $cur -NewName ([IO.Path]::GetFileName($old)); Log 'rename new exe into place'; Rename-Item -LiteralPath $new -NewName $leaf; Start-Sleep -Milliseconds 500; Log 'start new process'; Start-Process -FilePath $cur -ArgumentList '--after-update' -WindowStyle Normal; Log 'update done' }catch{ Log ('update failed: ' + $_.Exception.Message); if($old -and -not (Test-Path -LiteralPath $cur) -and (Test-Path -LiteralPath $old)){ try{ Rename-Item -LiteralPath $old -NewName $leaf; Log 'rollback old exe into place' }catch{ Log 'rollback old exe failed' } }; throw } finally{ if(Test-Path -LiteralPath $cur){ Log 'cleanup upgrade-old leftovers'; Get-ChildItem -LiteralPath $dir -Filter '%s.upgrade-old*' -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue }else{ Log 'skip cleanup: new exe missing, keep upgrade-old as runnable exe' } }`,
		cur, nxt, dir, logLit, leaf, pid, leaf)
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
