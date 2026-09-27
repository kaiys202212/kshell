package ui

import "strings"

// helpContent 是帮助面板的键位表；数据与实际按键集中在 handleKey，避免帮助与实现漂移。
var helpContent = []string{
	"键位",
	"",
	"Tab / 1 / 2 / 3   切换 Sessions / Files / Remote 视图",
	"↑↓ / j k          移动光标",
	"⏎                 进入（工作区→会话；会话→恢复；目录→展开）",
	"Esc               返回上一层 / 取消",
	"/                 搜索过滤（作用在当前聚焦的列表上）",
	"n                 新建会话（Files 视图会带上上下文篮里的文件）",
	"r                 重新扫描工作区与会话",
	"",
	"Files 视图",
	"space             把文件加入/移出上下文篮（新建会话时注入）",
	"a                 切换「显示全部」（忽略 .gitignore）",
	"",
	"Remote 视图",
	"i                 扫描工作区里的连接并进入导入界面",
	"space / 回车      勾选候选 / 导入勾选项",
	"x                 输入命令并在远程执行",
	"s                 进入交互式 shell（退出后回到 kshell）",
	"t                 连通性测试",
	"b / d             绑定到当前工作区 / 删除连接",
	"",
	"?                 显示/关闭本帮助",
	"q                 退出 kshell",
}

func renderHelp(height, width int, theme Theme) string {
	lines := make([]string, 0, len(helpContent)+1)
	for _, line := range helpContent {
		if line == "键位" {
			lines = append(lines, theme.Header.Render(line))
			continue
		}
		if strings.HasSuffix(line, "视图") {
			lines = append(lines, theme.TabActive.Render(line))
			continue
		}
		lines = append(lines, theme.Body.Render(line))
	}
	return fitBlock(lines, height, width)
}
