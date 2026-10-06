package providers

import (
	"regexp"
	"strings"
	"unicode"
)

// dropBlockTags 是「整块都不属于用户手写内容」的包装标签：标签连同内容一起删掉。
// 这些块是各 CLI 自己注入的机器上下文（运行提示、斜杠命令回显、IDE 选中信息、
// 附件清单一类），留在标题里只会得到 "<local-command-caveat>Caveat: ..." 这种噪音。
var dropBlockTags = []string{
	"local-command-caveat",
	"local-command-stdout",
	"command-name",
	"command-message",
	"timestamp", // Codex 在每条用户消息前置的本地时间戳块
	"system-reminder",
	"user_info",
	"rules",
	"additional_data",
	"manually_attached_skills",
	"external_links",
	"environment_context",
	"user_instructions",
	"ide_opened_file",
	"ide_selection",
}

// unwrapTags 是「包着真实用户输入」的标签：只删标签标记、保留里面的文本。
// 例如 CodeBuddy 把用户问题包在 <user_query> 里，斜杠命令的参数包在 <command-args> 里。
var unwrapTags = []string{
	"user_query",
	"user_message",
	"command-args",
}

// 成对块（可跨行，非贪婪）；未闭合的标签只删标记本身、不吞后面的正文。
var blockRes = buildBlockRes()
var tagOnlyRe = buildTagOnlyRe()

// oscColorReplyRe 匹配终端 OSC 4/10/11 查色应答正文（ESC ] 已被剥掉后的残片）。
// 例：4;0;rgb:2e2e/3434/3636、11;rgb:aaaa/bbbb/cccc
var oscColorReplyRe = regexp.MustCompile(`(?i)^\d+(?:;\d+)*;rgb:[0-9a-f./]+$`)

func buildBlockRes() []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(dropBlockTags))
	for _, tag := range dropBlockTags {
		out = append(out, regexp.MustCompile(`(?is)<`+tag+`\b[^>]*>.*?</`+tag+`\s*>`))
	}
	return out
}

func buildTagOnlyRe() *regexp.Regexp {
	names := make([]string, 0, len(dropBlockTags)+len(unwrapTags))
	names = append(names, dropBlockTags...)
	names = append(names, unwrapTags...)
	// 只匹配这些已知标签的开/闭/自闭合标记，绝不碰用户正文里其它尖括号内容
	return regexp.MustCompile(`(?is)</?(?:` + strings.Join(names, "|") + `)\b[^>]*/?>`)
}

// ClipPromptTitle 把用户刚提交的内容收成页签标题：只取第一行，清洗包装标签，最长 48 个字。
func ClipPromptTitle(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if i := strings.IndexAny(raw, "\r\n"); i >= 0 {
		raw = strings.TrimSpace(raw[:i])
	}
	s := cleanTitle(raw)
	r := []rune(s)
	const max = 48
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// cleanTitle 清洗会话标题候选文本：
//  1. 去掉被截断的开头包装块（会话文件只读头部，长块没有闭合标签是常态）；
//  2. 删掉 dropBlockTags 的整个块（含内容）；
//  3. 去掉 unwrapTags 及未闭合 known 标签的标记（保留内容）；
//  4. 折叠空白。
//
// 返回空串表示「这段文本全是机器注入的包装内容，不适合当标题」，调用方应跳过该候选。
func cleanTitle(raw string) string {
	if raw == "" {
		return ""
	}
	s := raw
	// 循环删除以处理嵌套（如 <additional_data> 里还嵌着 <rules>），上限防病态输入
	for i := 0; i < 4; i++ {
		next := dropTruncatedLeadingBlock(s)
		for _, re := range blockRes {
			next = re.ReplaceAllString(next, " ")
		}
		next = tagOnlyRe.ReplaceAllString(next, " ")
		if next == s {
			break
		}
		s = next
	}
	out := strings.Join(strings.Fields(s), " ")
	if oscColorReplyRe.MatchString(out) {
		return ""
	}
	return out
}

// dropTruncatedLeadingBlock 处理「正文以已知包装标签开头、但没有对应的闭合标签」的情况：
// 这几乎只发生在读取头部被截断时（长包装块被截掉尾巴），此时整段都不是用户正文，返回空串。
// 只认开头位置：正文中间出现的未闭合标签不吞后续内容。
func dropTruncatedLeadingBlock(s string) string {
	trimmed := strings.TrimLeft(s, " \t\r\n")
	if !strings.HasPrefix(trimmed, "<") {
		return s
	}
	lower := strings.ToLower(trimmed)
	for _, tag := range dropBlockTags {
		open := "<" + tag
		if !strings.HasPrefix(lower, open) {
			continue
		}
		rest := lower[len(open):]
		// 标签名边界：避免 <rulesfoo> 误命中 rules
		if rest != "" && rest[0] != '>' && rest[0] != '/' && !unicode.IsSpace(rune(rest[0])) {
			continue
		}
		if !strings.Contains(lower, "</"+tag) {
			return ""
		}
	}
	return s
}
