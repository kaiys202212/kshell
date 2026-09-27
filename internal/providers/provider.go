package providers

import (
	"io"
	"os"
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
