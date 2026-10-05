package providers

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const cursorID = "cursor"

// errCursorEmpty 表示头部没有任何真实用户消息（纯工具调用的空壳会话），列表里只会是噪音。
var errCursorEmpty = errors.New("cursor: 会话无实际用户消息")

// Cursor 对应 Cursor 官方 CLI（cursor-agent）。CLI 会话存于
// ~/.cursor/projects/<工作区slug>/agent-transcripts/<uuid>/<uuid>.jsonl，
// 每行 {"role":...,"message":{"content":[...]}}，与 Claude 格式相近但**没有**
// sessionId / timestamp / cwd 字段，ID 从路径取、时间取文件 mtime、工作区从 slug 逆解码。
type Cursor struct{}

func (Cursor) ID() string          { return cursorID }
func (Cursor) DisplayName() string { return "Cursor" }

func (Cursor) DetectSpec(home string) DetectSpec {
	// 官方安装脚本把二进制放在这些目录，且往往不写进当前进程的 PATH。
	dirs := []string{"~/.local/bin"}
	if runtime.GOOS == "windows" {
		root := `%LOCALAPPDATA%\cursor-agent`
		dirs = []string{root}
		// 根目录 shim 被卸掉后，versions\<ver>\ 里往往还留着可执行文件。
		dirs = append(dirs, cursorVersionInstallDirs(expandHome(root, home))...)
	}
	return DetectSpec{
		BinName: "cursor-agent",
		// 2026-10 起官方安装脚本把根目录 shim 改名为 agent.{cmd,ps1}，
		// 根目录与 versions\ 下都不再有 cursor-agent.* 文件。
		AltBinNames: []string{"agent"},
		InstallDirs: dirs,
		ConfigDirs:  []string{"~/.cursor"},
		// 官方更新器会周期性删光全部入口 shim（甚至更新中断时连 index.js
		// 都删），只留 versions\<ver>\node.exe + index.js 本体：兜底走 node 入口。
		NodeEntryScript: "index.js",
	}
}

// cursorVersionInstallDirs 列出官方安装根下 versions 子目录，**最新版本在前**。
// 目录名形如 YYYY.MM.DD-commit 或 YYYY.MM.DD-HH-MM-SS-commit；必须按数值解析后
// 降序排列——月份不补零（2026.9 vs 2026.10）时字典序会跨月错序。
func cursorVersionInstallDirs(root string) []string {
	entries, err := os.ReadDir(filepath.Join(root, "versions"))
	if err != nil {
		return nil
	}
	type versioned struct {
		path string
		ver  [6]int // year month day hour min sec，逐段数值比较即时间先后
		name string
	}
	var valid []versioned
	var invalid []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// 兼容 YYYY.MM.DD-commit 与带时分秒的 YYYY.MM.DD-HH-MM-SS-commit。
		if !strings.Contains(name, ".") || !strings.Contains(name, "-") {
			continue
		}
		ver, ok := parseCursorVersionDir(name)
		if !ok {
			invalid = append(invalid, name)
			continue
		}
		valid = append(valid, versioned{path: filepath.Join(root, "versions", name), ver: ver, name: name})
	}
	sort.SliceStable(valid, func(i, j int) bool {
		for k := 0; k < 6; k++ {
			if valid[i].ver[k] != valid[j].ver[k] {
				return valid[i].ver[k] > valid[j].ver[k]
			}
		}
		return valid[i].name > valid[j].name // 同版本号取名字大的（commit 哈希仅作稳定排序）
	})
	sort.Sort(sort.Reverse(sort.StringSlice(invalid)))
	out := make([]string, 0, len(valid)+len(invalid))
	for _, v := range valid {
		out = append(out, v.path)
	}
	return append(out, invalid...)
}

// parseCursorVersionDir 解析 versions 目录名为 6 段数值版本号。
// 缺时分秒时对应段补 0；任一段非数字视为非法。
func parseCursorVersionDir(name string) ([6]int, bool) {
	var ver [6]int
	segs := strings.Split(name, "-")
	if len(segs) < 2 || len(segs) > 4 {
		return ver, false
	}
	date := strings.Split(segs[0], ".")
	if len(date) != 3 {
		return ver, false
	}
	nums := append([]string{}, date...)
	if len(segs) == 4 {
		nums = append(nums, segs[1], segs[2], segs[3])
	}
	for i, s := range nums {
		n, err := strconv.Atoi(s)
		if err != nil {
			return ver, false
		}
		ver[i] = n
	}
	return ver, true
}

func (Cursor) SessionRoots(home string) []string { return nil }

func (Cursor) EnumerateSessions(home, _ string) ([]Session, error) {
	sessions, err := listCursorChatSessions(home)
	if err != nil {
		return nil, err
	}
	if len(sessions) > 0 {
		for i := range sessions {
			enrichCursorSession(home, &sessions[i])
		}
		return sessions, nil
	}
	return enumerateCursorTranscriptFallback(home)
}

func (Cursor) SessionFilePattern() string { return "*.jsonl" }

// MatchSessionRel 按目录深度过滤：projects/<slug>/agent-transcripts/<uuid>/<uuid>.jsonl
// 才是会话文件，同 slug 下的 terminals/ 等其它产物不能拿去 resume。
func (Cursor) MatchSessionRel(rel string) bool {
	rel = filepath.ToSlash(rel)
	rel = strings.TrimPrefix(rel, "./")
	parts := strings.Split(rel, "/")
	return len(parts) == 4 && parts[1] == "agent-transcripts" && strings.HasSuffix(rel, ".jsonl")
}

func (Cursor) ParseSession(path string, head []byte) (*Session, error) {
	// 记录里没有 sessionId，uuid 只能从 <uuid>/<uuid>.jsonl 的目录段取
	id := filepath.Base(filepath.Dir(path))
	title := ""
	for _, line := range strings.Split(string(head), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue // 坏行跳过
		}
		if role, _ := rec["role"].(string); role == "user" {
			// 与 Claude/Codex 一样走 cleanTitle：Cursor 用户消息常带
			// <timestamp>Sunday, ...</timestamp> 与 <user_query> 包装，
			// 只剥 user_query 前缀会把星期几当成页签标题。
			if text := cleanTitle(messageText(rec["message"])); text != "" {
				title = oneLine(text, 80)
				break
			}
		}
	}
	if title == "" {
		return nil, errCursorEmpty
	}

	// 无时间戳记录，文件 mtime 是唯一可靠的时间来源（两者同源）
	var updated time.Time
	if st, err := os.Stat(path); err == nil {
		updated = st.ModTime()
	}

	messages := 0
	if n, err := CountLines(path); err == nil {
		messages = n
	}

	slug := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(path))))
	return &Session{
		ID:        id,
		ToolID:    cursorID,
		Workspace: Cursor{}.SlugToWorkspace(slug),
		Title:     title,
		CreatedAt: updated,
		UpdatedAt: updated,
		Messages:  messages,
		Path:      path,
	}, nil
}

func (Cursor) NewSessionCmd(ws string, bin string) Launch {
	return Launch{Path: bin, Dir: ws}
}

// ResumeCmd 走实测确认过的 --resume <chatId>；启动目录取 slug 逆解码的工作区。
// slug 逆解码是有损的（路径自带 '-' 会被拆错），解出不存在的目录时宁可放弃启动
// 目录（launcher 会回退到当前目录），也不能让 exec 的 chdir 直接报错。
func (Cursor) ResumeCmd(s Session, bin string) Launch {
	dir := s.Workspace
	if dir != "" {
		if _, err := os.Stat(dir); err != nil {
			dir = ""
		}
	}
	return Launch{Path: bin, Args: []string{"--resume", s.ID}, Dir: dir}
}

// WorkspaceToSlug 复刻 Cursor CLI 的规则：盘符冒号直接删除，路径分隔符替换成 '-'
// （d:\data\ws → d-data-ws；注意与 Claude 的 D--data-... 不同）。
func (Cursor) WorkspaceToSlug(ws string) string {
	r := strings.NewReplacer(":", "", `\`, "-", "/", "-")
	return r.Replace(ws)
}

// SlugToWorkspace 是 WorkspaceToSlug 的逆运算，仅用于展示与 resume 启动目录的
// 尽力而为，**是有损的**：路径里自带的 '-' 会被误还原成分隔符。Cursor 的会话
// 记录里没有 cwd 字段，没有更权威的来源可用。
func (Cursor) SlugToWorkspace(slug string) string {
	if slug == "" {
		return ""
	}
	// 首段是单字符盘符（d-data-... → d:\data\...）
	if i := strings.Index(slug, "-"); i == 1 && len(slug) > 2 {
		return slug[0:1] + `:\` + strings.ReplaceAll(slug[2:], "-", `\`)
	}
	return strings.ReplaceAll(slug, "-", string(filepath.Separator))
}

func (Cursor) InstallRecipe() InstallRecipe {
	if runtime.GOOS == "windows" {
		return InstallRecipe{
			// 官方文档：irm 'https://cursor.com/install?win32=true' | iex
			// cursor.com/install.ps1 会 500，不能再用。
			InstallCmd: `irm 'https://cursor.com/install?win32=true' | iex`,
			Shell:      "powershell",
		}
	}
	return InstallRecipe{
		InstallCmd: "curl https://cursor.com/install -fsS | bash",
		Shell:      "sh",
	}
}
