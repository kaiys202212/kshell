package weixin

import "testing"

func TestMarkdownToText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"图片删除", "前![](http://x/a.png)后", "前后"},
		{"图片保留alt", "看![截图](http://x/a.png)这里", "看截图这里"},
		{"链接留文字", "见[文档](http://x/a.md)即可", "见文档即可"},
		{"围栏删标记留内容", "```go\ncode()\n```", "code()"},
		{"围栏保留内部格式", "```go\n**x** [l](u)\n```", "**x** [l](u)"},
		{"行内代码去反引号", "运行 `go test` 看看", "运行 go test 看看"},
		{"标题去井号", "## 标题一\n正文", "标题一\n正文"},
		{"加粗去星号", "**重点**内容", "重点内容"},
		{"斜体去单星", "*斜体*文字", "斜体文字"},
		{"删除线去波浪", "~~废弃~~保留", "废弃保留"},
		{"引用去箭头", "> 引用行\n普通行", "引用行\n普通行"},
		{"表格原样保留", "| a | b |\n|---|---|\n| 1 | 2 |", "| a | b |\n|---|---|\n| 1 | 2 |"},
		{"多空行压缩", "a\n\n\n\n\nb", "a\n\nb"},
		{"保留普通换行", "第一行\n第二行", "第一行\n第二行"},
		{"综合", "# 标题\n\n这是 **加粗** 与 `code`，见 [链接](url)。\n\n> 引用", "标题\n\n这是 加粗 与 code，见 链接。\n\n引用"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := markdownToText(tc.in); got != tc.want {
				t.Errorf("markdownToText(%q) = %q, 期望 %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestMarkdownToTextFenceContentUntouched(t *testing.T) {
	// 围栏内容里即使出现 # / > 等也不处理。
	in := "```bash\n# 注释\n> echo\n```\n## 真标题"
	want := "# 注释\n> echo\n真标题"
	if got := markdownToText(in); got != want {
		t.Errorf("= %q, 期望 %q", got, want)
	}
}

func TestHardBreaks(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"单换行升级为硬换行", "第一行\n第二行", "第一行  \n第二行"},
		{"段落空行不动", "段一\n\n段二", "段一\n\n段二"},
		{"连续行逐行升级", "a\nb\nc", "a  \nb  \nc"},
		{"行尾已有两空格不重复", "a  \nb", "a  \nb"},
		{"仅空白行归一为空行", "a\n \nb", "a\n\nb"},
		{"单行不加工", "只有一行", "只有一行"},
		{"空串", "", ""},
	}
	for _, c := range cases {
		if got := hardBreaks(c.in); got != c.want {
			t.Errorf("%s: hardBreaks(%q) = %q, 期望 %q", c.name, c.in, got, c.want)
		}
	}
}
