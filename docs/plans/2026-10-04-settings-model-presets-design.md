# 设计：设置页布局 + 模型预设 + 会话/权限模式

日期：2026-10-04  
分支：`feat/settings-model-presets`  
状态：已定稿

## 背景与目标

设置页需支持：

1. 布局分区（通用 / 模型 / 工具）
2. 外观默认深色
3. 模型提供商预设（双协议 Base URL，按 agent 注入）
4. 默认会话模式 TUI/ACP（可配，默认 TUI）
5. 权限模式 default/bypass（CLI 注入 + ACP 自动放行）

## 已确认决策

| 项 | 选择 |
|---|---|
| 实现路径 | 内置预设表扩展现有 ModelConfig（不引第三方 LLM SDK） |
| 模型 UX | 下拉选预设 → 填双 URL + 推荐模型，可手改；密钥手填 |
| 外观默认 | `dark` |
| 会话模式 | 全局 `tui`/`acp`；只影响新建/恢复默认路径；仍可手动开 ACP |
| 权限 | `default`/`bypass`；bypass = CLI 注入 + ACP 弹窗自动放行 |
| 预设范围 | OpenAI、DeepSeek、MiniMax、Qwen、Kimi、GLM、MiMo、OpenRouter、自定义 |
| Codex bypass | `--ask-for-approval never`（保留 sandbox，不用 `--yolo`） |

## 1. 配置

`~/.kshell/config.yaml` 扩展：

- `appearance.mode` 默认 `dark`
- `session_mode`: `tui` | `acp`（默认 `tui`）
- `permission_mode`: `default` | `bypass`（默认 `default`）
- `model.preset`、`model.openai_base_url`、`model.anthropic_base_url`
- 旧 `model.base_url`：加载时填入两份新字段；保存不再写出（`omitempty`）

## 2. 预设与注入

Go 内嵌预设表。`providers.ModelConfig` 含双 URL；Claude 用 Anthropic，Codex 用 OpenAI，Gemini 优先 OpenAI。

## 3. 会话模式

`OpenSession`/`OpenWorkspace`：仅当 `session_mode==acp` 且工具 ACP 可用时走聊天。  
显式 `OpenWorkspaceACP`/`OpenSessionACP` 忽略偏好。

## 4. 权限 bypass

- Claude: `--dangerously-skip-permissions`
- Codex: `--ask-for-approval never`
- Gemini/OpenCode: 无稳定 flag 则 no-op
- ACP：`Manager` 自动选 allow 类 option，不发 `chat:permission`

## 5. 设置页 UI

左导航 + 右内容（窄屏单列）：通用 / 模型 / 工具。

## 非目标

- 多套模型档案、远端预设、钥匙串
- 写各 agent 自身配置文件
- OpenCode 端点/密钥注入
