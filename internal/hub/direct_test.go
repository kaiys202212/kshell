package hub

import "testing"

func TestIsDirectPass(t *testing.T) {
	def := DefaultDirectPatterns() // ["#","@","continue"]
	cases := []struct {
		in   string
		want bool
	}{
		{"#g 1,2 hi", true},       // # 前缀(命令族整体直通,knownCommands 判定留给 Handle)
		{"＃st", true},             // 全角 # 也放行
		{"@1 帮我看看", true},         // @ 指名
		{"continue", true},        // 首词
		{"Continue please", true}, // 首词大小写不敏感
		{"explain this", false},   // explain 已移出默认集：进管理者循环
		{"你好", false},
		{"1", false},            // 纯数字进路由(语境由路由器决断)
		{"continuation", false}, // 非首词完整匹配
		{"", false},
		{"   ", false}, // 纯空白不算输入
	}
	for _, tc := range cases {
		if got := IsDirectPass(tc.in, def); got != tc.want {
			t.Fatalf("IsDirectPass(%q)=%v, want %v", tc.in, got, tc.want)
		}
	}
	// patterns 为 nil 时回落内置默认集。
	if !IsDirectPass("Continue please", nil) {
		t.Fatalf("nil patterns 未回落默认集")
	}
	// #/＃ 命令前缀不依赖白名单配置（命令合法性由 Handle 判定）。
	if !IsDirectPass("＃st", []string{"continue"}) {
		t.Fatalf("全角 # 应无条件放行")
	}
	// 用户自定义 pattern: 非 ASCII 项按首词前缀匹配。
	if !IsDirectPass("部署一下 prod", []string{"部署"}) {
		t.Fatalf("自定义前缀未命中")
	}
	// 多词 pattern: 整句前缀匹配（大小写不敏感），且必须从头开始。
	if !IsDirectPass("Continue Please do X", []string{"continue please"}) {
		t.Fatalf("多词 pattern 未命中")
	}
	if IsDirectPass("please continue please", []string{"continue please"}) {
		t.Fatalf("多词 pattern 不应命中句中位置")
	}
	// 自定义 ASCII 单词: 只按首词整词匹配，不做前缀。
	if !IsDirectPass("EXPLAIN x", []string{"explain"}) {
		t.Fatalf("自定义整词大小写不敏感未命中")
	}
	if IsDirectPass("explainx", []string{"explain"}) {
		t.Fatalf("自定义整词不应前缀命中")
	}
}

func TestDefaultDirectPatterns(t *testing.T) {
	got := DefaultDirectPatterns()
	for _, want := range []string{"#", "@", "continue"} {
		found := false
		for _, p := range got {
			if p == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("默认集缺 %q: %v", want, got)
		}
	}
	// explain 已移出默认集（该语境交管理者循环处理）。
	for _, p := range got {
		if p == "explain" {
			t.Fatalf("默认集不应再含 explain: %v", got)
		}
	}
}

// TestIsContinueConfirm 覆盖「继续类短确认」捷径判定：去空白后整词匹配，
// 大小写不敏感；非确认词不命中。
func TestIsContinueConfirm(t *testing.T) {
	for _, in := range []string{"继续", "  继续  ", "继续吧", "continue", "Continue", "好", "好的", "行", "ok", "OK", "Ok"} {
		if !IsContinueConfirm(in) {
			t.Fatalf("IsContinueConfirm(%q) 应命中", in)
		}
	}
	for _, in := range []string{"", "   ", "继续一下", "continue please", "好呀", "好吧", "不行", "okay", "继续任务"} {
		if IsContinueConfirm(in) {
			t.Fatalf("IsContinueConfirm(%q) 不应命中", in)
		}
	}
}

func TestConsoleBusy(t *testing.T) {
	c := NewConsole(&fakeExecutor{})
	if c.Busy() {
		t.Fatal("新建 Console 应为空闲")
	}
	if _, ok := c.PickTarget("hi"); !ok {
		t.Fatal("无会话时 PickTarget 也应建立待选菜单")
	}
	if !c.Busy() {
		t.Fatal("进入待选菜单后应报告忙碌")
	}
	if !c.TryCancelPick() {
		t.Fatal("TryCancelPick 应成功")
	}
	if c.Busy() {
		t.Fatal("取消待选菜单后应恢复空闲")
	}
	// 权限菜单同样算未决交互。
	c.OpenPermissionMenu("chat", "req", "desc")
	if !c.Busy() {
		t.Fatal("权限菜单待决时应报告忙碌")
	}
}
