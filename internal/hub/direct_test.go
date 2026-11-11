package hub

import "testing"

func TestIsDirectPass(t *testing.T) {
	def := DefaultDirectPatterns() // ["#","@","continue","explain"]
	cases := []struct {
		in   string
		want bool
	}{
		{"#g 1,2 hi", true},       // # 前缀(命令族整体直通,knownCommands 判定留给 Handle)
		{"＃st", true},             // 全角 # 也放行
		{"@1 帮我看看", true},       // @ 指名
		{"continue", true},        // 首词
		{"Continue please", true}, // 首词大小写不敏感
		{"explain this", true},
		{"你好", false},
		{"1", false},            // 纯数字进路由(语境由路由器决断)
		{"continuation", false}, // 非首词完整匹配
		{"", false},
	}
	for _, tc := range cases {
		if got := IsDirectPass(tc.in, def); got != tc.want {
			t.Fatalf("IsDirectPass(%q)=%v, want %v", tc.in, got, tc.want)
		}
	}
	// 用户自定义 pattern: 前缀匹配
	if !IsDirectPass("部署一下 prod", []string{"部署"}) {
		t.Fatalf("自定义前缀未命中")
	}
}

func TestDefaultDirectPatterns(t *testing.T) {
	got := DefaultDirectPatterns()
	for _, want := range []string{"#", "@", "continue", "explain"} {
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
}
