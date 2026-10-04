# 桌面端 GitHub Release 发布与自动升级

日期：2026-10-04  
仓库：`github.com/kaiys202212/kshell`  
范围：仅桌面端（`kshell-desktop.exe`）；TUI 不发布、不升级。

## 目标

1. 推送 `v*` 标签后，GitHub Actions 把便携 zip 发到 GitHub Release。
2. 桌面端启动时静默检查更新；有新版本再提示；用户确认后才下载。
3. 设置页可手动检查。国内优先走 GitHub 代理，失败再回退官方源。
4. 下载后校验 SHA-256，退出进程再替换可执行文件并重启。

## 非目标

- TUI 自动升级、NSIS/MSI 安装程序、后台静默下载、代码签名、Gitee 镜像仓。
- 私有仓库 / 带 Token 的代理请求。

## 方案取舍

| 方案 | 做法 | 取舍 |
|---|---|---|
| **A（采用）** | GitHub Release 为唯一真相；客户端用 URL 前缀代理链访问 API 与 asset | 无额外托管；代理失效可回退官方 |
| B | 再维护 Gitee Release | 每次发两份，运维成本高 |
| C | 引入第三方 selfupdate 库 | 难插入国内代理优先级，依赖更重 |

## 发布产物

- 触发：push tag `vX.Y.Z`（例 `v0.2.0`）。
- 资源：
  - `kshell-desktop-windows-amd64.zip`（内含 `kshell-desktop.exe`）
  - `SHA256SUMS`（`sha256sum` 两空格格式，覆盖 zip）
- 版本注入：`-ldflags -X github.com/yangk/kshell/internal/version.Version=<tag>`。
- 本地 `dev` / 空版本不检查升级。

## 代理链

公益前缀代理不稳定，内置有序列表，**先代理后官方**；某一源超时或非 2xx 立刻试下一个。默认（可测、可覆盖）：

1. `https://ghfast.top/`
2. `https://gh-proxy.com/`
3. `https://ghproxy.net/`
4. `https://mirror.ghproxy.com/`
5. 空前缀（官方 `api.github.com` / `github.com`）

拼接规则：`prefix + originalURL`，例如  
`https://ghfast.top/https://api.github.com/repos/kaiys202212/kshell/releases/latest`。

检查与下载尽量复用**第一次成功的前缀**；该前缀下载失败则继续后续源。请求带 `User-Agent: kshell`，单源超时 8s。

## 客户端行为

- `GET .../releases/latest`，解析 `tag_name`、asset 列表、`body`。
- 找到 zip 与 `SHA256SUMS`；比较语义化版本（去 `v` 前缀）。
- 仅当远端 **大于** 当前版本时 `Available=true`。
- 启动约 3s 后后台检查；有更新发 `update:available`。
- 用户点「立即升级」：下载 zip → 校验哈希 → 解出 exe → 写到 `<当前exe>.new` → 延迟脚本在进程退出后 `Move-Item` 覆盖并启动 → `quitApp`。
- 替换脚本复用现有「等 PID 退出再 Start-Process」模式，并增加覆盖 `.new`。

## UI

设置 → 通用 →「关于」：当前版本、「检查更新」、有更新时展示远端版本与说明摘要、「立即升级」、下载/失败文案。启动发现更新时用现有 notify 提示去设置页。

## 测试要点

- 代理链：第一源失败、第二源成功；全部失败报错。
- 版本比较：`v0.1.0` vs `v0.2.0`；相等不可用。
- SHA256 不匹配拒绝安装。
- `dev` 跳过检查。
- 前端：关于区、检查按钮、可用更新与升级按钮。

## 成功标准

- `v*` 标签能在 GitHub Release 看到 zip 与校验文件。
- 国内默认先打代理再打官方；代理挂了仍能从官方更新。
- 用户确认后替换桌面端并自动拉起新版本。
