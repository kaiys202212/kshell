package providers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// GenericSpec 是 ~/.kshell/providers.yaml 里的一项声明。
// D 类工具（Cline 等尚未内置的 CLI）的会话目录与命令参数各家不同且可能变化，
// 与其硬编码猜测，不如让用户改一行 yaml 生效。
type GenericSpec struct {
	ID     string `yaml:"id"`
	Name   string `yaml:"name"`
	Detect struct {
		Command string   `yaml:"command"`
		Dirs    []string `yaml:"dirs"`
	} `yaml:"detect"`
	Sessions struct {
		Glob   string `yaml:"glob"`
		Format string `yaml:"format"` // jsonl | json
	} `yaml:"sessions"`
	Fields struct {
		CWD            string   `yaml:"cwd"`
		ID             string   `yaml:"id"`
		Timestamp      string   `yaml:"timestamp"`
		Title          string   `yaml:"title"`
		TitleFallbacks []string `yaml:"titleFallbacks"` // title 取不到时依次尝试的字段
	} `yaml:"fields"`
	Resume struct {
		Args []string `yaml:"args"`
	} `yaml:"resume"`
	Permission struct {
		BypassArgs []string `yaml:"bypassArgs"` // Bypass 模式注入的参数（置于 provider 参数之前，全局 flag 形态）
	} `yaml:"permission,omitempty"`
	Verified bool `yaml:"verified"` // false 表示路径/参数未实测，UI 会标注
}

type genericSpecFile struct {
	Providers []GenericSpec `yaml:"providers"`
}

var errGenericNoID = errors.New("generic: 会话文件中未找到 id 字段")
var errGenericEmpty = errors.New("generic: 会话无实际内容（仅命令/系统记录）")

// Generic 按 GenericSpec 生成 provider 行为。
type Generic struct {
	Spec GenericSpec
	Home string
}

func (g Generic) ID() string {
	if g.Spec.ID == "" {
		return "generic"
	}
	return g.Spec.ID
}

func (g Generic) DisplayName() string {
	if g.Spec.Name == "" {
		return g.ID()
	}
	return g.Spec.Name
}

func (g Generic) DetectSpec(home string) DetectSpec {
	dirs := g.Spec.Detect.Dirs
	return DetectSpec{BinName: g.Spec.Detect.Command, ConfigDirs: dirs}
}

// SessionRoots 取 glob 中第一个通配符之前的目录作为遍历根。
func (g Generic) SessionRoots(home string) []string {
	glob := g.Spec.Sessions.Glob
	if glob == "" {
		return nil
	}
	root := rootFromGlob(expandTilde(glob, home))
	if root == "" {
		return nil
	}
	return []string{filepath.Clean(root)} // Clean 顺带把分隔符统一成当前系统的
}

func (g Generic) SessionFilePattern() string {
	glob := filepath.ToSlash(g.Spec.Sessions.Glob)
	if i := strings.LastIndex(glob, "/"); i >= 0 {
		return glob[i+1:]
	}
	return glob
}

// MatchSessionRel 按 glob 的目录深度过滤：~/.codebuddy/projects/*/*.jsonl 不匹配
// projects/ws/<sessionId>.jsonl/subagents/agent-x.jsonl 这类更深层的文件，
// 否则 CodeBuddy 的子代理记录会被当成独立会话收录，用它们的 ID 去 resume 必然失败。
func (g Generic) MatchSessionRel(rel string) bool {
	clean := filepath.ToSlash(expandTilde(g.Spec.Sessions.Glob, g.Home))
	root := rootFromGlob(clean)
	if root == "" {
		return true
	}
	relGlob := strings.TrimPrefix(clean[len(root):], "/")
	if relGlob == "" {
		return true // 无通配余量（glob 就是根本身），不过滤
	}
	ok, err := path.Match(relGlob, filepath.ToSlash(rel))
	return err == nil && ok
}

func (g Generic) ParseSession(path string, head []byte) (*Session, error) {
	if strings.EqualFold(g.Spec.Sessions.Format, "json") {
		return g.parseJSON(path, head)
	}
	return g.parseJSONL(path, head)
}

func (g Generic) parseJSON(path string, head []byte) (*Session, error) {
	var rec map[string]any
	if err := json.Unmarshal(head, &rec); err != nil {
		return nil, err
	}
	id := stringField(rec, g.Spec.Fields.ID)
	if id == "" {
		return nil, errGenericNoID
	}
	return &Session{
		ID:        id,
		ToolID:    g.ID(),
		Workspace: stringField(rec, g.Spec.Fields.CWD),
		Title:     oneLine(g.titleFrom(rec), 80),
		CreatedAt: timeField(rec, g.Spec.Fields.Timestamp),
		UpdatedAt: timeField(rec, g.Spec.Fields.Timestamp),
		Messages:  1,
		Path:      path,
	}, nil
}

func (g Generic) parseJSONL(path string, head []byte) (*Session, error) {
	var (
		id, cwd, title     string
		created, updated   time.Time
		haveTime           bool
		firstAssistantText string
	)

	for _, line := range strings.Split(string(head), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue // 坏行跳过
		}
		if id == "" {
			id = stringField(rec, g.Spec.Fields.ID)
		}
		if cwd == "" {
			cwd = stringField(rec, g.Spec.Fields.CWD)
		}
		if firstAssistantText == "" && isRecord(rec, "message", "assistant") {
			if text := cleanTitle(contentText(rec["content"])); text != "" {
				firstAssistantText = text
			}
		}
		if title == "" {
			title = oneLine(cleanTitle(g.titleFrom(rec)), 80)
		}
		if t := timeField(rec, g.Spec.Fields.Timestamp); !t.IsZero() {
			if !haveTime {
				created, updated, haveTime = t, t, true
			}
			if t.Before(created) {
				created = t
			}
			if t.After(updated) {
				updated = t
			}
		}
	}

	if id == "" {
		return nil, errGenericNoID
	}
	// 标题回退链：配置字段 → 首条真实用户消息 → 首条 assistant 消息
	//（全部跳过 <system-reminder>/<command-*>/<local-command-*> 这类包装文本）。
	if title == "" {
		title = oneLine(firstUserText(string(head)), 80)
	}
	if title == "" {
		title = oneLine(firstAssistantText, 80)
	}
	// 连 assistant 消息都没有、用户消息也全是包装记录：纯命令操作留下的空壳
	//（/clear、/config、change session 等），列表里只会是噪音，直接剔除。
	if title == "" && firstAssistantText == "" {
		return nil, errGenericEmpty
	}

	messages := 0
	if n, err := CountLines(path); err == nil {
		messages = n
	}
	return &Session{ID: id, ToolID: g.ID(), Workspace: cwd, Title: title,
		CreatedAt: created, UpdatedAt: updated, Messages: messages, Path: path}, nil
}

// titleFrom 按配置的字段链取标题：fields.title 优先，其次 titleFallbacks。
func (g Generic) titleFrom(rec map[string]any) string {
	for _, field := range append([]string{g.Spec.Fields.Title}, g.Spec.Fields.TitleFallbacks...) {
		if v := stringField(rec, field); v != "" {
			return v
		}
	}
	return ""
}

// firstUserText 找第一条「像人写的」用户消息文本，包装类记录全部跳过。
func firstUserText(head string) string {
	for _, line := range strings.Split(head, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content any    `json:"content"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec.Type != "" && rec.Type != "message" {
			continue
		}
		if rec.Role != "" && rec.Role != "user" {
			continue
		}
		text := cleanTitle(contentText(rec.Content))
		if text == "" {
			continue
		}
		return text
	}
	return ""
}

// isRecord 判断记录的 type/role 是否匹配（空串表示不校验该维度）。
func isRecord(rec map[string]any, typ, role string) bool {
	if t, _ := rec["type"].(string); typ != "" && t != typ {
		return false
	}
	if r, _ := rec["role"].(string); role != "" && r != role {
		return false
	}
	return typ != "" || role != ""
}

func (g Generic) NewSessionCmd(ws string, bin string) Launch {
	return Launch{Path: bin, Dir: ws}
}

func (g Generic) ResumeCmd(s Session, bin string) Launch {
	args := make([]string, 0, len(g.Spec.Resume.Args))
	for _, a := range g.Spec.Resume.Args {
		args = append(args, strings.ReplaceAll(a, "{id}", s.ID))
	}
	return Launch{Path: bin, Args: args, Dir: s.Workspace}
}

// InjectPermission 按 yaml 声明的 permission.bypassArgs 注入（未声明则 no-op）。
func (g Generic) InjectPermission(bypass bool) ([]string, map[string]string) {
	if !bypass || len(g.Spec.Permission.BypassArgs) == 0 {
		return nil, nil
	}
	return append([]string(nil), g.Spec.Permission.BypassArgs...), nil
}

// LoadGenericSpecs 从 yaml 读取自定义 provider 定义。
func LoadGenericSpecs(path string) ([]GenericSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseProvidersYAML(data)
}

// ParseProvidersYAML 解析并校验自定义 provider 定义，供编辑保存前预检。
func ParseProvidersYAML(data []byte) ([]GenericSpec, error) {
	var file genericSpecFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	return file.Providers, nil
}

// FormatProvidersYAML 把自定义 provider 列表编码成可回写的 yaml（不保留注释）。
func FormatProvidersYAML(specs []GenericSpec) (string, error) {
	if specs == nil {
		specs = []GenericSpec{}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(genericSpecFile{Providers: specs}); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// DefaultProvidersYAML 是随程序分发的标准接入模板：逐字段注释说明 + 一个可解析的
// 完整示例（全部标 verified: false）。同一份内容服务三处：文件缺失回填、设置页
// 「下载模板」、首次写入。用户改完在设置页导入即可接入新工具。
func DefaultProvidersYAML() string {
	return `# ============================================================
# kshell 自定义工具接入模板
# ============================================================
# 这是什么：
#   kshell 通过这份文件认识「未内置」的 agent CLI 工具（如 Cline、Aider 等）。
#   填写一个条目 = 接入一个工具，kshell 会自动发现它的会话并支持一键恢复。
#
# 怎么用：
#   1. 把下方示例条目里的路径/参数改成你机器上的实际值（每条 # 注释都写明怎么填）；
#   2. 在 kshell 设置页「自定义工具」区点【导入 yml】选择本文件：
#      与现有定义同 id 的条目会覆盖更新，新 id 会追加，完成后有汇总提示。
#
# 通用约定（所有路径与字段写法都遵守）：
#   - 路径支持 ~/ 前缀（展开为当前用户主目录），目录分隔符 / 与 \ 均可；
#   - fields 里的字段路径支持 a.b.c 形式逐层取值，中途遇到数组会自动取首个非空元素；
#   - resume.args 里的 {id} 占位符会在恢复会话时替换成真实会话 ID；
#   - 时间字段兼容 RFC3339 文本与秒/毫秒级数字时间戳；
#   - verified: false 表示路径/参数未实测，kshell 会在界面上标注，实测通过后可改为 true。
#
# 注意：claude / codex / gemini / cursor / opencode / codebuddy 已是内置工具，
# 不要在这里重复声明；残留的同 ID 条目会被内置实现覆盖。
# ============================================================

providers:
  # —— 完整示例：Cline（真实可解析的条目，照着改值即可）——
  # 接入多个工具时，按同样的缩进再复制一段「- id: ...」条目。
  - id: cline
    # name：界面上显示的工具名，可省略（省略时显示 id）。
    name: Cline / Roo
    # detect：kshell 怎么判断这个工具装没装。
    detect:
      # command：可执行文件名（在 PATH 里即可，如 cline），也可填完整路径。
      command: cline
      # dirs：该工具的配置/数据目录（辅助判断安装痕迹），可省略，可写多个。
      dirs:
        - ~/.cline
    # sessions：会话文件在哪里、长什么样。
    sessions:
      # glob：会话文件匹配模式。* 不跨目录，目录深度决定哪些文件算会话：
      # 更深的文件一律忽略（避免把子代理记录误当成独立会话）。
      glob: ~/.cline/tasks/*/*.json
      # format：会话文件格式，二选一——
      #   jsonl：每行一个 JSON 对象（多数 CLI 工具的会话日志形态）；
      #   json ：整个文件是一个 JSON 对象。
      format: json
    # fields：从会话记录里取关键字段的路径（a.b.c 形式）。
    fields:
      # cwd：会话所属工作区目录，kshell 按它把会话归到对应项目。
      cwd: cwd
      # id：会话 ID 的字段路径，恢复会话时把它的值传给 resume.args 里的 {id}。
      id: id
      # timestamp：时间字段，用于会话列表排序与展示。
      timestamp: ts
      # title：标题字段；取不到时自动回退：首条真实用户消息 → 首条 assistant 消息。
      title: task
      # titleFallbacks：标题回退字段列表，title 取不到时逐个尝试，可省略。
      titleFallbacks: []
    # resume：恢复会话的命令参数。
    resume:
      # args：参数列表，{id} 会被替换成会话 ID；不需要恢复功能时写 []。
      args: ["--task", "{id}"]
    # permission：权限模式为 Bypass 时注入的启动参数（全局 flag 形态，
    # 置于 provider 参数之前）。仅当该工具有「跳过权限确认」类参数时才配置，例如：
    # permission:
    #   bypassArgs: ["--trust-all"]
    # verified：false=未实测（界面标注）；确认可用后改成 true。
    verified: false
`
}

// MergeGenericSpecs 把 incoming 合入 existing：同 ID 原位覆盖（视为更新），新 ID 追加。
// incoming 里 ID 为空（含纯空白）或与同批靠前条目重复的会被跳过。
// 返回合并结果与统计：added=新增数，updated=覆盖数，skipped=无效/重复条目数。
func MergeGenericSpecs(existing, incoming []GenericSpec) (merged []GenericSpec, added, updated, skipped int) {
	merged = append([]GenericSpec(nil), existing...)
	index := make(map[string]int, len(merged)) // id → merged 下标
	for i, s := range merged {
		if s.ID != "" {
			index[s.ID] = i
		}
	}
	seen := make(map[string]bool)
	for _, s := range incoming {
		id := strings.TrimSpace(s.ID)
		if id == "" || seen[id] {
			skipped++
			continue
		}
		seen[id] = true
		s.ID = id
		if i, ok := index[id]; ok {
			merged[i] = s
			updated++
			continue
		}
		index[id] = len(merged)
		merged = append(merged, s)
		added++
	}
	return merged, added, updated, skipped
}

// EnsureProvidersFile 在文件不存在时写入默认模板。
func EnsureProvidersFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(DefaultProvidersYAML()), 0o600)
}

// rootFromGlob 取第一个通配符之前的路径作为遍历根（~/.a/*/x.jsonl → ~/.a）。
func rootFromGlob(glob string) string {
	clean := filepath.ToSlash(glob)
	idx := strings.IndexAny(clean, "*?[")
	if idx < 0 {
		return clean
	}
	return strings.TrimRight(clean[:idx], "/")
}

func expandTilde(path, home string) string {
	if home == "" {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		return filepath.Join(home, path[2:])
	}
	return path
}

// stringField 按 a.b.c 路径取值，并兼容字符串数组（取首个元素）。
func stringField(rec map[string]any, path string) string {
	if path == "" {
		return ""
	}
	value := lookup(rec, path)
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%v", v)
	case bool:
		return fmt.Sprintf("%v", v)
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				return s
			}
		}
	}
	return ""
}

func timeField(rec map[string]any, path string) time.Time {
	if path == "" {
		return time.Time{}
	}
	switch value := lookup(rec, path).(type) {
	case string:
		if t, err := time.Parse(time.RFC3339, value); err == nil {
			return t
		}
		// 有些工具把时间戳写成数字字符串（毫秒或秒）
		if n, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
			return epochTime(n)
		}
	case float64:
		return epochTime(value)
	}
	return time.Time{}
}

// epochTime 兼容毫秒与秒两种精度：>1e12 只能是毫秒级（2001 年之后），否则按秒处理。
func epochTime(n float64) time.Time {
	if n <= 0 {
		return time.Time{}
	}
	if n > 1e12 {
		return time.UnixMilli(int64(n))
	}
	return time.Unix(int64(n), 0)
}

func lookup(rec map[string]any, path string) any {
	parts := strings.Split(path, ".")
	var current any = rec
	for i, p := range parts {
		// 中途遇到数组（如 content: [{type,text},...]）：对每个元素继续走剩余路径，取首个非空值
		if arr, ok := current.([]any); ok {
			rest := strings.Join(parts[i:], ".")
			for _, item := range arr {
				if m, ok := item.(map[string]any); ok {
					if v := lookup(m, rest); nonEmptyLookup(v) {
						return v
					}
				}
			}
			return nil
		}
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = m[p]
	}
	return current
}

func nonEmptyLookup(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case string:
		return t != ""
	case []any:
		return len(t) > 0
	default:
		return true
	}
}
