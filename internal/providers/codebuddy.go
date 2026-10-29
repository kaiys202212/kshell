package providers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
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
	// codeBuddyAuthRun 是可替换的实探函数（测试打桩），生产为 codeBuddyACPUserId。
	codeBuddyAuthRun = codeBuddyACPUserId
)

// ProbeAuth 判定 CodeBuddy 登录态。破例起子进程（auth.go 约定只读静态文件，
// 与 Trae 同一先例）：CodeBuddy 的登录态没有稳定可读文件——凭据在加密的
// security/ 存储里，磁盘形态与登录态无关联（实测）。走 ACP 的官方扩展方法
// `_codebuddy.ai/getUserInfo`（2.172.1 源码与实机验证）：已登录返回
// result.userInfo.userId（非空），未登录 result 为空对象；注意 initialize 的
// authMethods 是硬编码恒广播的（源码实测），不能作为登录态判据。任何拿不准的
// 失败一律 AuthUnknown 保守放行。
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

// probeCodeBuddyAuth 是可测核心：run 返回登录用户 ID。
//   - 出错（无 bin / 超时 / 进程异常 / 旧版本无扩展方法）→ AuthUnknown；
//   - userId 非空 → AuthIn；
//   - userId 为空（getUserInfo 响应无 userInfo）→ AuthOut。
func probeCodeBuddyAuth(run func() (string, error)) AuthStatus {
	userID, err := run()
	if err != nil {
		return AuthUnknown
	}
	if userID != "" {
		return AuthIn
	}
	return AuthOut
}

// codeBuddyLineID 解析一行输出里的 JSON-RPC id（请求回调、其他响应、坏 JSON
// 返回 nil），用于探测的两步握手触发判定。
type codeBuddyLineID struct {
	ID *int `json:"id"`
}

// codeBuddyUserInfoResponse 是 `_codebuddy.ai/getUserInfo` 响应的解析目标。
// 只解 userId 一个字段：响应里还带 accessToken，绝不整包保留/落日志（探测
// 结果只有登录态三态，敏感凭据不进内存以外的任何地方）。
type codeBuddyUserInfoResponse struct {
	ID     *int `json:"id"`
	Result struct {
		UserInfo struct {
			UserID string `json:"userId"`
		} `json:"userInfo"`
	} `json:"result"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// parseCodeBuddyUserId 解析一行 ndJsonStream 输出：
//   - 是 id=2 的 getUserInfo 响应 → (userId, ok=true, errResp=false)；
//     userId 非空=已登录，空=未登录（未登录时 result 序列化为空对象）；
//   - 是 id=2 的 error 响应（如旧版本无此扩展方法返回 -32601）→ ok=false、
//     errResp=true（上层归一 AuthUnknown）；
//   - 其余行（initialize 响应、agent 反向请求、坏 JSON）→ 双 false，跳过。
func parseCodeBuddyUserId(line string) (userID string, ok bool, errResp bool) {
	var r codeBuddyUserInfoResponse
	if err := json.Unmarshal([]byte(line), &r); err != nil {
		return "", false, false
	}
	if r.ID == nil || *r.ID != 2 {
		return "", false, false
	}
	if r.Error != nil {
		return "", false, true
	}
	return r.Result.UserInfo.UserID, true, false
}

// codeBuddyInitJSON 是探测第一步的 initialize 请求（协议常量，与 chat 握手
// 解耦：探测只到 initialize + getUserInfo 为止，绝不下发 session/new）。
const codeBuddyInitJSON = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{"fs":{"readTextFile":true,"writeTextFile":false}}}}`

// codeBuddyUserInfoJSON 是探测第二步：官方扩展方法 _codebuddy.ai/getUserInfo
//（2.172.1 实机验证，无需会话即可调用）。
const codeBuddyUserInfoJSON = `{"jsonrpc":"2.0","id":2,"method":"_codebuddy.ai/getUserInfo"}`

// codeBuddyACPUserId 起 `codebuddy --acp` 子进程做两步探测：initialize（id=1）
// → 收到响应后发 `_codebuddy.ai/getUserInfo`（id=2）→ 收到响应即杀进程，
// 返回登录用户 ID（空串=未登录）。bin 解析复用 FindBins：Windows npm 安装
// 命中的是 codebuddy.cmd（.ps1 不在 PATHEXT，LookPath 不会返回），CreateProcess
// 会经 cmd.exe 包装执行它——node 真身是孙代，收尾必须树杀（见下方 treeKill）。
func codeBuddyACPUserId() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errNoCodeBuddyBin
	}
	bins := FindBins(CodeBuddy{}.DetectSpec(home), home)
	if len(bins) == 0 {
		return "", errNoCodeBuddyBin
	}
	// ctx 只做 deadline 判据（保留 context.DeadlineExceeded 错误语义）；
	// 绝不用 exec.CommandContext 执行超时杀——它只杀直属的 cmd.exe 包装，
	// node 成孤儿（2026-10-25 实测堆积 88 个 ~140MB 孤儿 node）。杀进程
	// 统一走树杀：timer 兜底超时，defer 兜底正常/异常路径。
	ctx, cancel := context.WithTimeout(context.Background(), codeBuddyAuthProbeTimeout)
	defer cancel()
	// Background ctx 无 Done：只借统一入口做 shim 解析 / 隐藏黑窗 / 批处理包装，
	// 超时仍由下方 timer + 树杀负责，避免 CommandContext 只杀 cmd.exe 留下孤儿 node。
	cmd := executil.NewCLICommand(context.Background(), bins[0], []string{"--acp"})
	executil.PrepareProcessGroup(cmd) // 独立进程组：超时/收尾树杀连带清理孙 node
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
	// sync.Once 串行化 timer 与 defer 的并发收尸：killTimer.Stop 不等待已在
	// 执行的回调，两个 goroutine 同时对同一 PID 跑树杀虽幂等，仍以防万一。
	var treeKillOnce sync.Once
	treeKill := func() { treeKillOnce.Do(func() { executil.KillProcessTree(cmd) }) }
	killTimer := time.AfterFunc(codeBuddyAuthProbeTimeout, treeKill)
	// 统一收尾：拿到响应或超时/EOF 都要杀整棵进程树，不留孤儿 node 进程。
	defer func() {
		killTimer.Stop()
		treeKill()
		_ = cmd.Wait()
	}()
	if _, err := stdin.Write([]byte(codeBuddyInitJSON + "\n")); err != nil {
		return "", err
	}
	sentUserInfo := false
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		line := sc.Text()
		if !sentUserInfo {
			// 第一步：等 initialize 的 id=1 响应（收到即发第二步请求）。
			var probe codeBuddyLineID
			if json.Unmarshal([]byte(line), &probe) == nil && probe.ID != nil && *probe.ID == 1 {
				sentUserInfo = true
				_, _ = stdin.Write([]byte(codeBuddyUserInfoJSON + "\n"))
			}
			continue
		}
		// 第二步：等 getUserInfo 的 id=2 响应。error 响应（旧版本无扩展方法）
		// 归一为错误，上层 AuthUnknown 保守放行。
		userID, ok, errResp := parseCodeBuddyUserId(line)
		if errResp {
			return "", errors.New("codebuddy: getUserInfo 返回错误")
		}
		if ok {
			_ = stdin.Close()
			return userID, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// stdout 关闭却始终没有期望响应：形态异常按 unknown 处理。
	return "", errors.New("codebuddy: 探测无响应")
}
