// Package clisync 把 kshell 的 agent CLI 与使用技能同步给外部 agent 宿主
// （WorkBuddy / CodeBuddy 内核）：释放最小 CLI 二进制到用户 bin 目录、保证
// PATH 可达、生成 SKILL.md 到 ~/.codebuddy/skills/kshell/。全部步骤幂等
//（内容哈希门控），可随桌面端每次启动重复执行。
package clisync

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// Options 是同步输入。Binary 为空表示当前构建未内嵌 CLI（dev 构建）。
type Options struct {
	Home    string
	Binary  []byte // 内嵌的最小 CLI 二进制（经根 main.go 的 embed 注入）
	Version string // 桌面端应用版本，写进 SKILL.md 版本行
	// ConfigurePath 为 true 时执行 PATH 配置（设置页按钮；启动自动路径恒 false
	// ——环境变量变更必须用户显式触发）。
	ConfigurePath bool
	// EnsurePath 注入 PATH 配置实现（测试打桩）；nil 用平台默认。
	EnsurePath func(binDir string) (string, error)
	// PathLookup 注入用户级 PATH 条目读取（Status 查询与体检用；测试打桩）；
	// nil 用平台默认（Windows 注册表 / 其它平台进程 PATH）。
	PathLookup func() []string
}

// Result 是一次同步/状态查询的逐步结果。State 取值：
// 二进制 released|current|not_embedded|present|absent|error；
// PATH added|ready|skipped|missing|error；技能 installed|current|present|absent|error。
type Result struct {
	BinaryPath  string `json:"binaryPath"`
	BinaryState string `json:"binaryState"`
	BinaryErr   string `json:"binaryErr,omitempty"`
	PathState   string `json:"pathState"`
	PathErr     string `json:"pathErr,omitempty"`
	SkillPath   string `json:"skillPath"`
	SkillState  string `json:"skillState"`
	SkillErr    string `json:"skillErr,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
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

// skillDir 返回 WorkBuddy（CodeBuddy 内核）用户级技能目录下本技能的位置。
// 目录口径与 internal/skills 的 targetRel["codebuddy"] 一致。
func skillDir(home string) string {
	return filepath.Join(home, ".codebuddy", "skills", "kshell")
}

// Sync 执行完整同步：释放二进制 + （可选）配置 PATH + 安装技能。单步失败
// 记入对应字段的 Err/State，不中断其余步骤；仅 Home 为空时返回顶层错误。
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

	// 1. 释放二进制。
	binDir := BinDir(opts.Home)
	res.BinaryPath = filepath.Join(binDir, BinaryName())
	state, err := releaseBinary(binDir, opts.Binary)
	res.BinaryState = state
	if err != nil {
		res.BinaryErr = err.Error()
	}

	// 2. PATH（仅显式请求时）。
	if opts.ConfigurePath {
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

	// 3. 技能文档。
	res.SkillPath = filepath.Join(skillDir(opts.Home), "SKILL.md")
	sstate, serr := installSkill(opts.Home, opts.Version)
	res.SkillState = sstate
	if serr != nil {
		res.SkillErr = serr.Error()
	}
	return res, nil
}

// Status 只读查询当前同步状态，不触发任何写入。
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

	res.SkillPath = filepath.Join(skillDir(opts.Home), "SKILL.md")
	if fileExists(res.SkillPath) {
		res.SkillState = statePresent
	} else {
		res.SkillState = stateAbsent
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
		out = append(out, fmt.Sprintf("PATH 中发现其它 kshell 副本（可能抢先命中）: %s", d))
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
