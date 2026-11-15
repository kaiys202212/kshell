package agentcli

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/yangk/kshell/internal/config"
)

// Endpoint 是连接发现文件的内存形态。
type Endpoint struct {
	URL     string `json:"url"`
	Token   string `json:"token"`
	PID     int    `json:"pid"`
	Version int    `json:"version"`
	// App 是桌面端应用版本（可选字段；旧桌面端不写，缺省空串 = 跳过版本比对）。
	App string `json:"app"`
}

// endpointVersion 必须与 agentsrv 的写入版本一致。
const endpointVersion = 1

// DefaultEndpointPath 返回连接发现文件路径（~/.kshell/agentapi/endpoint.json）。
// 配置目录不可用时回落 temp（与 desktop 写入口径对齐）。
func DefaultEndpointPath() string {
	paths, err := config.Paths()
	if err == nil && paths.Root != "" {
		return paths.Root + "/agentapi/endpoint.json"
	}
	return filepath.Join(os.TempDir(), "kshell-agentapi-endpoint.json")
}

// LoadEndpoint 读取并校验发现文件：不存在/版本不符视为桌面端未运行。
func LoadEndpoint(path string) (Endpoint, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Endpoint{}, ErrNotReachable
		}
		return Endpoint{}, err
	}
	var ep Endpoint
	if err := json.Unmarshal(raw, &ep); err != nil || ep.URL == "" || ep.Token == "" {
		return Endpoint{}, ErrNotReachable
	}
	if ep.Version != endpointVersion {
		return Endpoint{}, ErrNotReachable
	}
	return ep, nil
}

// Connect 从默认路径加载 endpoint 并构造客户端。
func Connect() (*Client, error) {
	ep, err := LoadEndpoint(DefaultEndpointPath())
	if err != nil {
		return nil, err
	}
	c := NewClient(ep.URL, ep.Token)
	c.App = ep.App
	return c, nil
}
