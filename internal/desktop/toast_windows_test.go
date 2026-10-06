//go:build windows

package desktop

import "testing"

// 构造层面的单测：确认通知对象带上了 AppUserModelID 与标题/正文。
// 真实弹窗（Push）留冒烟验证，不在单测里调用。
func TestNewAgentToast(t *testing.T) {
	n := newAgentToast("CodeBuddy 任务完成", "done")
	if n.AppID != toastAppID {
		t.Fatalf("AppID 应为 %q，得到 %q", toastAppID, n.AppID)
	}
	if n.Title != "CodeBuddy 任务完成" || n.Body != "done" {
		t.Fatalf("标题/正文不符: %q / %q", n.Title, n.Body)
	}
}
