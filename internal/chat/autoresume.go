package chat

import (
	"context"
	"errors"
	"strings"

	"github.com/yangk/kshell/internal/acp"
)

// shouldAutoResume 判断一轮失败是否值得用「继续」自动重试。
// 只认明确的瞬时网络/限流特征；未知错误一律放弃，避免把确定性失败变成循环。
func shouldAutoResume(err error, stop, stderr string) bool {
	text := strings.ToLower(stderr)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return false
		}
		var rpc *acp.RPCError
		if errors.As(err, &rpc) {
			switch rpc.Code {
			case acp.ErrCodeAuthRequired, -32601, -32602:
				return false
			}
		}
		text = strings.ToLower(err.Error()) + "\n" + text
	} else {
		switch stop {
		case "", "end_turn", "cancelled", "max_tokens", "max_turn_requests":
			return false
		case "refusal":
			// 上游常把 429 映射成 refusal；只在 stderr 有瞬时特征时续跑。
			return containsAny(text, transientHints)
		default:
			return false
		}
	}
	if containsAny(text, deterministicHints) {
		return false
	}
	return containsAny(text, transientHints)
}

var transientHints = []string{
	"429", "rate limit", "too many requests", "too frequent",
	"502", "503", "504", "overloaded", "temporarily unavailable",
	"timeout", "timed out", "deadline exceeded",
	"connection reset", "connection refused", "connection closed",
	"socket hang up", "unexpected eof", "stream error", "network error",
	"econnreset", "econnrefused", "etimedout", "enotfound",
}

var deterministicHints = []string{
	"authrequired", "unauthorized", "forbidden", "invalid",
	"not found", "unsupported", "quota", "billing", "insufficient",
	"context length", "too long", "maximum context",
	"permission denied", "does not exist", "canceled", "cancelled",
}

func containsAny(s string, hints []string) bool {
	for _, h := range hints {
		if strings.Contains(s, h) {
			return true
		}
	}
	return false
}
