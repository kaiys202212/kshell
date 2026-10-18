package hubtools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordingBridge 在 fakeBridge 之上记录最近一次 UpdateRequirement 的实参，
// 供确认执行后的合并语义断言用。
type recordingBridge struct {
	*fakeBridge
	lastUpdateID       string
	lastUpdateProject  string
	lastUpdateText     string
	lastUpdatePriority string
	lastUpdateTags     []string
}

func (r *recordingBridge) UpdateRequirement(id, project, text, priority string, tags []string) (RequirementView, error) {
	r.lastUpdateID, r.lastUpdateProject, r.lastUpdateText, r.lastUpdatePriority, r.lastUpdateTags = id, project, text, priority, tags
	return r.fakeBridge.UpdateRequirement(id, project, text, priority, tags)
}

func newTestSession() (*Session, *fakeBridge) {
	f := newFakeBridge()
	s := NewSession(Config{BaseURL: "http://x/v1", APIKey: "k", Model: "m"}, NewRegistry(f), nil)
	return s, f
}

func TestReadOnlyDirectExec(t *testing.T) {
	s, f := newTestSession()
	f.reqs = []RequirementView{{ID: "r1", Text: "修复登录超时", Status: "pending"}}
	result, pending, err := s.Execute(context.Background(), "list_requirements", `{}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if pending != nil {
		t.Fatalf("只读工具不应产生 pending: %+v", pending)
	}
	if !strings.Contains(result, `"id":"r1"`) {
		t.Fatalf("结果缺少关键字段: %s", result)
	}
	if f.called("ListRequirements") != 1 {
		t.Fatalf("ListRequirements 应被调用 1 次, got %d", f.called("ListRequirements"))
	}
}

func TestMutatingCreatesPending(t *testing.T) {
	s, f := newTestSession()
	result, pending, err := s.Execute(context.Background(), "create_requirement", `{"project":"/w/a","text":"新增导出"}`)
	if err != nil {
		t.Fatalf("Mutating 挂起不应返回 error: %v", err)
	}
	if pending == nil {
		t.Fatal("应返回 pending")
	}
	if pending.PendingID == "" {
		t.Fatal("pending 应带 PendingID")
	}
	if pending.ToolName != "create_requirement" {
		t.Fatalf("ToolName 不符: %s", pending.ToolName)
	}
	if result != "" {
		t.Fatalf("挂起时 result 应为空, got %q", result)
	}
	if f.called("CreateRequirement") != 0 {
		t.Fatalf("挂起时不应触达 Bridge, calls=%d", f.called("CreateRequirement"))
	}
}

func TestConfirmApprove(t *testing.T) {
	s, f := newTestSession()
	_, pending, err := s.Execute(context.Background(), "create_requirement", `{"project":"/w/a","text":"新增导出","priority":"high"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	result, err := s.Confirm(context.Background(), pending.PendingID, true)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if f.called("CreateRequirement") != 1 {
		t.Fatalf("确认后工具应真执行 1 次, got %d", f.called("CreateRequirement"))
	}
	if !strings.Contains(result, "new-1") {
		t.Fatalf("应返回执行结果, got %q", result)
	}
}

func TestConfirmReject(t *testing.T) {
	s, f := newTestSession()
	_, pending, err := s.Execute(context.Background(), "create_requirement", `{"project":"/w/a","text":"新增导出"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	result, err := s.Confirm(context.Background(), pending.PendingID, false)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if !strings.Contains(result, "拒绝") {
		t.Fatalf("应返回固定拒绝文本, got %q", result)
	}
	if f.called("CreateRequirement") != 0 {
		t.Fatalf("拒绝后不应执行, calls=%d", f.called("CreateRequirement"))
	}
}

func TestConfirmTwice(t *testing.T) {
	s, _ := newTestSession()
	_, pending, err := s.Execute(context.Background(), "create_requirement", `{"project":"/w/a","text":"x"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, err := s.Confirm(context.Background(), pending.PendingID, true); err != nil {
		t.Fatalf("首次 Confirm: %v", err)
	}
	if _, err := s.Confirm(context.Background(), pending.PendingID, true); !errors.Is(err, ErrUnknownPending) {
		t.Fatalf("二次 Confirm 应返回 ErrUnknownPending, got %v", err)
	}
}

func TestConfirmUnknownPending(t *testing.T) {
	s, _ := newTestSession()
	if _, err := s.Confirm(context.Background(), "nonexistent", true); !errors.Is(err, ErrUnknownPending) {
		t.Fatalf("未知 pendingID 应返回 ErrUnknownPending, got %v", err)
	}
}

func TestConfirmExpired(t *testing.T) {
	s, _ := newTestSession()
	base := time.Now()
	s.now = func() time.Time { return base }
	_, pending, err := s.Execute(context.Background(), "create_requirement", `{"project":"/w/a","text":"x"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// 拨到 6 分钟后：超过 5 分钟 TTL
	s.now = func() time.Time { return base.Add(6 * time.Minute) }
	if _, err := s.Confirm(context.Background(), pending.PendingID, true); !errors.Is(err, ErrPendingExpired) {
		t.Fatalf("超时应返回 ErrPendingExpired, got %v", err)
	}
	// 边界：恰好 5 分钟内仍可确认
	s.now = func() time.Time { return base }
	_, pending2, err := s.Execute(context.Background(), "create_requirement", `{"project":"/w/a","text":"y"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	s.now = func() time.Time { return base.Add(4 * time.Minute) }
	if _, err := s.Confirm(context.Background(), pending2.PendingID, true); err != nil {
		t.Fatalf("TTL 内应可确认: %v", err)
	}
}

func TestMergeUpdateArgs(t *testing.T) {
	f := newFakeBridge()
	f.reqs = []RequirementView{{ID: "r1", Text: "旧描述", Priority: "high", Tags: []string{"a", "b"}, Project: "/w/a"}}

	// 未提供的可选参数 = 保持当前值
	merged, err := mergeUpdateArgs(f, `{"id":"r1","text":"新描述"}`)
	if err != nil {
		t.Fatalf("mergeUpdateArgs: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(merged), &m); err != nil {
		t.Fatalf("合并结果不是合法 JSON: %v", err)
	}
	if m["priority"] != "high" {
		t.Fatalf("未提供的 priority 应保持当前值 high, got %v", m["priority"])
	}
	if m["project"] != "/w/a" {
		t.Fatalf("未提供的 project 应保持当前值 /w/a, got %v", m["project"])
	}
	if fmt.Sprint(m["tags"]) != "[a b]" {
		t.Fatalf("未提供的 tags 应保持当前值 [a b], got %v", m["tags"])
	}

	// 显式空 tags 数组 = 清空（保持 [] 而非回填当前值）
	merged2, err := mergeUpdateArgs(f, `{"id":"r1","text":"新描述","tags":[]}`)
	if err != nil {
		t.Fatalf("mergeUpdateArgs: %v", err)
	}
	var m2 map[string]any
	if err := json.Unmarshal([]byte(merged2), &m2); err != nil {
		t.Fatalf("合并结果不是合法 JSON: %v", err)
	}
	raw, ok := m2["tags"].([]any)
	if !ok || len(raw) != 0 {
		t.Fatalf("显式空 tags 应保持空数组, got %v", m2["tags"])
	}

	// 显式空串 priority = 清空
	merged3, err := mergeUpdateArgs(f, `{"id":"r1","text":"新描述","priority":""}`)
	if err != nil {
		t.Fatalf("mergeUpdateArgs: %v", err)
	}
	var m3 map[string]any
	if err := json.Unmarshal([]byte(merged3), &m3); err != nil {
		t.Fatalf("合并结果不是合法 JSON: %v", err)
	}
	if m3["priority"] != "" {
		t.Fatalf("显式空串 priority 应保持空串, got %v", m3["priority"])
	}

	// 缺 id 报错
	if _, err := mergeUpdateArgs(f, `{"text":"x"}`); err == nil {
		t.Fatal("缺少 id 应返回 error")
	}
}

func TestConfirmUpdateMergeKeepsCurrent(t *testing.T) {
	f := newFakeBridge()
	f.reqs = []RequirementView{{ID: "r1", Text: "旧描述", Priority: "high", Tags: []string{"a", "b"}, Project: "/w/a"}}
	rb := &recordingBridge{fakeBridge: f}
	s := NewSession(Config{}, NewRegistry(rb), nil)

	_, pending, err := s.Execute(context.Background(), "update_requirement", `{"id":"r1","text":"新描述"}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, err := s.Confirm(context.Background(), pending.PendingID, true); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if rb.lastUpdateText != "新描述" {
		t.Fatalf("text 应为新值, got %q", rb.lastUpdateText)
	}
	if rb.lastUpdatePriority != "high" {
		t.Fatalf("未提供的 priority 应保持当前值 high, got %q", rb.lastUpdatePriority)
	}
	if strings.Join(rb.lastUpdateTags, ",") != "a,b" {
		t.Fatalf("未提供的 tags 应保持当前值 [a b], got %v", rb.lastUpdateTags)
	}
}

func TestConfirmUpdateMergeClearsTags(t *testing.T) {
	f := newFakeBridge()
	f.reqs = []RequirementView{{ID: "r1", Text: "旧描述", Priority: "high", Tags: []string{"a", "b"}}}
	rb := &recordingBridge{fakeBridge: f}
	s := NewSession(Config{}, NewRegistry(rb), nil)

	_, pending, err := s.Execute(context.Background(), "update_requirement", `{"id":"r1","text":"新描述","tags":[]}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, err := s.Confirm(context.Background(), pending.PendingID, true); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if rb.lastUpdateTags == nil || len(rb.lastUpdateTags) != 0 {
		t.Fatalf("显式空 tags 应清空（传空数组而非保持 [a b]）, got %v", rb.lastUpdateTags)
	}
}

func TestExecuteConfirmedRejectsReadOnly(t *testing.T) {
	r := NewRegistry(newFakeBridge())
	if _, err := r.ExecuteConfirmed(context.Background(), "list_requirements", `{}`); err == nil {
		t.Fatal("ExecuteConfirmed 只接受写工具，只读工具应报错")
	}
}

func TestSessionConcurrent(t *testing.T) {
	s, f := newTestSession()
	f.reqs = []RequirementView{{ID: "r1", Text: "x", Status: "pending"}}
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if _, _, err := s.Execute(ctx, "list_requirements", `{}`); err != nil {
					t.Error(err)
					return
				}
				_, pending, err := s.Execute(ctx, "create_requirement", fmt.Sprintf(`{"project":"/w/a","text":"t%d-%d"}`, i, j))
				if err != nil || pending == nil {
					t.Errorf("Execute: err=%v pending=%v", err, pending)
					return
				}
				if _, err := s.Confirm(ctx, pending.PendingID, j%2 == 0); err != nil {
					t.Errorf("Confirm: %v", err)
					return
				}
				if _, err := s.Confirm(ctx, "bogus", true); !errors.Is(err, ErrUnknownPending) {
					t.Errorf("未知 pendingID 应 ErrUnknownPending, got %v", err)
					return
				}
				s.AppendMessage(Message{Role: "user", Content: "hi"})
				_ = s.History()
			}
		}(i)
	}
	wg.Wait()
}
