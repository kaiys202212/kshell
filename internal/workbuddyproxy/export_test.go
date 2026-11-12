package workbuddyproxy

// 本文件仅在测试构建中编译：SetChatOnceForTest 不应出现在生产二进制的公开 API 中。

import (
	"context"
	"net/http"

	"github.com/yangk/kshell/internal/workbuddy"
)

// SetChatOnceForTest 替换 ChatOnce 注入点，返回恢复函数。
// 生产实现 = 每请求 workbuddy.LoadCredential + workbuddy.ChatOnce。
func SetChatOnceForTest(fn func(ctx context.Context, cred workbuddy.Credential, body []byte, hc *http.Client) ([]byte, error)) (restore func()) {
	prev := chatOnce
	chatOnce = fn
	return func() { chatOnce = prev }
}
