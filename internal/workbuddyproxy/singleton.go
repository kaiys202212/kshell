package workbuddyproxy

import "sync"

// 进程内惰性单例：所有桌面端启动入口共用同一代理实例，避免每个工具各自
// 起监听。单例永不自动 Close——agent 是 kshell 子进程，主进程退出即一同
// 终结，代理生命周期天然对齐；强制关闭反而会打断仍在运行的会话。
var (
	defaultServerOnce sync.Once
	defaultServer     *Server
)

// Get 返回进程内代理单例（首次调用时创建，尚未启动监听）。
// Ensure 仍由 modelresolve 在真正选中 workbuddy 平台时才调用（惰性起监听）。
func Get() *Server {
	defaultServerOnce.Do(func() { defaultServer = New() })
	return defaultServer
}
