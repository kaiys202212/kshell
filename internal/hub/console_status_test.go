package hub

import (
	"strings"
	"testing"
)

func TestHandleStatusPerSession(t *testing.T) {
	f := &fakeExecutor{}
	f.statuses = []SessionStatus{
		{Ref: SessionRef{Title: "A", Tool: "claude", Workspace: "/w/alpha"}, State: "running", Summary: "正在修改 wxexecutor.go…"},
		{Ref: SessionRef{Title: "B", Tool: "codex", Workspace: "/w/beta"}, State: "perm", Summary: "请回复 1(允许)或 2(拒绝)"},
	}
	got := NewConsole(f).Handle("#st")[0].Text
	// status_running = "运行中任务: %d"(zh-CN 在后),标题行复用它打印 running 计数。
	if !strings.HasPrefix(got, "运行中任务") {
		t.Fatalf("#st 标题行应复用 status_running key, got %q", got)
	}
	for _, want := range []string{"1. [alpha]", "运行中", "正在修改 wxexecutor.go", "权限待办", "请回复 1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("回执缺 %q: %q", want, got)
		}
	}
}

func TestHandleStatusOrder(t *testing.T) {
	f := &fakeExecutor{}
	f.statuses = []SessionStatus{
		{Ref: SessionRef{Title: "C", Tool: "x", Workspace: "/w/c"}, State: "idle"},
		{Ref: SessionRef{Title: "A", Tool: "x", Workspace: "/w/a"}, State: "running"},
		{Ref: SessionRef{Title: "B", Tool: "x", Workspace: "/w/b"}, State: "perm"},
	}
	got := NewConsole(f).Handle("#st")[0].Text
	// 排序后 Num = i+1:running(A) 重编到会话行首,标题行含"运行中"故必须用
	// 带编号的会话行做锚点,不能拿"运行中"一词比大小。
	if !strings.Contains(got, "1. [a]") {
		t.Fatal("running 应排会话行首")
	}
	if !strings.Contains(got, "2. [b]") {
		t.Fatal("perm 应排第二")
	}
	if !strings.Contains(got, "3. [c]") {
		t.Fatal("idle 应排第三")
	}
}

func TestHandleStatusEmpty(t *testing.T) {
	f := &fakeExecutor{}
	if got := NewConsole(f).Handle("#st")[0].Text; got != "无任务" {
		t.Fatalf("空总览 = %q", got)
	}
}
