package chat

import (
	"testing"
	"time"

	"github.com/yangk/kshell/internal/acp"
)

func TestPromptSetsTitleFromFirstMessage(t *testing.T) {
	m, _, _ := newTestManager(t)
	var noted []Info
	m.SetOnInfo(func(info Info) { noted = append(noted, info) })
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: `D:\ws`, Title: "占位", ToolID: "claude"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(info.ID, "修复登录空指针\n第二行"); err != nil {
		t.Fatal(err)
	}
	got := m.List()[0]
	if got.Title != "修复登录空指针" || !got.Prompted {
		t.Fatalf("after prompt: %+v", got)
	}
	if len(noted) != 1 || noted[0].Title != "修复登录空指针" {
		t.Fatalf("onInfo: %+v", noted)
	}
	waitChatReady(t, m, info.ID)
	if err := m.Prompt(info.ID, "别覆盖标题"); err != nil {
		t.Fatal(err)
	}
	if m.List()[0].Title != "修复登录空指针" {
		t.Fatalf("second prompt overwrote: %+v", m.List()[0])
	}
}

func waitChatReady(t *testing.T, m *Manager, id string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, info := range m.List() {
			if info.ID == id && info.Status == StatusReady {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("chat not ready")
}

type mcpConn struct {
	fakeConn
	got []acp.McpServerStdio
}

func (c *mcpConn) SetMCP(s []acp.McpServerStdio) { c.got = append([]acp.McpServerStdio(nil), s...) }

type oneBackend struct{ conn Conn }

func (b oneBackend) Start(Spec, acp.Handler) (Conn, error) { return b.conn, nil }

func TestOpenInjectsMCPFactory(t *testing.T) {
	conn := &mcpConn{fakeConn: fakeConn{waitCh: make(chan struct{})}}
	m := NewManager(oneBackend{conn: conn}, nil, nil, nil)
	m.SetMCPFactory(func(id string) []acp.McpServerStdio {
		return []acp.McpServerStdio{{
			Type: "stdio", Name: "kshell", Command: "kshell", Args: []string{"mcp-archive", id},
		}}
	})
	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w", Title: "占位"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(conn.got) != 1 || len(conn.got[0].Args) != 2 || conn.got[0].Args[1] != info.ID {
		t.Fatalf("mcp = %+v id=%s", conn.got, info.ID)
	}
}
