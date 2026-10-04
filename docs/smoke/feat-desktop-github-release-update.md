# 冒烟：feat/desktop-github-release-update

- 设置 → 通用 → 关于：能看到当前版本；点「检查更新」不崩溃（开发构建可提示不检查）。
- 打 tag `vX.Y.Z` 并推送后，GitHub Release 出现 `kshell-desktop-windows-amd64.zip` 与 `SHA256SUMS`。
- 正式版本启动数秒后若有更新，出现「发现新版本」提示；设置里确认「立即升级」后进程退出并拉起新 exe。
- 断网或代理失败时仍会尝试后续源直至官方 GitHub。
