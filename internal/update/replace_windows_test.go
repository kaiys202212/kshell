//go:build windows

package update

import (
	"os"
	"os/exec"
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
		// PS 5.1 的 Start-Process 没有 -LiteralPath 参数，只能 -FilePath
		"Start-Process -FilePath $cur",
		"-ArgumentList '--after-update'",
		"-WindowStyle Normal",
		// 整体 try/catch，失败也写日志
		"catch",
		// catch rethrow 前尽力回滚：旧 exe 已让位（改名成功）而新 exe 未落位时，
		// 先把旧 exe 改名回去，避免安装目录落到无可执行文件。$old 在 Ticks 回退
		// 分支里才是实际让位名，回滚必须用 $old 而非固定 .upgrade-old。
		"if($old -and -not (Test-Path -LiteralPath $cur) -and (Test-Path -LiteralPath $old))",
		// 关键：脚本不得包含 Split-Path。PS 5.1 的 Split-Path 参数集里
		// -Leaf/-Parent 只配 -Path，唯一接受 -LiteralPath 的参数集只支持
		// -Resolve，`Split-Path -LiteralPath x -Leaf/-Parent` 在 5.1 下必抛
		// AmbiguousParameterSet（v0.1.21 升级必失败的根因），cmdlet 层运行时
		// 错误字符串断言拦不住用法本身，这里反向断言直接禁掉。
		// 叶名/父目录一律用 Go 侧传入字面量（$leaf/$dir）与 [IO.Path] 方法。
		"$dir='C:\\Program Files\\kshell'",
		"[IO.Path]::GetDirectoryName($log)",
		"[IO.Path]::GetFileName($old)",
		// 落位与回滚的 -NewName 用 Go 传入叶名字面量
		"-NewName $leaf",
		"Rename-Item -LiteralPath $old -NewName $leaf",
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
		{"拉起新进程", "Start-Process -FilePath $cur"},
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

	// 反向守卫：脚本任何位置不得出现 Split-Path（理由见上方 wants 注释）
	ps = replaceWaitStartPowerShell(pid, cur, nxt, log)
	if strings.Contains(ps, "Split-Path") {
		t.Fatalf("脚本禁用 Split-Path（PS 5.1 参数集限制）:\n%s", ps)
	}

	// 拿不到日志路径时回退脚本侧 USERPROFILE；Join-Path 必须移进守卫，
	// 避免空 USERPROFILE 时 Join-Path 抛错被外层吞掉、日志静默失效
	ps = replaceWaitStartPowerShell(pid, cur, nxt, "")
	if !strings.Contains(ps, "if(-not $log -and $env:USERPROFILE){ $log=Join-Path $env:USERPROFILE") {
		t.Fatalf("日志路径为空应回退 USERPROFILE 且受空值守卫:\n%s", ps)
	}
}

func TestReplaceWaitStartPowerShell_真实执行(t *testing.T) {
	// 用真 Windows PowerShell 5.1 执行整段脚本：cmdlet 层的运行时错误（如
	// v0.1.21 的 Split-Path 参数集 bug）字符串断言拦不住，必须真跑一遍。
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("拿不到测试二进制路径: %v", err)
	}
	src, err := os.ReadFile(self)
	if err != nil {
		t.Fatalf("读测试二进制: %v", err)
	}
	dir := t.TempDir()
	cur := filepath.Join(dir, "kshell-desktop.exe")
	nxt := cur + ".new"
	for _, p := range []string{cur, nxt} {
		if err := os.WriteFile(p, src, 0o755); err != nil {
			t.Fatalf("准备 %s: %v", p, err)
		}
	}
	// 日志放进不存在的子目录，顺带覆盖脚本内 New-Item 建目录路径
	logPath := filepath.Join(dir, "logs", "update.log")
	ps := replaceWaitStartPowerShell(999999, cur, nxt, logPath)
	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", ps)
	out, err := cmd.CombinedOutput()
	logBody, _ := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("脚本执行失败: %v\n脚本输出: %s\n升级日志:\n%s", err, out, logBody)
	}
	if !strings.Contains(string(logBody), "update done") {
		t.Fatalf("日志缺少 'update done':\n%s\n脚本输出: %s", logBody, out)
	}
	if _, serr := os.Stat(nxt); !os.IsNotExist(serr) {
		t.Fatalf(".new 应已改名落位")
	}
	got, rerr := os.ReadFile(cur)
	if rerr != nil {
		t.Fatalf("落位 exe 应可读: %v", rerr)
	}
	if len(got) != len(src) {
		t.Fatalf("落位 exe 大小不符: got %d want %d", len(got), len(src))
	}
	if leftovers, _ := filepath.Glob(cur + ".upgrade-old*"); len(leftovers) > 0 {
		t.Fatalf(".upgrade-old* 残留应被清理: %v", leftovers)
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
