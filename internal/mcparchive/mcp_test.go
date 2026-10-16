package mcparchive

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestHandleToolsListAndSuggest(t *testing.T) {
	list, ok := Handle([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`), nil)
	if !ok || !bytes.Contains(list, []byte(`"suggest_archive"`)) {
		t.Fatalf("list ok=%v body=%s", ok, list)
	}
	var got string
	resp, ok := Handle([]byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"suggest_archive","arguments":{"summary":"登录已修好"}}}`), func(summary string) error {
		got = summary
		return nil
	})
	if !ok || got != "登录已修好" || !bytes.Contains(resp, []byte("已询问用户是否归档")) {
		t.Fatalf("ok=%v summary=%q resp=%s", ok, got, resp)
	}
	if _, ok := Handle([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`), nil); ok {
		t.Fatal("notification should not respond")
	}
}

// ServeIO 必须按 MCP stdio 标准（换行分隔 JSON）读写；claude CLI 起本 server 后
// 发换行 JSON initialize，若按 LSP Content-Length 帧解析将永不应答，客户端干等
// 启动超时（实测 ~30s），把 ACP session/new 顶过握手上限。
func TestServeIONewlineDelimitedJSON(t *testing.T) {
	var out bytes.Buffer
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		``,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`, // 通知：不回包
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`not-json`, // 非法行：回 error 响应
	}, "\n")
	if err := ServeIO(strings.NewReader(in), &out, nil); err != nil {
		t.Fatalf("ServeIO: %v", err)
	}
	lines := nonEmptyLines(out.String())
	if len(lines) != 3 {
		t.Fatalf("应答应恰 3 行（initialize/tools/list/非法行），got %d: %q", len(lines), out.String())
	}
	if !strings.Contains(lines[0], `"protocolVersion"`) || !strings.Contains(lines[0], `"id":1`) {
		t.Fatalf("initialize 应答异常: %s", lines[0])
	}
	if !strings.Contains(lines[1], `"suggest_archive"`) || !strings.Contains(lines[1], `"id":2`) {
		t.Fatalf("tools/list 应答异常: %s", lines[1])
	}
	if !strings.Contains(lines[2], `"error"`) {
		t.Fatalf("非法 JSON 行应回 error 响应: %s", lines[2])
	}
	// 不应有 LSP 帧头残留
	if strings.Contains(out.String(), "Content-Length") {
		t.Fatalf("不应输出 LSP 帧头: %q", out.String())
	}
}

// 超长消息不得爆 Scanner 默认 64KB 上限：注入的 args 里可能带长 token。
func TestServeIOLongLine(t *testing.T) {
	var out bytes.Buffer
	long := strings.Repeat("x", 200*1024)
	in := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"junk":"` + long + `"}}`
	if err := ServeIO(strings.NewReader(in), &out, nil); err != nil {
		t.Fatalf("ServeIO: %v", err)
	}
	if !strings.Contains(out.String(), `"id":1`) {
		t.Fatalf("超长行应正常应答: %d bytes", out.Len())
	}
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimRight(l, "\r")
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func TestWriteServersMerge(t *testing.T) {
	path := t.TempDir() + "/mcp.json"
	err := WriteServers(path, []ServerDef{
		{Name: "kshell", Command: "exe", Args: []string{"mcp-archive"}},
		{Name: "kshell-ssh", Command: "exe", Args: []string{"mcp-ssh", "--token", "t"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"kshell-ssh"`)) || !bytes.Contains(raw, []byte(`"kshell"`)) {
		t.Fatalf("body=%s", raw)
	}
}

func TestPrependConfigOnlyClaude(t *testing.T) {
	got := PrependConfig("claude", `C:\mcp.json`, []string{"--resume", "s1"})
	want := []string{"--mcp-config", `C:\mcp.json`, "--resume", "s1"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("claude args = %#v", got)
	}
	plain := []string{"resume", "s1"}
	if strings.Join(PrependConfig("codex", `C:\mcp.json`, plain), "|") != "resume|s1" {
		t.Fatal("codex should be unchanged")
	}
}

func TestHubSuggestAuth(t *testing.T) {
	var ref, summary string
	h, err := StartHub(func(r, s string) { ref, summary = r, s })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)

	bad, err := http.Post(h.URL+"/suggest", "application/json", strings.NewReader(`{"ref":"terminal:t1","summary":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	bad.Body.Close()
	if bad.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d", bad.StatusCode)
	}

	req, err := http.NewRequest(http.MethodPost, h.URL+"/suggest", strings.NewReader(`{"ref":"chat:c1","summary":"做完了"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+h.Token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || ref != "chat:c1" || summary != "做完了" {
		t.Fatalf("status=%d ref=%q summary=%q", res.StatusCode, ref, summary)
	}
}
