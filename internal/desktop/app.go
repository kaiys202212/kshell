// Package desktop 是 Wails 桌面版的装配与绑定层：薄封装现有核心包暴露给前端。
// 业务逻辑一律下沉到 discovery/providers/remote/workspace 等包，本包只做参数组装与校验。
// 注意：本包不加构建标签（便于 go test 直接覆盖）；embed 前端资源在根包 main_wails.go（desktop 标签）。
package desktop

import "context"

// App 持有绑定层状态；字段在后续任务按需补充。
type App struct{}

// NewApp 创建绑定对象。
func NewApp() *App { return &App{} }

// Startup 由 Wails 在窗口启动后调用（Task 3 接入扫描与事件推送）。
func (a *App) Startup(ctx context.Context) { _ = ctx }
