package applang

// en / zh-CN 直显文案表。key 与前端资源无关联，只服务 Go 直显点。
var en = map[string]string{
	"tray.show_main":          "Show kshell",
	"tray.show_main_tip":      "Show kshell main window",
	"tray.exit":               "Quit",
	"tray.exit_tip":           "Quit kshell",
	"dialog.pick_project_dir": "Choose project directory",
	"toast.task_done":         "Task complete",
	"toast.task_error":        "Task failed",
	"toast.waiting_confirm":   "Waiting for confirmation",
	"terminal.title":          "Terminal",
	"role.user":               "User",
	"role.assistant":          "Assistant",
	"transcript.sqlite_body":  `This session is stored in a database; the full conversation cannot be expanded as a file. Click "Activate" to resume, then view it.`,
	"transcript.empty_body":   "No conversation content could be parsed. Click \"Activate\" to resume.",
}

var zhCN = map[string]string{
	"tray.show_main":          "显示主窗口",
	"tray.show_main_tip":      "显示 kshell 主窗口",
	"tray.exit":               "退出",
	"tray.exit_tip":           "退出 kshell",
	"dialog.pick_project_dir": "选择项目目录",
	"toast.task_done":         "任务完成",
	"toast.task_error":        "任务出错",
	"toast.waiting_confirm":   "等待确认",
	"terminal.title":          "终端",
	"role.user":               "用户",
	"role.assistant":          "助手",
	"transcript.sqlite_body":  "该会话保存在数据库中，无法以文件形式展开完整对话。请点击「激活」恢复后再查看。",
	"transcript.empty_body":   "未解析到对话正文。请点击「激活」恢复后查看。",
}
