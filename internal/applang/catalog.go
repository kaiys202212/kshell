package applang

// en / zh-CN 直显文案表。key 与前端资源无关联，只服务 Go 直显点。
var en = map[string]string{
	"tray.show_main":          "Show kshell",
	"tray.exit":               "Quit",
	"dialog.pick_project_dir": "Choose project directory",
	"toast.task_done":         "Task complete",
	"toast.task_error":        "Task failed",
	"toast.waiting_confirm":   "Waiting for confirmation",
	"terminal.title":          "Terminal",
	"role.user":               "User",
	"role.assistant":          "Assistant",
}

var zhCN = map[string]string{
	"tray.show_main":          "显示主窗口",
	"tray.exit":               "退出",
	"dialog.pick_project_dir": "选择项目目录",
	"toast.task_done":         "任务完成",
	"toast.task_error":        "任务出错",
	"toast.waiting_confirm":   "等待确认",
	"terminal.title":          "终端",
	"role.user":               "用户",
	"role.assistant":          "助手",
}
