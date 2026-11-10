package desktop

import (
	"context"
	"strings"
	"time"

	"github.com/yangk/kshell/internal/workbuddy"
)

// WorkbuddyModelView 是扫描到的原生模型候选（设置页勾选清单的一行）。
type WorkbuddyModelView struct {
	ID            string `json:"ID"`
	Name          string `json:"Name"`
	ContextWindow int    `json:"ContextWindow"`
}

// WorkbuddyScanView 是「从 WorkBuddy 导入」扫描的结果视图。
// Installed=本机装了 WorkBuddy（凭据文件存在）；CredentialOK=凭据可用。
// ErrKey 为稳定错误 key（前端 translateBackend 渲染），成功时为空。
type WorkbuddyScanView struct {
	Installed    bool                 `json:"Installed"`
	CredentialOK bool                 `json:"CredentialOK"`
	ErrKey       string               `json:"ErrKey"`
	Region       string               `json:"Region"`
	Models       []WorkbuddyModelView `json:"Models"`
}

// workbuddyScanOnError 把凭据解析失败映射为扫描视图：
// 未装/exe 缺失都归为未安装（设置页隐藏入口），其余视为装了但凭据不可用。
func workbuddyScanOnError(err error) WorkbuddyScanView {
	msg := err.Error()
	installed := !strings.Contains(msg, "err.workbuddy.not_installed") &&
		!strings.Contains(msg, "err.workbuddy.exe_not_found")
	return WorkbuddyScanView{Installed: installed, ErrKey: msg}
}

// workbuddyScanCredentialState 校验解出的凭据当前是否可用；
// 不可用返回 (视图, false)，ErrKey 固定 token_expired。
func workbuddyScanCredentialState(cred workbuddy.Credential) (WorkbuddyScanView, bool) {
	if cred.ExpiresAtMs > 0 && time.Now().UnixMilli() >= cred.ExpiresAtMs {
		return WorkbuddyScanView{
			Installed: true,
			ErrKey:    "err.workbuddy.token_expired",
		}, false
	}
	return WorkbuddyScanView{}, true
}

// scanWorkbuddyPlatform 执行一次完整扫描：凭据 → 目录。只读不改任何
// WorkBuddy 侧文件；结果不缓存（秒级开销，且凭据/目录都可能被 app 刷新）。
func scanWorkbuddyPlatform() WorkbuddyScanView {
	if !workbuddy.AuthFilesExist() {
		return WorkbuddyScanView{ErrKey: "err.workbuddy.not_installed"}
	}
	cred, err := workbuddy.LoadCredential()
	if err != nil {
		return workbuddyScanOnError(err)
	}
	view, ok := workbuddyScanCredentialState(cred)
	if !ok {
		return view
	}
	models, err := workbuddy.FetchModels(context.Background(), cred, nil)
	if err != nil {
		return WorkbuddyScanView{
			Installed:    true,
			CredentialOK: true,
			ErrKey:       err.Error(),
			Region:       cred.Region(),
			Models:       []WorkbuddyModelView{},
		}
	}
	out := make([]WorkbuddyModelView, 0, len(models))
	for _, m := range models {
		out = append(out, WorkbuddyModelView{ID: m.ID, Name: m.Name, ContextWindow: m.ContextWindow})
	}
	return WorkbuddyScanView{
		Installed:    true,
		CredentialOK: true,
		Region:       cred.Region(),
		Models:       out,
	}
}

// ScanWorkbuddyPlatform 是设置页「从 WorkBuddy 导入」的扫描入口。
func (a *App) ScanWorkbuddyPlatform() WorkbuddyScanView {
	return scanWorkbuddyPlatform()
}
