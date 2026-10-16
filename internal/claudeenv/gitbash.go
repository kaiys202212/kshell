// Package claudeenv 为 claude CLI（含其 ACP 适配器）补齐 Windows 运行环境。
// claude CLI 在 Windows 上强依赖 git-bash：不做 PATH 探测，只认
// CLAUDE_CODE_GIT_BASH_PATH 环境变量与标准安装路径（C:\Program Files\Git 等），
// Git 装在非标准位置时 CLI 启动即退出，
// 表现为 ACP session/new 报 Query closed before response received。
// 这里负责找到 bash.exe 并给出 CLAUDE_CODE_GIT_BASH_PATH 的注入值。
package claudeenv

// GitBashEnv 返回应注入的 CLAUDE_CODE_GIT_BASH_PATH 值；
// ok=false 表示不需要/无法确定，调用方不注入。
func GitBashEnv() (string, bool) { return gitBashEnvPlatform() }
