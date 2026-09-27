package providers

import (
	"bytes"
	"io"
	"os"
	"strings"
	"time"
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
	InstallDirs []string // 常见安装目录（支持 ~ 前缀）
	ConfigDirs  []string // 存在即说明装过（支持 ~ 前缀）
}

type Session struct {
	ID         string
	ToolID     string
	Workspace  string
	Title      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Messages   int
	Path       string
	ResumeArgs []string
}

// Launch 描述一次进程启动：会话恢复与新建会话都用它，交给 launcher 统一处理（含 Windows 的 .ps1 shim）。
type Launch struct {
	Path string
	Args []string
	Dir  string
}

type Provider interface {
	ID() string
	DisplayName() string
	DetectSpec(home string) DetectSpec
	SessionRoots(home string) []string
	SessionFilePattern() string // 会话文件后缀：Claude/Codex 是 *.jsonl，Gemini 是 *.json
	ParseSession(path string, head []byte) (*Session, error)
	NewSessionCmd(ws string, bin string, ctx []string) Launch
	ResumeCmd(s Session, bin string) Launch
}

// ReadHead 只读文件头部。会话 JSONL 动辄几十 MB，解析元信息绝不能整文件读入。
func ReadHead(path string, limit int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	buf := make([]byte, 0, limit)
	chunk := make([]byte, min(limit, 64*1024))
	for len(buf) < limit {
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

// ContextPrompt 把上下文篮里的文件拼成初始提示，新建会话时注入。
func ContextPrompt(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("请先阅读以下文件，再开始工作：")
	for _, p := range paths {
		b.WriteString("\n- " + p)
	}
	return b.String()
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

	switch content := msg["content"].(type) {
	case string:
		return content
	case []any:
		var parts []string
		for _, item := range content {
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
