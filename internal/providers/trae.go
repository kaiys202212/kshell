package providers

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/executil"
)

const traeID = "trae"

// Trae 对应 TraeCode CLI 2.0（traecli，字节跳动）。闭源 codex-rs fork，会话是
// Codex rollout 同构格式，解析委托 parseCodexRollout。仅支持 2.0：1.x（CoCo）的
// events.jsonl 格式完全不同，不读。官方文档 https://docs.trae.cn/cli_* 。
// 0.211.1 已实测：安装（%LOCALAPPDATA%\Programs\TraeCLI\bin）、--permission-mode、
// resume 子命令、acp serve 握手、login status / login。
// 待企业账号实测：真实会话 rollout 落盘、ACP 全流程对话；仍缓：Themer、通知 hook。
type Trae struct{}

// Trae 实现 LoginCmdProvider（编译期断言）：登录弹窗据此起官方交互式登录终端。
var _ LoginCmdProvider = Trae{}

// LoginCmd 返回官方交互式登录命令。traecli login 走企业浏览器 SSO；
// PAT 场景（TRAECLI_PERSONAL_ACCESS_TOKEN）也覆盖，由 CLI 自己引导。
func (Trae) LoginCmd() []string { return []string{"traecli", "login"} }

// Trae 实现 AuthProber（编译期断言）。
var _ AuthProber = Trae{}

// traeAuthTimeout 限制 login status 单次探测耗时：CLI 卡死不能拖住扫描与登录弹窗轮询。
const traeAuthTimeout = 3 * time.Second

var errNoTraeBin = errors.New("trae: 未找到 traecli 可执行文件")

// ProbeAuth 用官方 `traecli login status` 子命令判定登录态。破例起子进程
//（auth.go 的约定是只读静态配置文件）：TRAE 登录态没有稳定可读文件——
// 0.211.1 未登录时 ~/.trae/cli 下无 auth 产物，登录后文件形态未实测，
// 而 login status 是官方子命令（未登录：exit 非 0 + "Not logged in"，实测）。
// 探测点只有 DetectAll 组装与登录弹窗轮询（CheckToolAuth），单次 ~0.5s 可接受；
// 任何拿不准的失败一律 AuthUnknown 保守放行（宁可多跑一次失败的会话）。
func (Trae) ProbeAuth() AuthStatus {
	return probeTraeAuth(traeLoginStatus)
}

// probeTraeAuth 是可测核心：
//   - exit 0 且输出不含「not logged in」→ AuthIn；
//   - 非 0 退出且输出含「not logged in」（大小写不敏感）→ AuthOut（0.211.1 实测）；
//   - 其余（找不到 bin、超时、输出形态异常）→ AuthUnknown。
func probeTraeAuth(run func() (string, error)) AuthStatus {
	out, err := run()
	loggedOut := strings.Contains(strings.ToLower(out), "not logged in")
	switch {
	case err == nil && !loggedOut:
		return AuthIn
	case err != nil && loggedOut:
		return AuthOut
	default:
		return AuthUnknown
	}
}

// traeLoginStatus 运行 traecli login status，返回合并输出与进程错误。
// bin 解析复用 FindBins 口径：PATH 命中优先，落空再扫 InstallDirs 候选
//（官方安装器只写用户 PATH，已运行进程可能持旧值）。
func traeLoginStatus() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errNoTraeBin
	}
	bins := FindBins(Trae{}.DetectSpec(home), home)
	if len(bins) == 0 {
		return "", errNoTraeBin
	}
	ctx, cancel := context.WithTimeout(context.Background(), traeAuthTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bins[0], "login", "status")
	executil.HideWindow(cmd) // 桌面端扫描时避免黑窗闪烁
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (Trae) ID() string          { return traeID }
func (Trae) DisplayName() string { return "TRAE CLI" }

func (Trae) DetectSpec(home string) DetectSpec {
	spec := DetectSpec{
		BinName:     "traecli",
		AltBinNames: []string{"traex"}, // 内部名，官方安装器与 traecli.cmd 同目录落盘（0.211.1 实测）
		ConfigDirs:  []string{"~/.trae"},
		// 自动修复的残留信号：会话数据（history.jsonl 不算，用户可能从未开过会话）。
		ResidueDirs: []string{"~/.trae/cli/sessions"},
	}
	if runtime.GOOS == "windows" {
		// 官方安装器固定落盘 %LOCALAPPDATA%\Programs\TraeCLI\bin（写用户 PATH，
		// 但已运行进程的 PATH 是旧值，Detect 兜底须直接看安装目录）。
		spec.InstallDirs = []string{`%LOCALAPPDATA%\Programs\TraeCLI\bin`}
	} else {
		spec.InstallDirs = []string{"~/.local/bin"}
	}
	return spec
}

// SessionRoots 只给 sessions 子树：archived_sessions / history.jsonl 在根外，
// 压缩产物 *.jsonl.zst 不匹配 *.jsonl，均天然排除。
func (Trae) SessionRoots(home string) []string {
	return []string{filepath.Join(home, ".trae", "cli", "sessions")}
}

func (Trae) SessionFilePattern() string { return "*.jsonl" }

// MatchSessionRel 限定 YYYY/MM/DD/<file>.jsonl 四段日期结构（sessions 树里
// 其它层次的 jsonl 不收录）。不强制 rollout- 前缀：TRAE 未实测，若前缀与
// codex 有出入，过严会让全部会话静默不可见；杂散文件缺 session_meta 时
// 解析失败被安全跳过，宽松更稳。
func (Trae) MatchSessionRel(rel string) bool {
	rel = filepath.ToSlash(rel)
	rel = strings.TrimPrefix(rel, "./")
	parts := strings.Split(rel, "/")
	return len(parts) == 4 && strings.HasSuffix(parts[3], ".jsonl")
}

func (Trae) ParseSession(path string, head []byte) (*Session, error) {
	return parseCodexRollout(path, head, traeID)
}

func (Trae) NewSessionCmd(ws, bin string) Launch {
	return Launch{Path: bin, Dir: ws}
}

// ResumeCmd 走官方 `traecli resume <SESSION_ID>` 子命令（同 codex 风格）。
// 不用 `--resume <ID>` 空格形态：官方论坛实测空格分隔会把 ID 当初始提示词
// 发出（forum.trae.cn/t/topic/115573），等号写法才可靠，子命令形态最稳。
func (Trae) ResumeCmd(s Session, bin string) Launch {
	return Launch{Path: bin, Args: []string{"resume", s.ID}, Dir: s.Workspace}
}

// RemoteNewArgs / RemoteResumeArgs：TRAE 无官方远程协议，ssh 远端执行 CLI。
func (Trae) RemoteNewArgs(string) []string { return nil }
func (Trae) RemoteResumeArgs(s Session) []string {
	return []string{"resume", s.ID}
}

// ACPAdapter 声明 TRAE 的 ACP 入口：CLI 自带 acp serve 子命令（stdio）。
// 注意是两层子命令：`traecli acp` 只打 usage，真正的 stdio server 是
// `traecli acp serve`（0.211.1 实测握手通过；未登录时 session/new 返回
// -32000 Authentication required，kshell 侧按普通 ACP 错误呈现）。
func (Trae) ACPAdapter() ACPAdapter {
	return ACPAdapter{
		BinNames:  []string{"traecli", "traex"},
		ExtraArgs: []string{"acp", "serve"},
	}
}

// InjectPermission 见 permission.go（与各工具集中放置）。

// InstallRecipe 用官方安装脚本（非 npm）。PurgeDirs 只清 CLI 数据子树
// ~/.trae/cli：~/.trae 下可能有 TRAE IDE 共享内容，整体清掉太狠。
func (Trae) InstallRecipe() InstallRecipe {
	if runtime.GOOS == "windows" {
		return InstallRecipe{
			InstallCmd: "irm https://trae.cn/trae-cli/install_v2.ps1 | iex",
			Shell:      "powershell",
			PurgeDirs:  []string{"~/.trae/cli"},
		}
	}
	return InstallRecipe{
		InstallCmd: `sh -c "$(curl -fsSL https://trae.cn/trae-cli/install_v2.sh)"`,
		Shell:      "sh",
		PurgeDirs:  []string{"~/.trae/cli"},
	}
}
