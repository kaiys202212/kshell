package desktop

import (
	"context"
	"strings"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// pickDirectory / pickFile 是原生选择器的注入点：测试替换掉它们，避免真弹窗卡住用例。
var pickDirectory = func(ctx context.Context, title string) (string, error) {
	if ctx == nil {
		return "", errNotReady
	}
	return wailsRuntime.OpenDirectoryDialog(ctx, wailsRuntime.OpenDialogOptions{
		Title:                title,
		CanCreateDirectories: true,
	})
}

var pickFile = func(ctx context.Context, title string) (string, error) {
	if ctx == nil {
		return "", errNotReady
	}
	return wailsRuntime.OpenFileDialog(ctx, wailsRuntime.OpenDialogOptions{
		Title: title,
	})
}

func (a *App) dialogContext() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ctx
}

// PickDirectory 弹出原生文件夹选择器；用户取消返回空串。
func (a *App) PickDirectory(title string) (string, error) {
	dir, err := pickDirectory(a.dialogContext(), title)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(dir), nil
}

// PickFile 弹出原生文件选择器；用户取消返回空串。
func (a *App) PickFile(title string) (string, error) {
	file, err := pickFile(a.dialogContext(), title)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(file), nil
}
