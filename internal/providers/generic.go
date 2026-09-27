package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// GenericSpec 是 ~/.kshell/providers.yaml 里的一项声明。
// D 类工具（CodeBuddy、OpenCode、Cline 等）的会话目录与命令参数各家不同且可能变化，
// 与其硬编码猜测，不如让用户改一行 yaml 生效。
type GenericSpec struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
	Detect struct {
		Command string   `yaml:"command"`
		Dirs    []string `yaml:"dirs"`
	} `yaml:"detect"`
	Sessions struct {
		Glob   string `yaml:"glob"`
		Format string `yaml:"format"` // jsonl | json
	} `yaml:"sessions"`
	Fields struct {
		CWD       string `yaml:"cwd"`
		ID        string `yaml:"id"`
		Timestamp string `yaml:"timestamp"`
		Title     string `yaml:"title"`
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
		Title:     oneLine(stringField(rec, g.Spec.Fields.Title), 80),
		CreatedAt: timeField(rec, g.Spec.Fields.Timestamp),
		UpdatedAt: timeField(rec, g.Spec.Fields.Timestamp),
		Messages:  1,
		Path:      path,
	}, nil
}

func (g Generic) parseJSONL(path string, head []byte) (*Session, error) {
	var (
		id, cwd, title string
		created, updated time.Time
		haveTime        bool
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
		if title == "" {
			title = oneLine(stringField(rec, g.Spec.Fields.Title), 80)
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

	messages := 0
	if n, err := CountLines(path); err == nil {
		messages = n
	}
	return &Session{ID: id, ToolID: g.ID(), Workspace: cwd, Title: title,
		CreatedAt: created, UpdatedAt: updated, Messages: messages, Path: path}, nil
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
      glob: ~/.codebuddy/projects/*/*.jsonl
      format: jsonl
    fields:
      cwd: cwd
      id: sessionId
      timestamp: timestamp
      title: message.content
    resume:
      args: ["--resume", "{id}"]
    verified: false

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
	value, ok := lookup(rec, path).(string)
	if !ok {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t
	}
	return time.Time{}
}

func lookup(rec map[string]any, path string) any {
	parts := strings.Split(path, ".")
	var current any = rec
	for _, p := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = m[p]
	}
	return current
}
