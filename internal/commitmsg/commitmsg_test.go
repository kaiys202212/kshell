package commitmsg

import (
	"strings"
	"testing"
)

func TestSanitize(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"原样单行", "feat: 支持提交信息生成", "feat: 支持提交信息生成"},
		{"代码围栏", "```\nfix: 修复按钮禁用\n```", "fix: 修复按钮禁用"},
		{"带语言围栏", "```text\nrefactor: 拆分生成逻辑\n```", "refactor: 拆分生成逻辑"},
		{"首尾引号", "\"docs: 更新说明\"", "docs: 更新说明"},
		{"行内反引号", "`chore: 调整配置`", "chore: 调整配置"},
		{"中文前缀", "提交信息：chore: 调整配置", "chore: 调整配置"},
		{"英文前缀", "Commit message: perf: 优化加载", "perf: 优化加载"},
		{"多行只取首行", "feat: 首行\n\n正文解释", "feat: 首行"},
		{"前置空行", "\n\n  perf: 优化加载  \n", "perf: 优化加载"},
		{"中文句号结尾", "fix: 修复崩溃。", "fix: 修复崩溃"},
		{"英文句号结尾", "fix: handle nil.", "fix: handle nil"},
		{"空输出", "```\n```", ""},
		{"纯空白", "   \n\t", ""},
		{"折叠多余空格", "feat:   多个   空格", "feat: 多个 空格"},
		{"超长按 rune 截断", "feat: " + strings.Repeat("长", 100), "feat: " + strings.Repeat("长", 66)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Sanitize(c.in); got != c.want {
				t.Fatalf("Sanitize(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestBuildMessages_LanguageAndDiff(t *testing.T) {
	diff := "diff --git a/a.go b/a.go\n+new line"
	zh := BuildMessages("zh-CN", diff)
	if len(zh) != 2 {
		t.Fatalf("期望 system+user 两条消息，得到 %d", len(zh))
	}
	if zh[0].Role != "system" || !strings.Contains(zh[0].Content, "中文") {
		t.Fatalf("中文 system 消息异常: %+v", zh[0])
	}
	for _, typ := range []string{"feat", "fix", "refactor", "chore"} {
		if !strings.Contains(zh[0].Content, typ) {
			t.Fatalf("system 提示词缺 type %q", typ)
		}
	}
	if !strings.Contains(zh[0].Content, "只输出一行") {
		t.Fatal("system 提示词应约束单行输出")
	}
	if zh[1].Role != "user" || !strings.Contains(zh[1].Content, diff) {
		t.Fatalf("user 消息应包含 diff: %+v", zh[1])
	}

	en := BuildMessages("en", diff)
	if !strings.Contains(en[0].Content, "English") {
		t.Fatalf("英文 system 消息异常: %+v", en[0])
	}
}

func TestTruncateDiff(t *testing.T) {
	short := "diff --git a/a b/a\n+hi\n"
	if got := TruncateDiff(short, MaxDiffBytes); got != short {
		t.Fatalf("未超限不应改动: %q", got)
	}

	long := "diff --git a/a b/a\n" + strings.Repeat("+"+strings.Repeat("x", 99)+"\n", 400)
	got := TruncateDiff(long, 1024)
	if len(got) > 1024+len("\n... (diff truncated)") {
		t.Fatalf("截断后仍过长: %d", len(got))
	}
	if !strings.Contains(got, "diff --git a/a b/a") || !strings.HasSuffix(got, "... (diff truncated)") {
		t.Fatalf("截断结果异常: %.80q", got)
	}

	// 中文多字节：截断点不能切坏 rune。
	cn := strings.Repeat("中", 300)
	got = TruncateDiff(cn, 101)
	if !strings.HasSuffix(got, "... (diff truncated)") {
		t.Fatalf("应附加截断标记: %q", got)
	}
	if !strings.Contains(got, "中") {
		t.Fatal("截断后应保留有效内容")
	}
}

func TestMaxDiffBytes(t *testing.T) {
	if MaxDiffBytes != 24*1024 {
		t.Fatalf("MaxDiffBytes = %d", MaxDiffBytes)
	}
	if maxSubjectRunes != 72 {
		t.Fatalf("maxSubjectRunes = %d", maxSubjectRunes)
	}
}
