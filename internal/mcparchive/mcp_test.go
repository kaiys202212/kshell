package mcparchive

import (
	"bytes"
	"io"
	"net/http"
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
