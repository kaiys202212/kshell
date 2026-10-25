//go:build windows

package update

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestReplaceWaitStartPowerShell(t *testing.T) {
	pid := 4242
	cur := `C:\Program Files\kshell\kshell-desktop.exe`
	nxt := cur + ".new"
	log := `C:\Users\u\.kshell\logs\update.log`
	ps := replaceWaitStartPowerShell(pid, cur, nxt, log)
	for _, want := range []string{
		"Wait-Process -Id 4242",
		// 日志：绝对路径由 Go 侧算好嵌入，每步 Add-Content 追加
		log,
		"Add-Content -LiteralPath $log",
		"-Encoding UTF8",
		// 旧 exe 改名让位（改名不动文件本体，助手进程持有映像也不碍事）
		".upgrade-old",
		// 残留删不掉时回退唯一名
		"[DateTime]::Now.Ticks",
		"Remove-Item -LiteralPath $old",
		// 新 exe 纯改名落位（不用 Move-Item -Force，避免覆盖=删除语义）
		"Rename-Item -LiteralPath $cur",
		"Rename-Item -LiteralPath $new",
		"Start-Process -LiteralPath $cur",
		"-ArgumentList '--after-update'",
		"-WindowStyle Normal",
		// 整体 try/catch，失败也写日志
		"catch",
		// catch rethrow 前尽力回滚：旧 exe 已让位（改名成功）而新 exe 未落位时，
		// 先把旧 exe 改名回去，避免安装目录落到无可执行文件。$old 在 Ticks 回退
		// 分支里才是实际让位名，回滚必须用 $old 而非固定 .upgrade-old。
		"if($old -and -not (Test-Path -LiteralPath $cur) -and (Test-Path -LiteralPath $old))",
		"Rename-Item -LiteralPath $old -NewName (Split-Path -LiteralPath $cur -Leaf)",
		// finally 残留清理加前置守卫：仅当新 exe 已在位（Test-Path $cur）才删
		// .upgrade-old*，否则保留刚让位的旧 exe，绝不删掉唯一可执行文件
		"if(Test-Path -LiteralPath $cur){ Log 'cleanup upgrade-old leftovers'",
		// 尽力清理 .upgrade-old* 残留
		".upgrade-old*",
		// 路径含空格应原样保留在单引号里
		cur,
		nxt,
	} {
		if !strings.Contains(ps, want) {
			t.Fatalf("脚本缺少 %q:\n%s", want, ps)
		}
	}

	// 关键顺序：等旧进程 → 旧 exe 改名让位 → 新 exe 改名落位 → 拉起新进程
	type step struct {
		name  string
		token string
	}
	order := []step{
		{"等旧进程退出", "Wait-Process -Id 4242"},
		{"旧 exe 改名让位", "Rename-Item -LiteralPath $cur -NewName"},
		{"新 exe 改名落位", "Rename-Item -LiteralPath $new -NewName"},
		{"拉起新进程", "Start-Process -LiteralPath $cur"},
	}
	last := -1
	for _, s := range order {
		i := strings.Index(ps, s.token)
		if i < 0 {
			t.Fatalf("缺少步骤 %s:\n%s", s.name, ps)
		}
		if i < last {
			t.Fatalf("步骤 %s 顺序错误:\n%s", s.name, ps)
		}
		last = i
	}

	// 单引号转义
	ps = replaceWaitStartPowerShell(pid, `C:\a'b.exe`, `C:\a'b.exe.new`, log)
	if strings.Contains(ps, `a'b`) && !strings.Contains(ps, `a''b`) {
		t.Fatalf("单引号应转义: %s", ps)
	}

	// 拿不到日志路径时回退脚本侧 USERPROFILE；Join-Path 必须移进守卫，
	// 避免空 USERPROFILE 时 Join-Path 抛错被外层吞掉、日志静默失效
	ps = replaceWaitStartPowerShell(pid, cur, nxt, "")
	if !strings.Contains(ps, "if(-not $log -and $env:USERPROFILE){ $log=Join-Path $env:USERPROFILE") {
		t.Fatalf("日志路径为空应回退 USERPROFILE 且受空值守卫:\n%s", ps)
	}
}

func TestUpdateLogPath(t *testing.T) {
	t.Setenv("USERPROFILE", `C:\up-test`)
	t.Setenv("HOME", "")
	if got := updateLogPath(); got != filepath.Join(`C:\up-test`, ".kshell", "logs", "update.log") {
		t.Fatalf("updateLogPath() = %q", got)
	}
	// 无 home 不 panic，返回空串由脚本侧兜底
	t.Setenv("USERPROFILE", "")
	t.Setenv("HOME", "")
	if got := updateLogPath(); got != "" {
		t.Fatalf("无 home 时应返回空串, got %q", got)
	}
}

func TestSpawnReplaceCmd_脱离父进程(t *testing.T) {
	cmd := spawnReplaceCmd(`C:\kshell\kshell-desktop.exe`, `C:\kshell\kshell-desktop.exe.new`)
	if cmd.SysProcAttr == nil {
		t.Fatal("未脱离父进程 Job")
	}
	if cmd.SysProcAttr.CreationFlags&0x01000000 == 0 {
		t.Fatalf("CreationFlags = %#x 缺少 CREATE_BREAKAWAY_FROM_JOB", cmd.SysProcAttr.CreationFlags)
	}
}
