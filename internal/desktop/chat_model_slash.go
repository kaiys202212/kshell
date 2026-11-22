package desktop

import (
	"errors"
	"fmt"
	"strings"

	"github.com/yangk/kshell/internal/config"
	"github.com/yangk/kshell/internal/providers"
)

// errChatModelNotAvailable 是 /model 参数不可用的稳定错误 key 前缀。
// 现场事故：会话内执行 `/model claude`，Claude CLI 不认这个裸名（不是内置
// 别名），把它字面作为模型名发给 OpenCode Go 网关，返回 400
// "Model claude isn't included"。这类字面值一旦写进会话配置，后续消息会持续
// 失败；因此在投递前就拦掉 CLI 无法解析的值，引导用户用下拉选择。
var errChatModelNotAvailable = errors.New("err.chat.model_not_available")

// parseModelSlash 识别「整串就是一条 /model 指令」的输入，返回其参数与是否命中。
// 只认整串以 /model 开头（允许前后空白），避免误伤正文中提到 /model 的消息。
func parseModelSlash(text string) (arg string, isCmd bool) {
	t := strings.TrimSpace(text)
	rest, found := strings.CutPrefix(t, "/model")
	if !found {
		return "", false
	}
	// "/models" 这类前缀相同的命令不算：/model 之后必须紧跟空白或结束。
	if rest != "" && !strings.HasPrefix(rest, " ") && !strings.HasPrefix(rest, "\t") {
		return "", false
	}
	return strings.TrimSpace(rest), true
}

// checkModelSlash 判定 /model 参数能否直接投递：
//   - 空参数：纯查询，放行（由 agent 自己回显当前模型）；
//   - Claude 内置别名（default/sonnet/opus/haiku 及变体）：CLI 能解析，放行；
//   - 当前平台模型列表里的具体模型名：放行；
//   - 其余（如裸名 claude）：拒绝，返回带参数的稳定 key。
//
// 平台模型列表为空时（用户没配任何平台）不做判断：无从区分「合法模型名」
// 与「CLI 不认识的裸名」，交给 agent 自己报错比误拦合法模型更好。
//
// 非 claude 工具不做拦截：别名语义由各工具自定（cursor 的 default[] 等）。
func checkModelSlash(arg string, cfg config.ModelConfig) error {
	if arg == "" {
		return nil
	}
	if providers.IsClaudeBuiltinAlias(arg) {
		return nil
	}
	hasAnyModel := false
	for _, p := range cfg.Platforms {
		for _, m := range p.Models {
			if strings.TrimSpace(m) == "" {
				continue
			}
			hasAnyModel = true
			if strings.EqualFold(strings.TrimSpace(m), arg) {
				return nil
			}
		}
	}
	if !hasAnyModel {
		return nil
	}
	return fmt.Errorf("%w|%s", errChatModelNotAvailable, arg)
}

// isHandshakeTimeout 判断错误是否为 ACP 握手超时（可能是「合法慢」被截断）。
// 这类错误值得重试一次：cursor 的 session/new 会因 MCP 初始化变慢，
// 而会话往往已在磁盘落地，直接降级终端会白丢上下文。
func isHandshakeTimeout(err error) bool {
	return err != nil && strings.Contains(err.Error(), "err.chat.handshake_timeout")
}
