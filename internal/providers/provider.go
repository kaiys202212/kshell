package providers

import (
	"bytes"
	"io"
	"os"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/appearance"
)

// Detection 描述某个 agent 工具在本机的安装情况。
type Detection struct {
	Installed bool
	BinPath   string
	Source    string // path | install-dir | config-dir
}

// DetectSpec 让各 provider 声明「怎么找到自己」，检测逻辑则由 providers.Detect 统一实现，避免每家重复写一遍。
type DetectSpec struct {
	BinName     string   // PATH 上查找的可执行名
	AltBinNames []string // 备选可执行名：只在 InstallDirs 内匹配，不进 PATH 探测（防误命中同名无关文件）
	InstallDirs []string // 常见安装目录（支持 ~ 前缀与环境变量）
	ConfigDirs  []string // 存在即说明装过（支持 ~ 前缀）
}

type Session struct {
	ID        string
	ToolID    string
	Workspace string
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
	Messages  int
	Path      string
}

// Launch 描述一次进程启动：会话恢复与新建会话都用它，交给 launcher 统一处理（含 Windows 的 .ps1 shim）。
type Launch struct {
	Path string
	Args []string
	Dir  string
	// Env 是额外环境变量；nil 表示仅继承当前进程环境，非空时会叠加在父环境之上。
	Env map[string]string
}

type Provider interface {
	ID() string
	DisplayName() string
	DetectSpec(home string) DetectSpec
	SessionRoots(home string) []string
	SessionFilePattern() string // 会话文件后缀：Claude/Codex 是 *.jsonl，Gemini 是 *.json
	ParseSession(path string, head []byte) (*Session, error)
	NewSessionCmd(ws string, bin string) Launch
	ResumeCmd(s Session, bin string) Launch
}

// PathMatcher 可选接口：实现后扫描器会用「相对会话根的路径」做精确过滤，
// 用于排除 glob 覆盖不到的深层文件（如 CodeBuddy 的 subagents/*.jsonl）。
type PathMatcher interface {
	MatchSessionRel(rel string) bool // rel 为相对会话根的斜杠分隔路径
}

// SessionEnumerator 可选接口：给「会话不在文件里」的工具用（如 opencode 把会话放进 SQLite 库），
// 扫描器会解析出它的 CLI 路径后直接调用，绕过文件遍历与解析缓存。
type SessionEnumerator interface {
	EnumerateSessions(home string, bin string) ([]Session, error)
}

// Themer 可选接口：provider 声明如何让自身 TUI 跟随浅/深主题。
// args 追加到启动参数；env 合并进子进程环境；cacheDir 供需要落临时配置的工具使用
// （写入 kshell 自己的缓存目录，绝不修改工具自身的用户配置）。
type Themer interface {
	ThemeOverrides(theme appearance.Theme, cacheDir string) (args []string, env map[string]string)
}

// ReadHead 只读文件头部。会话 JSONL 动辄几十 MB，解析元信息绝不能整文件读入。
func ReadHead(path string, limit int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	buf := make([]byte, 0, limit)
	chunkCap := 64 * 1024
	if limit < chunkCap {
		chunkCap = limit
	}
	chunk := make([]byte, chunkCap)
	for len(buf) < limit {
		want := limit - len(buf)
		if want < len(chunk) {
			chunk = chunk[:want]
		}
		n, err := f.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
		}
		if err == io.EOF {
			return buf, nil
		}
		if err != nil {
			return buf, err
		}
	}
	return buf, nil
}

// CountLines 只数换行符，用来估算消息条数——比整文件 JSON 反序列化便宜几个数量级。
func CountLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	buf := make([]byte, 256*1024)
	count := 0
	for {
		n, err := f.Read(buf)
		count += bytes.Count(buf[:n], []byte{'\n'})
		if err == io.EOF {
			if n > 0 && buf[n-1] != '\n' {
				count++ // 末行没有换行符也要算一条
			}
			return count, nil
		}
		if err != nil {
			return count, err
		}
	}
}

// messageText 兼容 content 为字符串或 [{type:"text",text:"..."}] 两种形态。
func messageText(v any) string {
	msg, ok := v.(map[string]any)
	if !ok {
		if s, ok := v.(string); ok {
			return s
		}
		return ""
	}
	return contentText(msg["content"])
}

// contentText 提取消息 content 的纯文本，兼容字符串与块数组两种形态。
func contentText(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		var parts []string
		for _, item := range c {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := block["text"].(string); ok {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

// oneLine 把标题压成单行并截断，避免列表里出现换行或超长文本。
func oneLine(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "…"
}
