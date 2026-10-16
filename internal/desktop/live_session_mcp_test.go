package desktop

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/yangk/kshell/internal/mcparchive"
)

// TestChatMCPServersNoTypeKey 验证 chatMCPServers 产出的 stdio server 序列化
// 不携带 type 键：claude-agent-acp 按「"type" in server」区分形态，带 type
// （含 "stdio"）会被当 HTTP server 处理并丢弃 command，claude CLI 在
// session/new 阶段即退出（-32603: Query closed before response received）。
func TestChatMCPServersNoTypeKey(t *testing.T) {
	app, _, _ := newTestApp(t)
	app.mu.Lock()
	app.archiveHub = &mcparchive.Hub{URL: "http://127.0.0.1:9", Token: "tok"}
	app.mu.Unlock()

	servers := app.chatMCPServers("no-such-chat")
	if len(servers) == 0 {
		t.Fatal("chatMCPServers 未产出任何 server（archiveHub 未生效）")
	}
	b, err := json.Marshal(servers)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(`"type"`)) {
		t.Fatalf("stdio server 序列化不应含 type 键: %s", b)
	}
	if !bytes.Contains(b, []byte(`"command"`)) {
		t.Fatalf("stdio server 应保留 command: %s", b)
	}
}
