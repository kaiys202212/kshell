package chat

import (
	"context"
	"errors"
	"testing"

	"github.com/yangk/kshell/internal/acp"
)

// authInit 构造带登录方式的 initialize 结果（LoadSession 能力默认开着）。
func authInit(methods ...acp.AuthMethod) acp.InitializeResult {
	return acp.InitializeResult{
		ProtocolVersion:   1,
		AgentCapabilities: acp.AgentCapabilities{LoadSession: true},
		AuthMethods:       methods,
	}
}

// TestOpenAuthRequiredAuthenticatesThenRetries 全链路：new 首次 -32000 →
// 推 authenticating 状态 → authenticate(cursor_login) → 重试恰好一次成功。
func TestOpenAuthRequiredAuthenticatesThenRetries(t *testing.T) {
	m, b, _ := newTestManager(t)
	conn := b.conn
	conn.initSet = true
	conn.initResult = authInit(acp.AuthMethod{ID: "cursor_login", Name: "Cursor Login"})
	conn.newAuthN = 1

	var infos []Info
	m.SetOnInfo(func(i Info) { infos = append(infos, i) })

	info, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err != nil {
		t.Fatalf("登录后重试应成功: %v", err)
	}
	conn.mu.Lock()
	calls := append([]string(nil), conn.authCalls...)
	newN := conn.newCalls
	conn.mu.Unlock()
	if len(calls) != 1 || calls[0] != "cursor_login" {
		t.Fatalf("Authenticate 调用 = %v, want [cursor_login]", calls)
	}
	if newN != 2 {
		t.Fatalf("NewSession 调用 %d 次, want 2（首次 + 登录后重试）", newN)
	}
	authenticating := false
	for _, i := range infos {
		if i.Status == StatusAuthenticating {
			authenticating = true
		}
	}
	if !authenticating {
		t.Fatalf("应推送过 authenticating 状态: %+v", infos)
	}
	if info.Status != StatusReady {
		t.Fatalf("最终状态 = %q, want ready", info.Status)
	}
}

// agent 报告需要登录却不提供任何登录方式：无从认证，返回稳定 key。
func TestOpenAuthRequiredNoMethods(t *testing.T) {
	m, b, _ := newTestManager(t)
	conn := b.conn
	conn.initSet = true
	conn.initResult = authInit()
	conn.newAuthN = 1

	_, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err == nil || err.Error() != "err.chat.auth_required" {
		t.Fatalf("err = %v, want err.chat.auth_required", err)
	}
}

// authenticate 本身失败：错误原样上抛（保留具体原因）。
func TestOpenAuthenticateFails(t *testing.T) {
	m, b, _ := newTestManager(t)
	conn := b.conn
	conn.initSet = true
	conn.initResult = authInit(acp.AuthMethod{ID: "cursor_login"})
	conn.newAuthN = 1
	conn.authErr = errors.New("browser login cancelled")

	_, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err == nil || err.Error() != "browser login cancelled" {
		t.Fatalf("err = %v, want 原始 authenticate 错误", err)
	}
}

// authenticate 超时归一成 err.chat.auth_required：若透传 DeadlineExceeded
// 会被调用方误译成 handshake_timeout，语义完全不同。
func TestOpenAuthTimeoutMapsToAuthRequired(t *testing.T) {
	m, b, _ := newTestManager(t)
	conn := b.conn
	conn.initSet = true
	conn.initResult = authInit(acp.AuthMethod{ID: "cursor_login"})
	conn.newAuthN = 1
	conn.authErr = context.DeadlineExceeded

	_, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err == nil || err.Error() != "err.chat.auth_required" {
		t.Fatalf("err = %v, want err.chat.auth_required", err)
	}
}

// 登录后重试仍 -32000：归一成稳定 key（裸 RPCError 不给前端）。
func TestOpenRetryStillAuthRequired(t *testing.T) {
	m, b, _ := newTestManager(t)
	conn := b.conn
	conn.initSet = true
	conn.initResult = authInit(acp.AuthMethod{ID: "cursor_login"})
	conn.newAuthN = 5 // 恒报未登录：重试也失败

	_, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, "")
	if err == nil || err.Error() != "err.chat.auth_required" {
		t.Fatalf("err = %v, want err.chat.auth_required", err)
	}
}

// 认证失败的握手不得留下幽灵 authenticating 会话：失败时最后一次 onInfo
// 必须是 exited 终态（authenticating 已外发，前端 upsert 会保留该行）。
func TestOpenAuthFailPushesTerminalMeta(t *testing.T) {
	m, b, _ := newTestManager(t)
	conn := b.conn
	conn.initSet = true
	conn.initResult = authInit(acp.AuthMethod{ID: "cursor_login"})
	conn.newAuthN = 1
	conn.authErr = errors.New("browser login cancelled")

	var statuses []string
	m.SetOnInfo(func(i Info) { statuses = append(statuses, i.Status) })

	if _, err := m.Open("new:1", Info{Kind: KindNew, Workspace: "/w"}, Spec{Path: "x"}, ""); err == nil {
		t.Fatal("authenticate 失败应报错")
	}
	// List 不残留（m.fail 摘除），且外发序列以 authenticating 开头、exited 收尾。
	if got := m.List(); len(got) != 0 {
		t.Fatalf("失败会话应被摘除, List=%+v", got)
	}
	if len(statuses) != 2 || statuses[0] != StatusAuthenticating || statuses[1] != StatusExited {
		t.Fatalf("外发状态序列 = %v, want [authenticating exited]", statuses)
	}
}

// 恢复会话路径同样要覆盖：load 首次 -32000 → authenticate → 重试成功。
func TestOpenLoadSessionAuthFlow(t *testing.T) {
	m, b, _ := newTestManager(t)
	conn := b.conn
	conn.initSet = true
	conn.initResult = authInit(acp.AuthMethod{ID: "cursor_login"})
	conn.loadAuthN = 1

	info, err := m.Open("session:s1", Info{
		Kind: KindSession, SessionID: "sess-load", Workspace: "/w", ToolID: "cursor",
	}, Spec{Path: "x"}, "sess-load")
	if err != nil {
		t.Fatalf("登录后重试 load 应成功: %v", err)
	}
	conn.mu.Lock()
	calls := append([]string(nil), conn.authCalls...)
	loadN := conn.loadCalls
	conn.mu.Unlock()
	if len(calls) != 1 || loadN != 2 {
		t.Fatalf("authCalls=%v loadCalls=%d, want 各 1/2 次", calls, loadN)
	}
	if info.Status != StatusReady {
		t.Fatalf("最终状态 = %q, want ready", info.Status)
	}
}
