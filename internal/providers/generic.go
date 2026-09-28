package providers

import (
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
// D 类工具（CodeBuddy、OpenCode、Cline 等）的会话目录与命令参数各家不同且可能变化，
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
			if text := strings.TrimSpace(contentText(rec["content"])); text != "" && !isWrapperText(text) {
				firstAssistantText = text
			}
		}
		if title == "" {
			title = oneLine(g.titleFrom(rec), 80)
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
		text := strings.TrimSpace(contentText(rec.Content))
		if text == "" || isWrapperText(text) {
			continue
		}
		return text
	}
	return ""
}

// isWrapperText 识别 CLI 注入的包装文本（<system-reminder>、<command-*>、
// <local-command-*>、<manually_attached_skills>、<environment_context> 等），
// 它们都不是用户手写的内容，不适合当会话标题。
func isWrapperText(s string) bool {
	return strings.HasPrefix(s, "<")
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

func (g Generic) NewSessionCmd(ws string, bin string, ctx []string) Launch {
	launch := Launch{Path: bin, Dir: ws}
	if prompt := ContextPrompt(ctx); prompt != "" {
		launch.Args = []string{prompt}
	}
	return launch
}

func (g Generic) ResumeCmd(s Session, bin string) Launch {
	args := make([]string, 0, len(g.Spec.Resume.Args))
	for _, a := range g.Spec.Resume.Args {
		args = append(args, strings.ReplaceAll(a, "{id}", s.ID))
	}
	return Launch{Path: bin, Args: args, Dir: s.Workspace}
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

// DefaultProvidersYAML 是随程序分发的默认模板：预置常见工具的猜测值，全部标 verified: false。
func DefaultProvidersYAML() string {
	return `# kshell 自定义工具定义
# 用法：把下面某项的路径/参数改成你机器上的实际值即可，无需改代码。
# glob 支持 ~/ 前缀；fields 支持 a.b.c 形式的字段路径；resume 参数里的 {id} 会被替换成会话 ID。
# verified: false 表示未经实测，kshell 会在界面上标注。
providers:
  - id: codebuddy
    name: CodeBuddy
    detect:
      command: codebuddy
      dirs:
        - ~/.codebuddy
    sessions:
      # 只收一层：projects/<工作区>/<sessionId>.jsonl。
      # 更深的 subagents/*.jsonl 是子代理记录，其 ID 无法 --resume。
      glob: ~/.codebuddy/projects/*/*.jsonl
      format: jsonl
    fields:
      cwd: cwd
      id: sessionId
      timestamp: timestamp            # 毫秒数字或 RFC3339 均可
      title: summary                  # summary 记录（首条用户消息摘要）
      titleFallbacks: ["aiTitle"]     # 其次 AI 生成的会话标题；都没有再用首条真实用户消息
    resume:
      args: ["--resume", "{id}"]
    verified: true

  - id: opencode
    name: OpenCode
    detect:
      command: opencode
      dirs:
        - ~/.local/share/opencode
    sessions:
      glob: ~/.local/share/opencode/storage/*/*.json
      format: json
    fields:
      cwd: cwd
      id: id
      timestamp: time.updated
      title: title
    resume:
      args: ["--session", "{id}"]
    verified: false

  - id: cline
    name: Cline / Roo
    detect:
      command: cline
      dirs:
        - ~/.cline
    sessions:
      glob: ~/.cline/tasks/*/*.json
      format: json
    fields:
      cwd: cwd
      id: id
      timestamp: ts
      title: task
    resume:
      args: ["--task", "{id}"]
    verified: false
`
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
