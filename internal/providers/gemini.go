package providers

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"time"

	"github.com/yangk/kshell/internal/appearance"
)

const geminiID = "gemini"

var errGeminiNoSessionID = errors.New("gemini: 会话文件中未找到 sessionId")

// decodeTruncated 解析「可能被截断的单 JSON head」：整段失败时用
// tolerantClose 找最后一个安全元素边界并补齐括号。sessionId/标题/工作区
// 都在头部，截断丢弃的只是尾部 message 内容——好会话不再被记成 failed。
func decodeTruncated(data []byte, v any) error {
	if err := json.Unmarshal(data, v); err == nil {
		return nil
	}
	fixed, ok := tolerantClose(data)
	if !ok {
		return errors.New("gemini: head 无可解析的完整 JSON")
	}
	return json.Unmarshal(fixed, v)
}

// tolerantClose 扫描 JSON 的括号栈与字符串状态，回退到最后一个「元素边界」
//（{ [ 之后、} ] 之后、逗号之前），丢弃残缺尾部并按栈逆序补齐闭合括号。
// 返回 ok=false 表示结构损坏或本就完整（后者不该发生：调用方先整段试过）。
func tolerantClose(data []byte) ([]byte, bool) {
	var stack []byte
	inString, esc := false, false
	lastSafe := -1
	for i, c := range data {
		if inString {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{', '[':
			stack = append(stack, c)
			lastSafe = i + 1 // 容器刚开始，{} / [] 即合法
		case '}', ']':
			want := byte('}')
			if c == ']' {
				want = '['
			}
			if len(stack) == 0 || stack[len(stack)-1] != want {
				return nil, false
			}
			stack = stack[:len(stack)-1]
			lastSafe = i + 1
		case ',':
			lastSafe = i // 逗号前是完整 value（尾部逗号非法，取 i 不取 i+1）
		}
	}
	if len(stack) == 0 || lastSafe <= 0 {
		return nil, false
	}
	out := data[:lastSafe]
	for j := len(stack) - 1; j >= 0; j-- {
		if stack[j] == '{' {
			out = append(out, '}')
		} else {
			out = append(out, ']')
		}
	}
	return out, true
}

// Gemini 对应 Gemini CLI。
// 注意：本机未安装 Gemini CLI，下面的会话根目录与 resume 参数均为按已知默认值的推断（未实测）。
// 路径或参数不对时改这里即可，不影响其他 provider。
type Gemini struct{}

func (Gemini) ID() string          { return geminiID }
func (Gemini) DisplayName() string { return "Gemini CLI" }

func (Gemini) DetectSpec(home string) DetectSpec {
	return DetectSpec{
		BinName:    geminiID,
		ConfigDirs: []string{"~/.gemini"},
	}
}

func (Gemini) SessionRoots(home string) []string {
	return []string{filepath.Join(home, ".gemini", "tmp")}
}

func (Gemini) ParseSession(path string, head []byte) (*Session, error) {
	var rec struct {
		SessionID  string    `json:"sessionId"`
		ID         string    `json:"id"`
		CWD        string    `json:"cwd"`
		Project    string    `json:"project"`
		Start      time.Time `json:"startTime"`
		LastUpdate time.Time `json:"lastUpdated"`
		Messages   []struct {
			Type    string `json:"type"`
			Content string `json:"content"`
		} `json:"messages"`
	}

	// head 对超大会话是截断的（残缺尾部）；截断在顶层对象内部时整段 JSON
	// 无法解析，好会话会被记成 failed。二分回退找「最小可解析前缀」：
	// Decode 成功的前缀具单调性（完整 JSON + 垃圾尾部仍成功），
	// sessionId/标题在头部，回退不伤解析目标。
	if err := decodeTruncated(head, &rec); err != nil {
		return nil, err
	}

	id := rec.SessionID
	if id == "" {
		id = rec.ID
	}
	if id == "" {
		return nil, errGeminiNoSessionID
	}

	ws := rec.CWD
	if ws == "" {
		ws = rec.Project
	}

	title := ""
	for _, m := range rec.Messages {
		if m.Type == "user" && m.Content != "" {
			title = oneLine(m.Content, 80)
			break
		}
	}

	created := rec.Start
	updated := rec.LastUpdate
	if updated.IsZero() {
		updated = created
	}

	return &Session{
		ID:        id,
		ToolID:    geminiID,
		Workspace: ws,
		Title:     title,
		CreatedAt: created,
		UpdatedAt: updated,
		Messages:  len(rec.Messages),
		Path:      path,
	}, nil
}

func (Gemini) NewSessionCmd(ws string, bin string) Launch {
	return Launch{Path: bin, Dir: ws}
}

// ResumeCmd 的 --resume 参数未经实测（本机未安装 Gemini CLI），需在有该工具的机器上校准。
func (Gemini) ResumeCmd(s Session, bin string) Launch {
	return Launch{Path: bin, Args: []string{"--resume", s.ID}, Dir: s.Workspace}
}

// RemoteNewArgs / RemoteResumeArgs：Gemini 无官方远程协议，ssh 远端执行 CLI。
func (Gemini) RemoteNewArgs(string) []string { return nil }
func (Gemini) RemoteResumeArgs(s Session) []string {
	return []string{"--resume", s.ID}
}

func (Gemini) SessionFilePattern() string { return "*.json" }

// ThemeOverrides 指向生成的系统级 settings（最高优先级层），并关闭自动主题轮询。
func (Gemini) ThemeOverrides(theme appearance.Theme, cacheDir string) ([]string, map[string]string) {
	if cacheDir == "" {
		return nil, nil
	}
	name := "Default Light"
	if theme == appearance.ThemeDark {
		name = "Default"
	}
	path, err := writeThemeJSON(cacheDir, "gemini-"+string(theme)+".json", map[string]any{
		"ui": map[string]any{"theme": name, "autoThemeSwitching": false},
	})
	if err != nil {
		return nil, nil
	}
	return nil, map[string]string{"GEMINI_CLI_SYSTEM_SETTINGS_PATH": path}
}

func (Gemini) InstallRecipe() InstallRecipe {
	r := npmInstall("@google/gemini-cli")
	r.PurgeDirs = []string{"~/.gemini"}
	return r
}
