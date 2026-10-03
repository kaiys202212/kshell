# 全局模型配置（对所有 agent 启动时注入）设计

- 日期：2026-10-03
- 分支：`feat/model-config`
- 状态：设计已定稿，待写实现计划

## 1. 背景与目标

现状：各 agent 的模型/端点/密钥只能改各自的配置文件（如 `~/.claude/settings.json` 的 `env` 段）。用户希望**在 kshell 设置页统一配置模型/端点/密钥，并在启动各 agent 时注入**，避免逐个改工具配置。

触发场景：用户本机 Claude 指向第三方 Anthropic 兼容端点（`https://token-plan-cn.xiaomimimo.com/anthropic`，模型 `mimo-v2.5`），token 失效（实测 401），claude 卡住。希望 kshell 能统一管理这类配置。

**已确认的决策**：

1. 配置字段：**模型 + 端点 + 密钥**。
2. 结构：**全局端点 + 密钥共享；模型名每 agent 单独填**（可留空=不改）。
3. 作用于：**仅启动时注入环境变量/参数**，不写各工具自身配置文件。
4. 覆盖：**Claude / Codex / Gemini / OpenCode 全上**（各工具映射方式不同，未实测项标注）。
5. 密钥存储：写入 `~/.kshell/config.yaml`（已是 `0600`），UI 掩码，**密钥不回传前端**。

## 2. 非目标（v1）

- 不写各工具自身的配置文件；不接系统钥匙串。
- 不支持自定义 provider（`providers.yaml`）的模型注入（接口预留）。
- 不做 per-workspace / per-session 覆盖。
- 不做运行时热更新：配置只对新启动的进程生效。

## 3. 配置模型与存储

`internal/config/config.go` 新增：

```go
// ModelConfig 是全局模型/端点配置 + 每 agent 模型名。
type ModelConfig struct {
    Enabled bool              `yaml:"enabled"`  // 总开关
    BaseURL string            `yaml:"base_url"` // 全局端点（可空）
    APIKey  string            `yaml:"api_key"`  // 全局密钥（可空）
    Agents  map[string]string `yaml:"agents"`   // toolID -> 模型名（空串=不改）
}
```

- `Config` 增加 `Model ModelConfig \`yaml:"model"\``。
- `Default()`：`Enabled:false`、`Agents` 空 map。
- `normalized()`：`Agents` 为 nil 时置空 map，各值 `TrimSpace`；`BaseURL` 非空但非 `http://`/`https://` 开头时丢弃（回落空），不阻断加载。
- 存储沿用现有 `Save`（临时文件 + 原子替换，`0600`）。

**密钥不落前端**：读取只返回 `APIKeySet bool`；写入约定「空串=保持原值」，另有 `ClearAPIKey` 显式清除。

## 4. Provider 注入接口与各 agent 映射

新增 `internal/providers/model.go`：

```go
// ModelConfig 是按工具适配后的模型注入值（全局端点/密钥 + 该工具的模型名）。
type ModelConfig struct {
    BaseURL string
    APIKey  string
    Model   string
}

// ModelInjector 可选接口：把模型配置翻译成该工具认的启动参数与环境变量。
// 只注入非空字段；工具不支持的字段忽略。
type ModelInjector interface {
    InjectModel(cfg ModelConfig) (args []string, env map[string]string)
}
```

各内置工具的映射：

| 工具 | 模型 | 端点 | 密钥 | 状态 |
|---|---|---|---|---|
| Claude | `ANTHROPIC_MODEL` + `ANTHROPIC_DEFAULT_{SONNET,OPUS,HAIKU}_MODEL` | `ANTHROPIC_BASE_URL` | `ANTHROPIC_AUTH_TOKEN` | 已确认 |
| Codex | 参数 `-m <model>` | `OPENAI_BASE_URL` | `OPENAI_API_KEY` | 未实测 |
| Gemini | 参数 `--model <model>` | `GOOGLE_GEMINI_BASE_URL` | `GEMINI_API_KEY` | 未实测 |
| OpenCode | 参数 `--model <model>`（期望 `provider/model`） | 无稳定 env | 无稳定 env | 未实测；端点/密钥 v1 不支持 |

要点：

- Claude 同步三个 `DEFAULT_*` 别名：代理端点通常只认单一模型，避免面板选中 sonnet/opus 时命中不存在的模型。
- 只注入非空字段；某项留空即不传，不覆盖工具默认。
- 未实测映射先按已知/约定实现；实测校准只改对应 provider 的方法，不影响其它层。

## 5. launch 集成（终端 + ACP 两条路径都注入）

`internal/launch/launch.go` 新增：

```go
// ModelOptions 描述模型注入：Resolver 按 toolID 返回该工具应注入的配置（nil 表示不注入）。
type ModelOptions struct {
    Resolver func(toolID string) (providers.ModelConfig, bool)
}

// applyModel 与现有 applyTheme 同构：provider 实现 ModelInjector 时追加参数/环境变量。
func applyModel(l providers.Launch, p providers.Provider, mo ModelOptions) providers.Launch {
    if mo.Resolver == nil {
        return l
    }
    cfg, ok := mo.Resolver(p.ID())
    if !ok {
        return l
    }
    if inj, ok := p.(providers.ModelInjector); ok {
        args, env := inj.InjectModel(cfg)
        l.Args = append(l.Args, args...)
        l.Env = providers.MergeEnv(l.Env, env)
    }
    return l
}
```

签名扩展（尾部加 `mo ModelOptions`）：

- `ForSession(ps, tools, s, to, mo)`、`ForWorkspace(ps, tools, ws, to, mo)`、`ForWorkspaceTool(ps, tools, ws, toolID, to, mo)`：`applyModel(applyTheme(...))`。
- `ForSessionACP(ps, tools, s, mo)`、`ForWorkspaceACP(ps, tools, ws, toolID, mo)`：需按 toolID 取到 provider（用于 `InjectModel`），`applyModel(acpLaunch(...))`。

调用点更新（6 处，TUI + 桌面）：

- `internal/desktop/terminal.go`：`OpenSessionTerminal` / `OpenWorkspaceTerminal`。
- `internal/desktop/chat.go`：`OpenSession` / `OpenWorkspace`（ACP）。
- `internal/ui/*`：TUI 的 `ForSession` / `ForWorkspace`。

桌面与 TUI 各加 `modelOptions()`（对称于现有 `themeOptions()`）：

```go
func (a *App) modelOptions() launch.ModelOptions {
    m := a.snapshot().Config.Model
    if !m.Enabled {
        return launch.ModelOptions{}
    }
    return launch.ModelOptions{Resolver: func(toolID string) (providers.ModelConfig, bool) {
        return providers.ModelConfig{BaseURL: m.BaseURL, APIKey: m.APIKey, Model: m.Agents[toolID]}, true
    }}
}
```

数据流：`Config.Model` → resolver → `InjectModel` → `Launch.Args/Env` → `launcher.Build` →（终端）`terminal.Spec.Env` /（聊天）`chat.Spec.Env` → `acp.Spec.Env` → 子进程。仅对新启动进程生效。

## 6. 桌面绑定与设置页 UI

`internal/desktop/settings.go`：

```go
type ModelConfigView struct {
    Enabled   bool              `json:"Enabled"`
    BaseURL   string            `json:"BaseURL"`
    Agents    map[string]string `json:"Agents"`
    APIKeySet bool              `json:"APIKeySet"`
}
type ModelConfigInput struct {
    Enabled     bool              `json:"Enabled"`
    BaseURL     string            `json:"BaseURL"`
    APIKey      string            `json:"APIKey"` // 空=保持
    ClearAPIKey bool              `json:"ClearAPIKey"`
    Agents      map[string]string `json:"Agents"`
}
func (a *App) GetModelConfig() (ModelConfigView, error)
func (a *App) SetModelConfig(in ModelConfigInput) error
```

- `SetModelConfig` 走 `saveConfig` 串行化；`ClearAPIKey` → 置空；`APIKey != ""` → 覆盖；否则保持。
- `BaseURL` 非空且非 `http(s)://` 开头 → 报错、不落盘。

`frontend/src/lib/api.ts`：`ModelConfigView` / `ModelConfigInput` 类型 + `getModelConfig` / `setModelConfig` 封装 + 绑定接口。

`frontend/src/pages/Settings.tsx` 新增「模型」区：

- 启用开关；Base URL 输入；API Key（`type=password`，占位随 `APIKeySet` 变，「清除密钥」勾选框，**从不回显密钥**）。
- 每 agent 模型名：固定列出 Claude Code / Codex CLI / Gemini CLI / OpenCode 四行，值取 `Agents[id]`。
- 保存按钮 → `setModelConfig`，成功提示「已保存，对新启动的会话生效」；失败 toast。
- 说明文字：启动时注入（不改工具自身配置）；Claude 受 `~/.claude/settings.json` 的 `env` 影响，若不生效需先清掉该段。

## 7. 边界与错误处理

- `BaseURL` 校验失败 → 报错、不落盘。
- 密钥前后端都不回显；`config.yaml` 维持 `0600`。
- `Enabled=false` → resolver 为空，完全不注入；空字段不注入。
- 未安装工具不受影响；ACP 不可用回退终端时同样注入（两条路径都过 `applyModel`）。
- 只影响新启动进程；改配置后需重开页签/会话；配置损坏沿用现有「备份重建」。

## 8. 风险与验证点

1. **（关键）Claude 注入可能被 `~/.claude/settings.json` 的 `env` 段覆盖** —— 必须实测优先级；若 settings.json 胜出，需先清掉其 `env` 段，kshell 注入才生效。
2. Codex `OPENAI_BASE_URL`/`OPENAI_API_KEY`、Gemini `GOOGLE_GEMINI_BASE_URL`、OpenCode 的端点/密钥：未实测，按约定实现并标注。
3. OpenCode `--model` 是否受支持、是否要求 `provider/model` 形式：未实测。
4. 密钥明文 `0600`（Windows 上 chmod 语义有限）——与现有配置同口径。

## 9. 测试策略

- **Go 单测**：`config` 默认/归一（非法 URL 丢弃）；`GetModelConfig` 不泄漏密钥、`SetModelConfig`（保持/覆盖/清除/非法 URL）；各内置 `InjectModel` 映射（只注入非空）；`launch.applyModel` 在终端与 ACP 两路径的 args/env 合并与 toolID 路由。
- **前端 vitest**：设置页「模型」区加载回填、密钥掩码、留空保存传空串、勾选清除传 `ClearAPIKey=true`。
- **冒烟**：Windows 真机在设置页填端点/密钥/模型（先清掉 `~/.claude/settings.json` 的 `env`），验证新会话命中新端点；其余工具按已知映射抽查。

## 10. 里程碑（供实现计划拆解）

- M1 `config.ModelConfig` + 归一 + 单测。
- M2 `providers.ModelInjector` + 四个内置映射 + 单测。
- M3 `launch.applyModel` + 签名扩展 + 6 处调用点 + 单测。
- M4 desktop `GetModelConfig`/`SetModelConfig` + 单测。
- M5 前端 api + 设置页「模型」区 + vitest。
- M6 冒烟（真机，尤其验证 Claude 注入优先级）。
