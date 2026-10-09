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

	if err := json.Unmarshal(head, &rec); err != nil {
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
