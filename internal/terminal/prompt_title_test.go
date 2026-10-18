package terminal

import "testing"

func TestWriteEnterSetsPromptTitleOnce(t *testing.T) {
	b := &stubBackend{}
	var noted []Info
	m := NewManager(b, nil, nil)
	m.SetOnInfo(func(info Info) { noted = append(noted, info) })

	info, err := m.Open("new:1", Info{
		Kind: KindNew, Workspace: `D:\ws`, Title: "占位", ToolID: "claude",
	}, Spec{Path: "claude"}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Write(info.ID, []byte("修复登录空指针\r")); err != nil {
		t.Fatal(err)
	}
	got := m.List()[0]
	if got.Title != "修复登录空指针" || !got.Prompted {
		t.Fatalf("after enter: %+v", got)
	}
	if len(noted) != 1 || noted[0].Title != "修复登录空指针" {
		t.Fatalf("onInfo: %+v", noted)
	}
	if err := m.Write(info.ID, []byte("另一句\r")); err != nil {
		t.Fatal(err)
	}
	if m.List()[0].Title != "修复登录空指针" || len(noted) != 1 {
		t.Fatalf("second line overwrote: %+v notes=%d", m.List()[0], len(noted))
	}
}

func TestWriteIgnoresOSCColorReplyForPromptTitle(t *testing.T) {
	b := &stubBackend{}
	m := NewManager(b, nil, nil)
	info, err := m.Open("new:1", Info{
		Kind: KindNew, Workspace: `D:\ws`, Title: "占位", ToolID: "opencode",
	}, Spec{Path: "opencode"}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	// xterm 对 OSC 4 查色的应答会走 onData→Write；不得当成首行标题，也不应标 Prompted
	oscBEL := "\x1b]4;0;rgb:2e2e/3434/3636\x07"
	if err := m.Write(info.ID, []byte(oscBEL+"\r")); err != nil {
		t.Fatal(err)
	}
	got := m.List()[0]
	if got.Title != "占位" || got.Prompted {
		t.Fatalf("after OSC BEL: %+v", got)
	}
	oscST := "\x1b]4;0;rgb:aaaa/bbbb/cccc\x1b\\"
	if err := m.Write(info.ID, []byte(oscST+"\r")); err != nil {
		t.Fatal(err)
	}
	got = m.List()[0]
	if got.Title != "占位" || got.Prompted {
		t.Fatalf("after OSC ST: %+v", got)
	}
	if err := m.Write(info.ID, []byte("修复登录空指针\r")); err != nil {
		t.Fatal(err)
	}
	got = m.List()[0]
	if got.Title != "修复登录空指针" || !got.Prompted {
		t.Fatalf("real prompt: %+v", got)
	}
}

func TestWriteEnterIgnoresShellAndEscape(t *testing.T) {
	b := &stubBackend{}
	m := NewManager(b, nil, nil)
	info, err := m.Open("sh:1", Info{Kind: KindShell, Workspace: `D:\ws`, Title: "终端"}, Spec{Path: "cmd"}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Write(info.ID, []byte("dir\r")); err != nil {
		t.Fatal(err)
	}
	if m.List()[0].Title != "终端" || m.List()[0].Prompted {
		t.Fatalf("shell should stay: %+v", m.List()[0])
	}

	agent, err := m.Open("new:2", Info{Kind: KindNew, Title: "占位", Workspace: `D:\ws`}, Spec{Path: "claude"}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	// 方向键转义不进标题；退格按字符删（修复 + 退格 + 好 = 修好）
	if err := m.Write(agent.ID, []byte("\x1b[A修复\x7f好\r")); err != nil {
		t.Fatal(err)
	}
	if got := m.List()[1].Title; got != "修好" {
		t.Fatalf("title = %q", got)
	}
}

func TestPrepareSpecRunsBeforeStart(t *testing.T) {
	b := &stubBackend{}
	m := NewManager(b, nil, nil)
	m.SetPrepareSpec(func(id string, info Info, spec Spec) Spec {
		spec.Args = append([]string{"--mcp-config", id}, spec.Args...)
		return spec
	})
	info, err := m.Open("new:1", Info{Kind: KindNew, Title: "占位"}, Spec{Path: "claude", Args: []string{"--resume", "s1"}}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	got := b.lastSpec().Args
	if len(got) != 4 || got[0] != "--mcp-config" || got[1] != info.ID || got[2] != "--resume" || got[3] != "s1" {
		t.Fatalf("args = %#v id=%s", got, info.ID)
	}
}

// TestPrepareSpecSkipsNonAgentKinds 锁定「非 agent 会话不注入」的口径:
// shell/ssh 是用户自己的命令行,login 跑的是工具官方登录命令(如 cursor-agent
// login),三类都吃不得 MCP/agenthook 注入——注入参数会改变命令语义甚至让
// 登录命令解析失败。agent 会话(session/new)则必须注入。
func TestPrepareSpecSkipsNonAgentKinds(t *testing.T) {
	cases := []struct {
		kind   string
		inject bool
	}{
		{KindSession, true},
		{KindNew, true},
		{KindShell, false},
		{KindSSH, false},
		{KindLogin, false},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			b := &stubBackend{}
			m := NewManager(b, nil, nil)
			m.SetPrepareSpec(func(id string, info Info, spec Spec) Spec {
				spec.Args = append([]string{"--mcp-config", id}, spec.Args...)
				return spec
			})
			key := "k:" + c.kind
			if _, err := m.Open(key, Info{Kind: c.kind, Title: "占位"}, Spec{Path: "claude", Args: []string{"--resume", "s1"}}, 80, 24); err != nil {
				t.Fatal(err)
			}
			got := b.lastSpec().Args
			if c.inject {
				if len(got) == 0 || got[0] != "--mcp-config" {
					t.Fatalf("kind=%s 应注入, args = %#v", c.kind, got)
				}
				return
			}
			if len(got) != 2 || got[0] != "--resume" || got[1] != "s1" {
				t.Fatalf("kind=%s 不应注入, args = %#v", c.kind, got)
			}
		})
	}
}
