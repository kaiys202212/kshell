package weixin

import (
	"regexp"
	"strings"
)

// 微信文本消息不支持 Markdown，出站前做一次降级转换：保留信息、去掉标记。
var (
	reImage    = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)        // 图片：alt 非空时保留 alt
	reLink     = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)         // 链接：只留文字
	reInlineCp = regexp.MustCompile("`+")                            // 行内代码反引号（围栏另行走逐行保护）
	reHeading  = regexp.MustCompile(`(?m)^#{1,6}\s+`)                // 行首标题前缀
	reBold     = regexp.MustCompile(`\*\*([^*]+)\*\*`)               // 加粗
	reItalic   = regexp.MustCompile(`(^|[^*])\*([^*\n]+)\*([^*]|$)`) // 单星斜体（先去加粗，残留单星才命中）
	reStrike   = regexp.MustCompile(`~~([^~]+)~~`)                   // 删除线
	reQuote    = regexp.MustCompile(`(?m)^[ \t]*>[ \t]?`)            // 行首引用前缀
	reBlankRun = regexp.MustCompile(`\n{3,}`)                        // 3+ 连续换行压成 1 空行
)

// markdownToText 把 Markdown 降级为适合微信文本消息的纯文本。
// 规则：图片删除（保留 alt）、链接只留文字、代码围栏去标记但内容原样
// （内部不做任何转义）、行内反引号去掉、标题/加粗/斜体/删除线/引用去标记、
// 表格行与普通换行保留、3+ 空行压成 1 空行。
func markdownToText(s string) string {
	// 逐行 toggle 扫描：围栏内容段整体保护，其余段落套用降级规则。
	// parts 交替存放已转换的普通文本与待原样保留的围栏内容（用 \x00 包裹标记）。
	var parts []string
	var cur strings.Builder
	inFence := false
	flush := func(protected bool) {
		text := cur.String()
		cur.Reset()
		if !protected {
			text = transformLines(text)
			// 普通段收尾统一去尾换行，段间换行由拼接阶段补。
			text = strings.TrimSuffix(text, "\n")
		}
		if protected {
			text = "\x00" + strings.TrimSuffix(text, "\n") + "\x00"
		}
		if text != "" {
			parts = append(parts, text)
		}
	}
	for _, raw := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimLeft(raw, " \t"), "```") {
			flush(inFence) // 开围栏：刚才是普通段(false)；闭围栏：刚才是保护段(true)
			inFence = !inFence
			continue
		}
		cur.WriteString(raw)
		cur.WriteString("\n")
	}
	if inFence {
		flush(true) // 未闭合围栏：剩余内容按保护段处理
	} else {
		flush(false)
	}

	var sb strings.Builder
	for i, p := range parts {
		if i > 0 {
			sb.WriteString("\n")
		}
		if strings.HasPrefix(p, "\x00") { // 围栏内容段：剥标记原样保留
			sb.WriteString(strings.TrimSuffix(strings.TrimPrefix(p, "\x00"), "\x00"))
		} else {
			sb.WriteString(p)
		}
	}
	// 段间拼接可能残留多余空行，统一压缩后去首尾空白。
	return strings.TrimSpace(reBlankRun.ReplaceAllString(sb.String(), "\n\n"))
}

// transformLines 对非围栏文本应用除围栏外的全部降级规则。
func transformLines(s string) string {
	s = reImage.ReplaceAllString(s, "$1")
	s = reLink.ReplaceAllString(s, "$1")
	s = reInlineCp.ReplaceAllString(s, "")
	s = reHeading.ReplaceAllString(s, "")
	s = reBold.ReplaceAllString(s, "$1")
	s = reItalic.ReplaceAllString(s, "$1$2$3")
	s = reStrike.ReplaceAllString(s, "$1")
	s = reQuote.ReplaceAllString(s, "")
	s = reBlankRun.ReplaceAllString(s, "\n\n")
	return s
}
