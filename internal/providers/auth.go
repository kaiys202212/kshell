package providers

// AuthStatus 是登录态探测的结论。用字符串三态而非 iota:值本身可直接进
// 日志、快照与前端展示,不必再维护一层映射。
type AuthStatus string

const (
	// AuthUnknown 表示无法稳定探测或解析异常:调用方必须保守放行,
	// 不得据此拒绝用户(宁可多跑一次失败的 ACP,不能错杀已登录用户)。
	AuthUnknown AuthStatus = "unknown"
	// AuthIn 表示已探测到有效登录态(如 authInfo.userId 为非空字符串)。
	AuthIn AuthStatus = "in"
	// AuthOut 表示已稳定判定为未登录(从未登录、已登出,或登录态被清空)。
	AuthOut AuthStatus = "out"
)

// AuthProber 是可选接口:由能稳定探测登录态的 provider 实现(目前仅 Cursor)。
// 实现只读自身静态配置文件,不启动子进程,也不依赖 Detect() 成功——
// 安装残缺(Broken)时登录态往往仍是可信的,探测结论不能跟着安装判定一起失效。
type AuthProber interface {
	ProbeAuth() AuthStatus
}

// LoginCmdProvider 是可选接口:由提供官方交互式登录命令的工具实现(目前仅 Cursor)。
// 返回**裸命令**(如 ["cursor-agent","login"]),不带绝对路径:登录不依赖工作区,
// 这里探测反而会与 Detect 的结论重复、两处口径易走偏。可执行文件的实际解析分两层:
// 调用方(desktop.OpenLoginTerminal)优先复用探测结果 tool.BinPath/BinArgs 组装;
// 裸名仅在探测缺失时回退,由 terminal 后端 commandLine 按 PATH 做 exec.LookPath,
// Windows 的 .ps1 shim 由 launcher 解析(裸名对它是 no-op)。
type LoginCmdProvider interface {
	LoginCmd() []string
}
