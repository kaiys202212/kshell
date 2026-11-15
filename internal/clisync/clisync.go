// Package clisync 把 kshell 的 agent CLI 与使用技能同步给外部 agent 宿主
//（WorkBuddy / Claude Code / Cursor 等）：释放最小 CLI 二进制到用户 bin 目录、
// 保证 PATH 可达、按宿主列表生成 SKILL.md（目录经 internal/skills 的
// targetRel 映射，新增宿主零改动）。全部步骤幂等（内容哈希门控），可随桌面端
// 每次启动重复执行。
package clisync

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/yangk/kshell/internal/skills"
)

// Options 是同步输入。Binary 为空表示当前构建未内嵌 CLI（dev 构建）。
type Options struct {
	Home    string
	Binary  []byte // 内嵌的最小 CLI 二进制（经根 main.go 的 embed 注入）
	Version string // 桌面端应用版本，写进 SKILL.md 版本行
	// Targets 是技能写入的宿主列表（toolID，经 skills.TargetRoot 解析目录）。
	// 空 = 技能步整体跳过——装到哪些宿主由用户显式决定，绝不擅自扩散。
	Targets []string
	// ConfigurePath 为 true 时执行 PATH 配置（设置页按钮；启动自动路径恒 false
	// ——环境变量变更必须用户显式触发）。
	ConfigurePath bool
	// EnsurePath 注入 PATH 配置实现（测试打桩）；nil 用平台默认。
	EnsurePath func(binDir string) (string, error)
	// PathLookup 注入用户级 PATH 条目读取（Status 查询与体检用；测试打桩）；
	// nil 用平台默认（Windows 注册表 / 其它平台进程 PATH）。
	PathLookup func() []string
}

// SkillStep 是单个宿主的技能安装状态。
type SkillStep struct {
	ToolID string `json:"toolID"`
	Path   string `json:"path"`
	State  string `json:"state"`
	Err    string `json:"err,omitempty"`
}

// Result 是一次同步/状态查询的逐步结果。State 取值：
// 二进制 released|current|not_embedded|present|absent|error；
// PATH added|ready|skipped|missing|error；技能 installed|current|present|absent|error。
type Result struct {
	BinaryPath  string      `json:"binaryPath"`
	BinaryState string      `json:"binaryState"`
	BinaryErr   string      `json:"binaryErr,omitempty"`
	PathState   string      `json:"pathState"`
	PathErr     string      `json:"pathErr,omitempty"`
	Skills      []SkillStep `json:"skills,omitempty"`
	Warnings    []string    `json:"warnings,omitempty"`
}

// 各步骤状态常量。
const (
	stateReleased    = "released"
	stateCurrent     = "current"
	stateNotEmbedded = "not_embedded"
	statePresent     = "present"
	stateAbsent      = "absent"
	stateError       = "error"

	pathAdded   = "added"
	pathReady   = "ready"
	pathSkipped = "skipped"
	pathMissing = "missing"

	skillInstalled = "installed"
)

// syncMu 串行化同步：启动后台 ensure 与设置页按钮可能并发执行。
var syncMu sync.Mutex

// BinaryName 返回释放出的 CLI 文件名。
func BinaryName() string {
	if runtime.GOOS == "windows" {
		return "kshell.exe"
	}
	return "kshell"
}

// BinDir 返回 CLI 释放目录：Windows 用 ~/.kshell/bin（配合注册表 PATH），
// 其它平台用 ~/.local/bin（多数发行版默认在 PATH）。
func BinDir(home string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(home, ".kshell", "bin")
	}
	return filepath.Join(home, ".local", "bin")
}

// skillPathFor 返回某宿主技能目录下本技能的 SKILL.md 路径。目录解析完全
// 委托 skills.TargetRoot——宿主目录知识只存在一处（internal/skills/targetRel）。
func skillPathFor(home, toolID string) (string, bool) {
	root, ok := skills.TargetRoot(toolID, home)
	if !ok {
		return "", false
	}
	return filepath.Join(root, "kshell", "SKILL.md"), true
}

// KnownHostIDs 返回支持技能安装的宿主列表（透传 internal/skills 的稳定顺序）。
func KnownHostIDs() []string { return skills.KnownToolIDs() }

// ExistingSkillTargets 扫描已装过 kshell 技能的宿主（启动自愈口径：只刷新
// 装过的，不扩散新目标）。
func ExistingSkillTargets(home string) []string {
	var out []string
	for _, id := range KnownHostIDs() {
		if p, ok := skillPathFor(home, id); ok && fileExists(p) {
			out = append(out, id)
		}
	}
	return out
}

// Sync 执行完整同步：释放二进制 + （可选）配置 PATH + 按宿主列表安装技能。
// 单宿主失败记入对应 SkillStep.Err，不中断其余步骤；仅 Home 为空时返回顶层错误。
func Sync(opts Options) (Result, error) {
	if opts.Home == "" {
		return Result{}, fmt.Errorf("clisync: home 为空")
	}
	syncMu.Lock()
	defer syncMu.Unlock()

	res := Result{}
	ensure := opts.EnsurePath
	if ensure == nil {
		ensure = ensurePath
	}

	// 1. 释放二进制（全局一次，所有宿主共用同一份 CLI）。
	binDir := BinDir(opts.Home)
	res.BinaryPath = filepath.Join(binDir, BinaryName())
	state, err := releaseBinary(binDir, opts.Binary)
	res.BinaryState = state
	if err != nil {
		res.BinaryErr = err.Error()
	}

	// 2. PATH（仅显式请求且有内嵌二进制时；dev 构建没有 CLI，配 PATH 只会
	// 指向空目录，跳过）。
	if opts.ConfigurePath && len(opts.Binary) > 0 {
		pstate, perr := ensure(binDir)
		res.PathState = pstate
		if perr != nil {
			res.PathErr = perr.Error()
		} else {
			// PATH 体检：只提示旧副本，不调整顺序。
			res.Warnings = auditWarnings(binDir, opts.pathLookup())
		}
	} else {
		res.PathState = pathSkipped
	}

	// 3. 技能文档：按宿主列表逐个安装，单宿主失败不拖垮其余。
	for _, toolID := range opts.Targets {
		step := SkillStep{ToolID: toolID}
		p, ok := skillPathFor(opts.Home, toolID)
		if !ok {
			step.State = stateError
			step.Err = "err.hostsync.unknown_host|" + toolID
			res.Skills = append(res.Skills, step)
			continue
		}
		step.Path = p
		step.State, step.Err = installSkillFor(opts.Home, toolID, opts.Version)
		res.Skills = append(res.Skills, step)
	}
	return res, nil
}

// Status 只读查询当前同步状态，不触发任何写入；Skills 覆盖全部已知宿主
//（前端按检测到的宿主筛选展示）。
func Status(opts Options) Result {
	res := Result{}
	binDir := BinDir(opts.Home)
	res.BinaryPath = filepath.Join(binDir, BinaryName())
	if len(opts.Binary) == 0 {
		res.BinaryState = stateNotEmbedded
	} else if fileExists(res.BinaryPath) {
		res.BinaryState = statePresent
	} else {
		res.BinaryState = stateAbsent
	}

	for _, toolID := range KnownHostIDs() {
		step := SkillStep{ToolID: toolID}
		if p, ok := skillPathFor(opts.Home, toolID); ok {
			step.Path = p
			if fileExists(p) {
				step.State = statePresent
			} else {
				step.State = stateAbsent
			}
		} else {
			step.State = stateAbsent
		}
		res.Skills = append(res.Skills, step)
	}

	res.PathState = pathStatusWith(opts.pathLookup(), binDir)
	return res
}

// pathLookup 归一 PATH 条目读取入口：注入优先，缺省平台实现。
func (o Options) pathLookup() func() []string {
	if o.PathLookup != nil {
		return o.PathLookup
	}
	return readUserPathEntries
}

// ensurePath 是平台 PATH 配置的分发入口（详见 path_windows.go / path_unix.go）。
func ensurePath(binDir string) (string, error) {
	if runtime.GOOS == "windows" {
		return ensurePathWindows(binDir)
	}
	return ensurePathUnix(binDir, currentPathEnv(), userRCFiles())
}

// auditWarnings 返回 PATH 体检警告：排在释放目录之前、且目录内存在 kshell
// 可执行文件的条目（旧副本可能抢先命中）。只提示，不调整顺序。
func auditWarnings(binDir string, lookup func() []string) []string {
	stale := auditEntries(binDir, lookup())
	if len(stale) == 0 {
		return nil
	}
	out := make([]string, 0, len(stale))
	for _, d := range stale {
		// wire key 形态：前端经 translateBackend 渲染成用户语言。
		out = append(out, fmt.Sprintf("err.hostsync.stale_copy|%s", d))
	}
	return out
}

// fileExists 报告路径存在（文件或目录）。
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// sha256Hex 计算内容哈希（释放幂等门控）。
func sha256Hex(b []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(b))
}
