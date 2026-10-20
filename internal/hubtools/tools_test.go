package hubtools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

// fakeBridge 是内存桩：记录各方法调用次数，返回固定数据。
// 带锁保护 calls：并发测试中 Session 会并发调用 Bridge。
type fakeBridge struct {
	mu        sync.Mutex
	calls     map[string]int
	reqs      []RequirementView
	workspace *WorkspaceView
	tasks     []TaskView
	task      *TaskView
	recent    []ChatLogEntry
	// dispatch 桩：记录实参并返回预置结果
	dispatchRef      string
	dispatchTaskText string
	dispatchLabel    string
	dispatchErr      error
}

func newFakeBridge() *fakeBridge {
	return &fakeBridge{calls: map[string]int{}}
}

// count 记录一次调用（线程安全）。
func (f *fakeBridge) count(key string) {
	f.mu.Lock()
	f.calls[key]++
	f.mu.Unlock()
}

// called 读取调用次数（线程安全）。
func (f *fakeBridge) called(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[key]
}

func (f *fakeBridge) ListRequirements() ([]RequirementView, error) {
	f.count("ListRequirements")
	return f.reqs, nil
}

func (f *fakeBridge) GetRequirement(id string) (*RequirementView, error) {
	f.count("GetRequirement")
	for i := range f.reqs {
		if f.reqs[i].ID == id {
			return &f.reqs[i], nil
		}
	}
	return nil, errors.New("需求不存在: " + id)
}

func (f *fakeBridge) CreateRequirement(project, text, priority string, tags []string) (RequirementView, error) {
	f.count("CreateRequirement")
	return RequirementView{ID: "new-1", Project: project, Text: text, Priority: priority, Tags: tags}, nil
}

func (f *fakeBridge) UpdateRequirement(id, project, text, priority string, tags []string) (RequirementView, error) {
	f.count("UpdateRequirement")
	return RequirementView{ID: id, Project: project, Text: text, Priority: priority, Tags: tags}, nil
}

func (f *fakeBridge) DeleteRequirement(id string) error {
	f.count("DeleteRequirement")
	return nil
}

func (f *fakeBridge) ListWorkspaces() ([]WorkspaceView, error) {
	f.count("ListWorkspaces")
	return []WorkspaceView{{Path: "/w/a", Name: "a", SessionCount: 2}}, nil
}

func (f *fakeBridge) GetWorkspace(id string) (*WorkspaceView, error) {
	f.count("GetWorkspace")
	return f.workspace, nil
}

func (f *fakeBridge) ListRunningTasks() ([]TaskView, error) {
	f.count("ListRunningTasks")
	return f.tasks, nil
}

func (f *fakeBridge) GetTaskDetail(id string) (*TaskView, error) {
	f.count("GetTaskDetail")
	return f.task, nil
}

func (f *fakeBridge) RecentMessages(n int) []ChatLogEntry {
	f.count("RecentMessages")
	return f.recent
}

func (f *fakeBridge) DispatchTask(ref, task string) (string, error) {
	f.count("DispatchTask")
	f.dispatchRef, f.dispatchTaskText = ref, task
	return f.dispatchLabel, f.dispatchErr
}

func TestToolsRegistered(t *testing.T) {
	r := NewRegistry(newFakeBridge())
	got := r.Tools()
	want := map[string]bool{
		"list_requirements": false, "get_requirement": false,
		"create_requirement": false, "update_requirement": false, "delete_requirement": false,
		"list_workspaces": false, "get_workspace": false,
		"list_running_tasks": false, "get_task_detail": false,
		"dispatch_task": false,
	}
	seen := map[string]bool{}
	for _, td := range got {
		if _, dup := seen[td.Name]; dup {
			t.Fatalf("工具名重复: %s", td.Name)
		}
		seen[td.Name] = true
		if _, ok := want[td.Name]; !ok {
			t.Fatalf("未预期的工具: %s", td.Name)
		}
		want[td.Name] = true
	}
	for name, ok := range want {
		if !ok {
			t.Fatalf("缺少工具: %s", name)
		}
	}
}

func TestSchemaValid(t *testing.T) {
	r := NewRegistry(newFakeBridge())
	for _, td := range r.Tools() {
		if !json.Valid([]byte(fmtJSON(td.Parameters))) {
			t.Fatalf("%s: Parameters 不是合法 JSON", td.Name)
		}
		if td.Parameters["type"] != "object" {
			t.Fatalf("%s: Parameters.type 应为 object, got %v", td.Name, td.Parameters["type"])
		}
		if td.Description == "" {
			t.Fatalf("%s: 描述为空", td.Name)
		}
	}
}

// fmtJSON 序列化辅助（避免测试内重复 import 别名）。
func fmtJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// TestDispatchTaskExec 验证 dispatch_task 即时执行：透传 ref/task 给 Bridge，
// 结果包装为 {"chat": label}；Bridge 错误原样透传；不是 Mutating（无挂起）。
func TestDispatchTaskExec(t *testing.T) {
	s, f := newTestSession()
	f.dispatchLabel = "Cursor「修复登录」"
	result, pending, err := s.Execute(context.Background(), "dispatch_task", `{"ref":"3","task":"修复登录"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if pending != nil {
		t.Fatalf("dispatch_task 不应产生 pending: %+v", pending)
	}
	if !strings.Contains(result, `"chat":"Cursor「修复登录」"`) {
		t.Fatalf("结果应含目标标签: %s", result)
	}
	if f.dispatchRef != "3" || f.dispatchTaskText != "修复登录" {
		t.Fatalf("实参未透传: ref=%q task=%q", f.dispatchRef, f.dispatchTaskText)
	}

	// ref 无法解析：错误透传给模型
	f.dispatchErr = errors.New("未知的会话编号: 9")
	if _, _, err := s.Execute(context.Background(), "dispatch_task", `{"ref":"9","task":"x"}`); err == nil {
		t.Fatal("Bridge 错误应透传")
	}
}

// TestMutatingFlag 通过注册表元数据断言三个写工具的 Mutating 标记。
func TestMutatingFlag(t *testing.T) {
	r := NewRegistry(newFakeBridge())
	mutating := map[string]bool{}
	for name, m := range r.mutating {
		mutating[name] = m
	}
	for _, name := range []string{"create_requirement", "update_requirement", "delete_requirement"} {
		if !mutating[name] {
			t.Fatalf("%s 应标记为 Mutating", name)
		}
	}
	for _, name := range []string{"list_requirements", "get_requirement", "list_workspaces", "get_workspace", "list_running_tasks", "get_task_detail"} {
		if mutating[name] {
			t.Fatalf("%s 不应标记为 Mutating", name)
		}
	}
}

func TestListRequirementsExec(t *testing.T) {
	f := newFakeBridge()
	f.reqs = []RequirementView{{ID: "r1", Text: "修复登录超时", Priority: "high", Status: "pending"}}
	r := NewRegistry(f)
	out, err := r.Execute(context.Background(), "list_requirements", `{}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, `"id":"r1"`) || !strings.Contains(out, "修复登录超时") {
		t.Fatalf("输出缺少关键字段: %s", out)
	}
	if f.called("ListRequirements") != 1 {
		t.Fatalf("ListRequirements 应被调用 1 次, got %d", f.called("ListRequirements"))
	}
}

func TestGetWorkspaceExec(t *testing.T) {
	f := newFakeBridge()
	f.workspace = &WorkspaceView{Path: "/w/a", Name: "a", SessionCount: 3, ToolCounts: map[string]int{"claude": 2}}
	r := NewRegistry(f)
	out, err := r.Execute(context.Background(), "get_workspace", `{"id":"/w/a"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, `"/w/a"`) || !strings.Contains(out, `"sessionCount":3`) {
		t.Fatalf("输出缺少关键字段: %s", out)
	}
}

func TestListRunningTasksExec(t *testing.T) {
	f := newFakeBridge()
	f.tasks = []TaskView{{ID: "t1", Title: "重构 hubtools", ToolID: "claude", Workspace: "/w/a", Status: "running"}}
	r := NewRegistry(f)
	out, err := r.Execute(context.Background(), "list_running_tasks", `{}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, `"id":"t1"`) || !strings.Contains(out, `"status":"running"`) {
		t.Fatalf("输出缺少关键字段: %s", out)
	}
}

func TestMutatingPending(t *testing.T) {
	f := newFakeBridge()
	r := NewRegistry(f)
	for _, tc := range []struct {
		name    string
		args    string
		summary []string
	}{
		{"create_requirement", `{"project":"/w/a","text":"新增导出功能","priority":"high","tags":["p2"]}`, []string{"新增导出功能", "/w/a"}},
		{"delete_requirement", `{"id":"r42"}`, []string{"r42"}},
	} {
		out, err := r.Execute(context.Background(), tc.name, tc.args)
		if out != "" {
			t.Fatalf("%s: 挂起时不应有结果文本, got %q", tc.name, out)
		}
		if !errors.Is(err, ErrConfirmationRequired) {
			t.Fatalf("%s: 应返回 ErrConfirmationRequired, got %v", tc.name, err)
		}
		var pending *PendingConfirm
		if !errors.As(err, &pending) {
			t.Fatalf("%s: 错误应包装 *PendingConfirm, got %v", tc.name, err)
		}
		if pending.ToolName != tc.name || pending.ArgsJSON != tc.args {
			t.Fatalf("%s: PendingConfirm 字段不符: %+v", tc.name, pending)
		}
		for _, kw := range tc.summary {
			if !strings.Contains(pending.Summary, kw) {
				t.Fatalf("%s: 摘要 %q 缺少 %q", tc.name, pending.Summary, kw)
			}
		}
	}
	if f.called("CreateRequirement") != 0 || f.called("DeleteRequirement") != 0 {
		t.Fatalf("Mutating 工具挂起时不应触达 Bridge, create=%d delete=%d", f.called("CreateRequirement"), f.called("DeleteRequirement"))
	}
}

func TestUnknownTool(t *testing.T) {
	r := NewRegistry(newFakeBridge())
	if _, err := r.Execute(context.Background(), "no_such_tool", `{}`); err == nil {
		t.Fatal("未知工具应返回 error")
	}
}

func TestBadArgsJSON(t *testing.T) {
	r := NewRegistry(newFakeBridge())
	if _, err := r.Execute(context.Background(), "list_workspaces", `{bad`); err == nil {
		t.Fatal("非法 JSON 应返回 error")
	}
}
