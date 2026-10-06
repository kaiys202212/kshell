//go:build windows

package desktop

import toast "git.sr.ht/~jackmordaunt/go-toast/v2"

// toastAppID 是 toast 的 AppUserModelID：Windows 用它归组通知中心条目，
// 与应用名保持一致的展示形态即可（无需注册表级 AppData）。
const toastAppID = "KShell"

// newAgentToast 构造一条系统通知（独立出来便于做构造层面的单测）。
func newAgentToast(title, body string) toast.Notification {
	return toast.Notification{AppID: toastAppID, Title: title, Body: body}
}

// showAgentToast 弹出 Windows 系统通知；失败返回 error，调用方降级为仅 emit。
func showAgentToast(title, body string) error {
	n := newAgentToast(title, body)
	return n.Push()
}
