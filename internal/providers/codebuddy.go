package providers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/yangk/kshell/internal/executil"
)

const codeBuddyID = "codebuddy"

// CodeBuddy 对应腾讯云 CodeBuddy Code CLI。会话与 Generic 已实测模板同构，
// 解析委托 Generic，避免两套字段路径。
type CodeBuddy struct{}

func (CodeBuddy) ID() string          { return codeBuddyID }
func (CodeBuddy) DisplayName() string { return "CodeBuddy" }

func (CodeBuddy) wrap(home string) Generic {
	return Generic{Spec: codeBuddySpec(), Home: home}
}

func codeBuddySpec() GenericSpec {
	var s GenericSpec
	s.ID = codeBuddyID
	s.Name = "CodeBuddy"
	s.Detect.Command = "codebuddy"
	s.Detect.Dirs = []string{"~/.codebuddy"}
	s.Sessions.Glob = "~/.codebuddy/projects/*/*.jsonl"
	s.Sessions.Format = "jsonl"
	s.Fields.CWD = "cwd"
	s.Fields.ID = "sessionId"
	s.Fields.Timestamp = "timestamp"
	s.Fields.Title = "summary"
	s.Fields.TitleFallbacks = []string{"aiTitle"}
	s.Resume.Args = []string{"--resume", "{id}"}
	s.Verified = true
	return s
}

func (CodeBuddy) DetectSpec(home string) DetectSpec {
	return DetectSpec{
		BinName:     "codebuddy",
		AltBinNames: []string{"cbc"},
		ConfigDirs:  []string{"~/.codebuddy"},
	}
}

func (c CodeBuddy) SessionRoots(home string) []string {
	return c.wrap(home).SessionRoots(home)
}

func (c CodeBuddy) SessionFilePattern() string {
	return c.wrap("").SessionFilePattern()
}

func (c CodeBuddy) MatchSessionRel(rel string) bool {
	return c.wrap("").MatchSessionRel(rel)
}

func (c CodeBuddy) ParseSession(path string, head []byte) (*Session, error) {
	return c.wrap("").ParseSession(path, head)
}

func (c CodeBuddy) NewSessionCmd(ws, bin string) Launch {
	return c.wrap("").NewSessionCmd(ws, bin)
}

func (c CodeBuddy) ResumeCmd(s Session, bin string) Launch {
	return c.wrap("").ResumeCmd(s, bin)
}

// RemoteNewArgs / RemoteResumeArgs：CodeBuddy 无官方远程协议，ssh 远端执行 CLI。
func (CodeBuddy) RemoteNewArgs(string) []string { return nil }
func (CodeBuddy) RemoteResumeArgs(s Session) []string {
	return []string{"--resume", s.ID}
}

// ACPAdapter 声明 CodeBuddy 的 ACP 入口：CLI 自带 --acp（stdio ndJsonStream），
// 适配器与 CLI 同二进制，故无 npx 兜底。
func (CodeBuddy) ACPAdapter() ACPAdapter {
	return ACPAdapter{
		BinNames:  []string{"codebuddy", "cbc"},
		ExtraArgs: []string{"--acp"},
	}
}

func (CodeBuddy) InstallRecipe() InstallRecipe {
	r := npmInstall("@tencent-ai/codebuddy-code")
	r.PurgeDirs = []string{"~/.codebuddy"}
	return r
}

// ---- 登录态探测与登录命令 ----

// CodeBuddy 实现 AuthProber / LoginCmdProvider（编译期断言）。
var _ AuthProber = CodeBuddy{}
var _ LoginCmdProvider = CodeBuddy{}

// LoginCmd 返回裸 codebuddy：CodeBuddy 无官方 login 子命令（2.172.1 实测），
// 交互式 TUI 首次启动即进入登录引导，登录弹窗据此拉起终端触发登录流程。
func (CodeBuddy) LoginCmd() []string { return []string{"codebuddy"} }

var errNoCodeBuddyBin = errors.New("codebuddy: 未找到可执行文件")

// codeBuddyAuthProbeTimeout 限制单次探测时长：node CLI 冷启动实测 ~2.4s，
// 卡死不能拖住扫描与登录弹窗轮询。
const codeBuddyAuthProbeTimeout = 10 * time.Second

// codeBuddyAuthCacheTTL 是探测结果缓存时长。CheckToolAuth（登录弹窗 2s 轮询）
// 与 DetectAll 都会调 ProbeAuth，每次都拉子进程不可接受；登录后状态翻转的
// 可感知延迟为 TTL+轮询间隔，仍属「点刷新即校验」的预期节奏。
const codeBuddyAuthCacheTTL = 5 * time.Second

// codeBuddyAuthCacheEntry 是带过期时刻的探测缓存（包级共享：CodeBuddy 是无状态值）。
type codeBuddyAuthCacheEntry struct {
	status  AuthStatus
	expires time.Time
}

var (
	codeBuddyAuthMu    sync.Mutex
	codeBuddyAuthCache codeBuddyAuthCacheEntry
	// codeBuddyAuthRun 是可替换的实探函数（测试打桩），生产为 codeBuddyACPInitialize。
	codeBuddyAuthRun = codeBuddyACPInitialize
)

// ProbeAuth 判定 CodeBuddy 登录态。破例起子进程（auth.go 约定只读静态文件，
// 与 Trae 同一先例）：CodeBuddy 的登录态没有稳定可读文件——凭据在加密的
// security/ 存储里，未登录时目录形态与已登录无法区分（实测）；ACP initialize
// 是官方协议入口：未登录时广播 authMethods（本机实测 iOA/external/internal/
// selfhosted 四种），已登录不广播。任何拿不准的失败一律 AuthUnknown 保守放行。
func (CodeBuddy) ProbeAuth() AuthStatus {
	codeBuddyAuthMu.Lock()
	if !codeBuddyAuthCache.expires.IsZero() && time.Now().Before(codeBuddyAuthCache.expires) {
		st := codeBuddyAuthCache.status
		codeBuddyAuthMu.Unlock()
		return st
	}
	codeBuddyAuthMu.Unlock()
	st := probeCodeBuddyAuth(codeBuddyAuthRun)
	codeBuddyAuthMu.Lock()
	codeBuddyAuthCache = codeBuddyAuthCacheEntry{status: st, expires: time.Now().Add(codeBuddyAuthCacheTTL)}
	codeBuddyAuthMu.Unlock()
	return st
}

// probeCodeBuddyAuth 是可测核心：run 返回一行 stdout 输出。
//   - 出错（无 bin / 超时 / 进程异常）→ AuthUnknown；
//   - id=1 响应且 authMethods 非空 → AuthOut；
//   - id=1 响应且 authMethods 空/缺省 → AuthIn；
//   - 输出形态异常（非 id=1 响应）→ AuthUnknown。
func probeCodeBuddyAuth(run func() (string, error)) AuthStatus {
	line, err := run()
	if err != nil {
		return AuthUnknown
	}
	n, ok := parseCodeBuddyAuthMethods(line)
	if !ok {
		return AuthUnknown
	}
	if n > 0 {
		return AuthOut
	}
	return AuthIn
}

// codeBuddyInitResponse 是 initialize 响应的解析目标：只关心 id 与 authMethods。
type codeBuddyInitResponse struct {
	ID     *int `json:"id"`
	Result struct {
		AuthMethods []struct {
			ID string `json:"id"`
		} `json:"authMethods"`
	} `json:"result"`
}

// parseCodeBuddyAuthMethods 解析一行 ndJsonStream 输出：是 id=1 的 initialize
// 响应时返回 authMethods 数量与 ok=true；其余行（agent 反向请求、其他响应、
// 坏 JSON）返回 ok=false。
func parseCodeBuddyAuthMethods(line string) (int, bool) {
	var r codeBuddyInitResponse
	if err := json.Unmarshal([]byte(line), &r); err != nil {
		return 0, false
	}
	if r.ID == nil || *r.ID != 1 {
		return 0, false
	}
	return len(r.Result.AuthMethods), true
}

// codeBuddyInitJSON 是探测用的 initialize 请求（协议常量，与 chat 握手解耦：
// 探测只到 initialize 为止，绝不下发 session/new）。
const codeBuddyInitJSON = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{"fs":{"readTextFile":true,"writeTextFile":false}}}}`

// codeBuddyACPInitialize 起 `codebuddy --acp` 子进程，stdin 写一条 initialize，
// 逐行读 stdout 直到 id=1 的响应（拿到即杀进程返回）。bin 解析复用 FindBins：
// Windows npm 安装命中的是 codebuddy.cmd（.ps1 不在 PATHEXT，LookPath 不会
// 返回），CreateProcess 可直接执行 .cmd。
func codeBuddyACPInitialize() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errNoCodeBuddyBin
	}
	bins := FindBins(CodeBuddy{}.DetectSpec(home), home)
	if len(bins) == 0 {
		return "", errNoCodeBuddyBin
	}
	ctx, cancel := context.WithTimeout(context.Background(), codeBuddyAuthProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bins[0], "--acp")
	executil.HideWindow(cmd) // 扫描与轮询时避免黑窗闪烁
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	// 统一收尾：拿到响应或超时/EOF 都要杀进程收尸，不留孤儿 node 进程。
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	if _, err := stdin.Write([]byte(codeBuddyInitJSON + "\n")); err != nil {
		return "", err
	}
	_ = stdin.Close()
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		line := sc.Text()
		if _, ok := parseCodeBuddyAuthMethods(line); ok {
			return line, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// stdout 关闭却始终没有 id=1 响应：形态异常按 unknown 处理。
	return "", errors.New("codebuddy: initialize 无响应")
}
