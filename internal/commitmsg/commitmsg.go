// Package commitmsg 负责把 staged diff 变成一条可提交的 Git 提交信息：
// 提示词构造、diff 截断与模型输出清洗。纯逻辑，不依赖 desktop/UI。
package commitmsg

import (
	"strings"
	"unicode/utf8"

	"github.com/yangk/kshell/internal/hubtools"
)

const (
	// MaxDiffBytes 是发给模型的 diff 上限：留足上下文又避免撑爆请求体。
	MaxDiffBytes = 24 * 1024
	// maxSubjectRunes 是单行提交信息上限（Conventional Commits 推荐的 50/72 折中）。
	maxSubjectRunes = 72
	truncatedMarker = "\n... (diff truncated)"
)

// BuildMessages 构造生成提交信息的对话消息。lang 以 zh 开头用中文输出，
// 其余用英文；系统提示词固定中文，避免多语言模板维护漂移。
func BuildMessages(lang, diff string) []hubtools.Message {
	langName := "中文"
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "zh") {
		langName = "English"
	}
	var b strings.Builder
	b.WriteString("你是专业的 Git 提交信息生成器。根据用户提供的已暂存变更（staged diff），生成一条提交信息。\n")
	b.WriteString("要求：\n")
	b.WriteString("1. 只输出一行文本，格式为 `type: 简述`，不要代码块、引号、解释，结尾不加句号。\n")
	b.WriteString("2. type 只能是 feat / fix / refactor / perf / test / docs / build / ci / chore / style 之一。\n")
	b.WriteString("3. 简述使用" + langName + "，不超过 50 个字符，说明「做了什么/为什么」，不要罗列文件清单。\n")
	b.WriteString("4. 变更含多个主题时，选占主导的 type，简述概括整体；不要编造 diff 中不存在的内容。\n")
	return []hubtools.Message{
		{Role: "system", Content: b.String()},
		{Role: "user", Content: "已暂存变更（staged diff）：\n" + diff},
	}
}

// Sanitize 把模型回复清洗成可直接填入提交框的单行文本：
// 去代码围栏/引号/常见前缀，取首个非空行，折叠空白，去尾部句号，
// 并按 rune 截断到 maxSubjectRunes。清洗后为空由调用方视为失败。
func Sanitize(raw string) string {
	lines := strings.Split(stripFences(raw), "\n")
	first := ""
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			first = line
			break
		}
	}
	first = trimQuotes(first)
	for _, prefix := range []string{
		"提交信息：", "提交信息:", "提交说明：", "提交说明:",
		"Commit message:", "commit message:", "Message:", "message:",
	} {
		if strings.HasPrefix(first, prefix) {
			first = strings.TrimSpace(strings.TrimPrefix(first, prefix))
			break
		}
	}
	first = trimQuotes(first)
	first = strings.Join(strings.Fields(first), " ")
	first = strings.TrimSuffix(first, "。")
	first = strings.TrimSuffix(first, ".")
	first = strings.TrimSpace(first)
	if runes := []rune(first); len(runes) > maxSubjectRunes {
		first = string(runes[:maxSubjectRunes])
	}
	return first
}

// stripFences 丢掉 Markdown 代码围栏行（``` 开头），保留围栏内正文。
func stripFences(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// trimQuotes 去掉包裹首尾的常见引号与反引号。
func trimQuotes(s string) string {
	return strings.Trim(strings.TrimSpace(s), "`\"'“”‘’")
}

// TruncateDiff 超限时在最后一个换行处截断并追加说明标记，避免切坏
// 多字节 rune 或半行 diff。maxBytes <= 0 时用 MaxDiffBytes。
func TruncateDiff(text string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = MaxDiffBytes
	}
	if len(text) <= maxBytes {
		return text
	}
	cut := maxBytes - len(truncatedMarker)
	if cut <= 0 {
		cut = maxBytes
	}
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	if i := strings.LastIndexByte(text[:cut], '\n'); i > 0 {
		cut = i
	}
	return text[:cut] + truncatedMarker
}
