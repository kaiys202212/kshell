package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/appearance"
	"github.com/yangk/kshell/internal/executil"
)

const (
	opencodeID = "opencode"
	// opencodeQueryTimeout：opencode 是 Node CLI，冷启动约 1s，留足余量但不拖垮扫描。
	opencodeQueryTimeout = 20 * time.Second
	// opencodeSessionsSQL 只取根会话：parent_id 非空的是子代理记录，没法用 --session 恢复。
	//
	// 必须写成**单行**：Windows 上 opencode 是 npm 的 .cmd 包装脚本，命令行最终由 cmd.exe
	// 重新解析一次，参数里夹带换行会被截断（实测多行 SQL 报 `no such column: id`，
	// opencode 的会话因此一条都查不出来）——换行只影响可读性，绝不要为了排版拆行。
	opencodeSessionsSQL = `select id, directory as cwd, title, time_created as created, time_updated as updated from session where parent_id is null and time_archived is null order by time_updated desc`
)

var errOpencodeNotFileBased = errors.New("opencode: 会话存放在 SQLite 库中，请走 EnumerateSessions")

// Opencode 实现 ACPProvider / LoginCmdProvider / AuthProber（编译期断言）。
var (
	_ ACPProvider      = Opencode{}
	_ LoginCmdProvider = Opencode{}
	_ AuthProber       = Opencode{}
)

// ACPAdapter 声明 opencode 的 ACP 入口：CLI 自带 acp 子命令（stdio ndjson）。
func (Opencode) ACPAdapter() ACPAdapter {
	return ACPAdapter{BinNames: []string{"opencode"}, NPMPackage: "opencode-ai", ExtraArgs: []string{"acp"}}
}

// LoginCmd 返回官方交互式登录命令；desktop 登录弹窗复用探测到的可执行入口。
func (Opencode) LoginCmd() []string { return []string{"opencode", "auth", "login"} }

// ProbeAuth 读取 opencode 的 auth.json：至少一个 provider 有非空凭据才算已登录。
// auth.json 只由 `opencode auth login` 写入；用户也可能把凭据直接配在
// ~/.config/opencode/opencode.json 的 provider.*.options.apiKey 里而不生成该文件，
// 所以文件缺失不能判为未登录——一律 AuthUnknown 保守放行，只有明确读到
// 空凭据（文件在、能解析、但没有有效值）才判 Out。
func (Opencode) ProbeAuth() AuthStatus {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return AuthUnknown
	}
	return probeOpencodeAuth(home)
}

// probeOpencodeAuth 抽出 home 参数便于表驱动测试。缺文件、读失败、JSON 损坏、
// 顶层 null 都是 Unknown（错误方向必须偏保守，不能错杀已登录用户）；
// 仅当文件存在且解析成功、却没有任何 provider 带非空凭据时才判 Out。
func probeOpencodeAuth(home string) AuthStatus {
	data, err := os.ReadFile(filepath.Join(opencodeDataDir(home), "auth.json"))
	if err != nil {
		return AuthUnknown
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return AuthUnknown
	}
	if top == nil {
		return AuthUnknown
	}
	for _, raw := range top {
		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err != nil {
			continue
		}
		for key, val := range obj {
			// type 只是方式标记（api/oauth），不算凭据本身。
			if key == "type" {
				continue
			}
			if s, ok := val.(string); ok && strings.TrimSpace(s) != "" {
				return AuthIn
			}
		}
	}
	return AuthOut
}

// Opencode 对应 opencode CLI。
// 会话发现不走文件：新版 opencode 已把 JSON 会话迁移到 ~/.local/share/opencode/opencode.db，
// 旧的文件目录里只剩 session_diff 垃圾，所以这里实现 SessionEnumerator，
// 由扫描器直接调 `opencode db <sql> --format json` 读库。
type Opencode struct{}

func (Opencode) ID() string          { return opencodeID }
func (Opencode) DisplayName() string { return "OpenCode" }

func (Opencode) DetectSpec(home string) DetectSpec {
	return DetectSpec{
		BinName:    opencodeID,
		ConfigDirs: []string{"~/.local/share/opencode"},
	}
}

// SessionRoots 返回 nil：没有可遍历的会话目录，交给 EnumerateSessions。
func (Opencode) SessionRoots(home string) []string { return nil }

func (Opencode) SessionFilePattern() string { return "" }

func (Opencode) ParseSession(path string, head []byte) (*Session, error) {
	return nil, errOpencodeNotFileBased
}

func (Opencode) NewSessionCmd(ws string, bin string) Launch {
	return Launch{Path: bin, Dir: ws}
}

// ResumeCmd 用 `opencode --session <id>` 恢复（本机 opencode 1.18 实测可用）。
func (Opencode) ResumeCmd(s Session, bin string) Launch {
	return Launch{Path: bin, Args: []string{"--session", s.ID}, Dir: s.Workspace}
}

// RemoteNewArgs / RemoteResumeArgs：OpenCode 无官方远程协议，ssh 远端执行 CLI。
func (Opencode) RemoteNewArgs(string) []string { return nil }
func (Opencode) RemoteResumeArgs(s Session) []string {
	return []string{"--session", s.ID}
}

// ThemeOverrides 指向生成的 tui.json，注入与 kshell 明暗一致的固定主题。
// 不用 system：system 会向终端 OSC 查色，应答经 xterm onData 回写后会被当成首条用户消息，
// 会话标题变成 "4;0;rgb:…" 一类残片（Gemini 同理关掉了 autoThemeSwitching）。
func (Opencode) ThemeOverrides(theme appearance.Theme, cacheDir string) ([]string, map[string]string) {
	if cacheDir == "" {
		return nil, nil
	}
	name := "opencode"
	if theme == appearance.ThemeLight {
		name = "catppuccin-latte"
	}
	path, err := writeThemeJSON(cacheDir, "opencode-"+string(theme)+".json", map[string]any{"theme": name})
	if err != nil {
		return nil, nil
	}
	return nil, map[string]string{"OPENCODE_TUI_CONFIG": path}
}

// OpencodeSessionsSQL 返回枚举根会话的单行 SQL（本地/远端共用，勿拆行）。
func OpencodeSessionsSQL() string { return opencodeSessionsSQL }

// ParseOpencodeSessionsJSON 解析 `opencode db … --format json` 的 stdout。
func ParseOpencodeSessionsJSON(raw []byte, dbPath string) ([]Session, error) {
	return parseOpencodeSessions(raw, dbPath)
}

// EnumerateSessions 查询 opencode 的 SQLite 会话表。
// bin 为空（只检测到配置目录、CLI 不在 PATH）时静默跳过：这不算扫描失败。
func (Opencode) EnumerateSessions(home, bin string) ([]Session, error) {
	if strings.TrimSpace(bin) == "" {
		return nil, nil
	}
	raw, err := runOpencodeQuery(bin, opencodeSessionsSQL)
	if err != nil {
		return nil, err
	}
	return parseOpencodeSessions(raw, opencodeDBPath(home))
}

// runOpencodeQuery 执行查询并返回 stdout（抽成变量便于测试注入）。
var runOpencodeQuery = func(bin, query string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), opencodeQueryTimeout)
	defer cancel()

	// 走统一入口：Windows 上 .cmd 需要 %COMSPEC% /S /C 包装，
	// 否则 `C:\Program Files\...\opencode.cmd` 会被截成 `C:\Program`。
	cmd := executil.NewCLICommand(ctx, bin, []string{"db", query, "--format", "json"})
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			detail := strings.TrimSpace(string(exitErr.Stderr))
			if idx := strings.IndexByte(detail, '\n'); idx >= 0 {
				detail = detail[:idx]
			}
			return nil, fmt.Errorf("opencode db 退出码 %d: %s", exitErr.ExitCode(), detail)
		}
		return nil, err
	}
	return out, nil
}

// opencodeRow 对应 `--format json` 的一行，键名即 SQL 列名（时间字段是毫秒 epoch）。
type opencodeRow struct {
	ID      string `json:"id"`
	CWD     string `json:"cwd"`
	Title   string `json:"title"`
	Created int64  `json:"created"`
	Updated int64  `json:"updated"`
}

func parseOpencodeSessions(raw []byte, dbPath string) ([]Session, error) {
	var rows []opencodeRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("解析 opencode 会话 JSON 失败: %w", err)
	}

	out := make([]Session, 0, len(rows))
	for _, r := range rows {
		// 没有工作区的会话无处可去（无法恢复、也无法归入某个项目），直接跳过。
		if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.CWD) == "" {
			continue
		}
		created := time.UnixMilli(r.Created)
		updated := time.UnixMilli(r.Updated)
		if r.Updated == 0 {
			updated = created
		}
		out = append(out, Session{
			ID:        r.ID,
			ToolID:    opencodeID,
			Workspace: canonicalCWD(r.CWD),
			Title:     oneLine(cleanTitle(r.Title), 80), // 丢弃 OSC 查色残片等不适合展示的标题
			CreatedAt: created,
			UpdatedAt: updated,
			Path:      dbPath,
		})
	}
	return out, nil
}

// canonicalCWD 把 opencode 库里的目录归一到平台写法。
// opencode 在 Windows 上存的是 `D:/data/ws`（前斜杠），而其它工具的会话里是 `D:\data\ws`：
// 同一目录出现两种写法时，工作区聚合键相同但 Path 不同，
// 按原样比较的界面（会话列表）就会漏掉 opencode 的会话。
func canonicalCWD(cwd string) string {
	return filepath.Clean(strings.TrimSpace(cwd))
}

// opencodeDataDir 是 opencode 的数据目录（会话库与 auth.json 同放此处）。
func opencodeDataDir(home string) string {
	return filepath.Join(home, ".local", "share", "opencode")
}

func opencodeDBPath(home string) string {
	return filepath.Join(opencodeDataDir(home), "opencode.db")
}

func (Opencode) InstallRecipe() InstallRecipe {
	r := npmInstall("opencode-ai")
	r.PurgeDirs = []string{"~/.config/opencode", "~/.local/share/opencode"}
	return r
}
