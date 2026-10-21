package appdocs

import (
	"strings"
	"testing"
)

// TestGetEmbedDocsNonEmpty：四个内嵌文档都能取到且非空（embed 路径写错时这里先挂）。
func TestGetEmbedDocsNonEmpty(t *testing.T) {
	cases := []struct{ kind, lang string }{
		{"help", "zh-CN"},
		{"help", "en"},
		{"changelog", "zh-CN"},
		{"changelog", "en"},
	}
	for _, c := range cases {
		got := Get(c.kind, c.lang)
		if strings.TrimSpace(got) == "" {
			t.Errorf("Get(%q, %q) 返回空内容", c.kind, c.lang)
		}
	}
}

// TestGetLangFallback：未知语言（含空串）回退英文版内容。
func TestGetLangFallback(t *testing.T) {
	for _, lang := range []string{"fr", "zh", "ZH-CN", ""} {
		if got := Get("help", lang); got != Get("help", "en") {
			t.Errorf("Get(help, %q) 应回退英文版", lang)
		}
		if got := Get("changelog", lang); got != Get("changelog", "en") {
			t.Errorf("Get(changelog, %q) 应回退英文版", lang)
		}
	}
}

// TestGetKindRouting：kind 精确路由到对应文档；非法 kind 返回空串。
func TestGetKindRouting(t *testing.T) {
	help := Get("help", "en")
	changelog := Get("changelog", "en")
	if help == changelog {
		t.Fatal("help 与 changelog 应返回不同文档")
	}
	if !strings.Contains(changelog, "## v") {
		t.Error("changelog 应包含版本小节（## vX.Y.Z）")
	}
	if got := Get("readme", "en"); got != "" {
		t.Errorf("非法 kind 应返回空串，got %q", got)
	}
	if got := Get("", "en"); got != "" {
		t.Errorf("空 kind 应返回空串，got %q", got)
	}
}

// TestGetZhCNReturnsChinese：zh-CN 返回中文内容——用稳定标记断言，不硬编码全文。
func TestGetZhCNReturnsChinese(t *testing.T) {
	if got := Get("help", "zh-CN"); !strings.Contains(got, "帮助") {
		t.Error("中文 help 应包含「帮助」标记")
	}
	if got := Get("changelog", "zh-CN"); !strings.Contains(got, "## v") {
		t.Error("中文 changelog 应包含版本小节（## vX.Y.Z）")
	}
}

// TestHelpDocDetail：help 是详细使用说明，按「约 300 行中文」量级设下限，防止占位稿混入。
func TestHelpDocDetail(t *testing.T) {
	zh := Get("help", "zh-CN")
	en := Get("help", "en")
	if lines := strings.Count(zh, "\n"); lines < 200 {
		t.Errorf("中文 help 应为详细说明（不少于 200 行），got %d 行", lines)
	}
	if len(en) < len(zh)/3 {
		t.Error("英文 help 篇幅与中文版严重不符，疑似未完成翻译")
	}
}
