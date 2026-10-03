# kshell 颜色模式配置（跟随系统明暗）设计

日期：2026-10-03
状态：已确认（用户拍板 4 项决策）

## 1. 背景与目标

期望 kshell 支持颜色模式配置，可跟随操作系统明暗（浅色/深色）。范围包括：

1. **kshell 自身所有界面**：桌面 React UI、内嵌 xterm 终端、Wails 原生窗口、TUI（Bubbletea）。
2. **kshell 启动的 agent 工具 TUI**：claude / codex / gemini / opencode / generic，其 TUI 也跟随系统明暗。

已确认决策：

| 决策点 | 结论 |
|---|---|
| 模式取值 | `system`（默认）/ `light` / `dark`，默认跟随系统，允许手动强制覆盖 |
| 配置入口 | 桌面 Settings 页新增「外观」选项，即时生效并写回 `~/.kshell/config.yaml`；也支持手动编辑 YAML。TUI 只读配置 |
| agent 注入强度 | 只注入**环境变量 / 启动参数**（含指向 kshell 生成文件的环境变量），**绝不修改 agent 工具自身的用户配置文件** |
| 工具范围 | 全部：claude、codex、gemini、opencode、generic |
| 运行时生效范围 | 桌面 UI + 内嵌 xterm 实时切换；外部终端窗口与已启动的 agent TUI **下次启动**生效 |

## 2. 现状

- 配置无任何外观字段（`internal/config/config.go:21` `Config`），且 `config.Save`（`config.go:80`）目前只在语法损坏重建时被调用，没有面向用户的写回通路。
- TUI 配色硬编码 ANSI 色号，仅认 `NO_COLOR`（`internal/ui/theme.go:22-57`）。
- 桌面端仅靠 WebView2 的 `prefers-color-scheme` 跟随系统（`frontend/src/style.css:56`），无手动切换、无 `data-theme` 基础；xterm 主题也读 `matchMedia`（`frontend/src/components/TerminalView.tsx:29-39`）。
- 没有 Go 侧 OS 明暗检测（无注册表 / `DwmSetWindowAttribute` / `uxtheme` 调用）。
- 启动 agent 时不注入任何颜色相关环境变量（`internal/launch`、`internal/terminal`、`internal/desktop/window_win.go`），`terminal.Spec.Env`（`internal/terminal/manager.go:56`）始终为空。

## 3. 方案选型

采用**方案 A**：新增 `internal/appearance` 作为唯一主题源，各界面（桌面前端、TUI、xterm、Wails 原生窗口、agent 启动）只做「读取 + 呈现」的薄适配。

- 备选 B（把解析塞进 config、各界面各自读）会令 OS 检测、变化监听、工具注入散落各处，重复且难测。
- 备选 C（只做 CSS/终端跟随、不注入 agent）与需求冲突。

## 4. 设计

### 4.1 配置与 `appearance` 核心

**配置（`internal/config`）**

```go
type Appearance struct {
    Mode string `yaml:"mode"` // system | light | dark
}
```

- 加入 `Config`；`Default()` 置 `mode: system`；`normalized()` 校验非法值回落 `system`。
- 复用现有包函数 `Save(p Layout, c Config) error`（`config.go:80`）持久化；写回动作由桌面 App 负责（见 4.2），config 包不新增专用 setter。

**新包 `internal/appearance`（唯一主题源）**

- `type Mode string`：`System | Light | Dark`。
- `type Theme string`：`Light | Dark`。
- `ParseMode(string) Mode`：非法即 `System`。
- `Resolve(mode Mode) Theme`：`light/dark` 直接返回；`system` 走 OS 检测。
- OS 检测（构建约束拆分）：
  - `detect_windows.go`：读注册表 `HKCU\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize\AppsUseLightTheme`（`1`=浅、`0`=深），失败兜底深色。
  - `detect_other.go`：兜底深色（TUI 另有终端探测，见 4.3）。
  - 检测函数抽成可替换的包级变量，便于单测。
- `Watcher`：仅当 mode=`system` 时启动，轮询注册表（约 3s）检测变化，变化时回调；`ctx` 取消即停。桌面用它做实时切换；非 Windows 不启动。
- 通用注入变量拼装：`COLORFGBG`（深色 `15;0`、浅色 `0;15`）+ `COLORTERM=truecolor`。

> TUI 的 `system` 特例：TUI 输出到终端，`system` 用终端实际背景判定（`lipgloss.HasDarkBackground()`，基于 termenv OSC 11），而非读 Windows 注册表——终端背景通常随 OS，且 SSH 场景更正确。

### 4.2 桌面界面

**Wails 绑定与事件（`internal/desktop`）**

- `GetAppearance() -> {mode, resolved}`。
- `SetAppearanceMode(mode string) error`（桌面 App 方法）：用 `appearance.ParseMode` 校验 → 更新 `Options.Config.Appearance.Mode` → `config.Save(paths, cfg)` → 按需启停 `Watcher` → 广播。
- 事件 `appearance:changed`（`{mode, resolved}`）：设置变更或 OS 明暗变化时发出，驱动前端与原生窗口。
- App 启动时根据 mode 启动/停止 Watcher；退出时取消。

**CSS（`frontend/src/style.css`）**

- 将纯 `@media (prefers-color-scheme: dark)` 改为属性驱动：
  - `:root { color-scheme: light; ...浅色 token }`
  - `[data-theme="dark"] { color-scheme: dark; ...深色 token }`
- `color-scheme` 一并解决原生滚动条与表单控件配色。

**前端状态与订阅（`frontend/src/state/` + 应用挂载处）**

- Zustand store 保存 `{mode, resolved}`。
- 挂载时调用 `GetAppearance` 初始化 `<html data-theme>`，并 `EventsOn('appearance:changed')` 实时更新。
- 为减少首屏闪烁，`index.html` 内联脚本先按 `prefers-color-scheme` 设初始 `data-theme`，绑定返回后纠正。

**xterm（`frontend/src/components/TerminalView.tsx`）**

- `terminalTheme()` 改读解析后的 theme（替代 `matchMedia`）。
- 主题变化时更新已存在实例 `term.options.theme`。

**原生窗口（新增 `internal/desktop/appearance_windows.go` + `appearance_other.go`）**

- `DwmSetWindowAttribute(hwnd, DWMWA_USE_IMMERSIVE_DARK_MODE=20, dark?1:0)` 让窗口边框/阴影匹配。
- `runtime.WindowSetBackgroundColour` 设背景色，避免切换瞬间白/黑闪。
- 非 Windows 为空实现。

**Settings 页**：新增「外观」分段选择（跟随系统 / 浅色 / 深色），调用 `SetAppearanceMode`。

### 4.3 TUI（Bubbletea）

- `internal/ui/theme.go`：
  - 抽出两套调色板 `darkPalette`（即现有硬编码色）与新增 `lightPalette`（浅底深字，调整 accent/dim/fg）。
  - `NewTheme(mode appearance.Mode)`：`light/dark` 直接选；`system` 用 `lipgloss.HasDarkBackground()`；`NO_COLOR` 优先级最高 → `plainTheme()`。
- 接线：`cmd/kshell/main.go` 把 `cfg.Appearance.Mode` 传入，`internal/ui/app.go:136/143` 透传（移除写死的 `config.Default()` 兜底主题）。
- 实时切换：启动时解析一次，不做 TUI 内热重绘（留扩展点）。

### 4.4 agent 启动注入

**注入缝（不改各工具配置文件）**

- `providers.Launch` 增加 `Env map[string]string`。
- providers 可选接口：

  ```go
  type Themer interface {
      ThemeOverrides(theme appearance.Theme) (args []string, env map[string]string)
  }
  ```

- `internal/launch` 生成 `Launch` 后套用 provider 覆盖，并**始终**追加通用环境变量 `COLORFGBG`（深 `15;0` / 浅 `0;15`）+ `COLORTERM=truecolor`。
- 环境变量透传链路：
  - 内嵌终端：`desktop/terminal.go` 填 `terminal.Spec.Env` → `backend_pty.go`（`cmd.Env`）。
  - 外部窗口：`windowsLauncher.Launch` 给 `exec.Command` 设 `Env` → 其后 `powershell` 子进程继承。
  - TUI 路径：`internal/launcher/launcher.go` 给 `cmd.Env` 赋值。

**各工具映射（`system` 启动时解析一次）**

| 工具 | 注入方式 |
|---|---|
| claude | 参数 `--settings '{"theme":"light"\|"dark"}'`（会话级，不写文件） |
| codex | 参数 `-c tui.theme=catppuccin-latte`（浅）/ `catppuccin-mocha`（深） |
| gemini | env `GEMINI_CLI_SYSTEM_SETTINGS_PATH` → `gemini-<theme>.json`：`{"ui":{"theme":"Default Light"\|"Default","autoThemeSwitching":false}}` |
| opencode | env `OPENCODE_TUI_CONFIG` → `opencode-<theme>.json`：`{"theme":"system"}`（`system` 主题按承载终端背景自适应） |
| generic | 仅通用 `COLORFGBG` / `COLORTERM` |

- 生成的 JSON 落在 `~/.kshell/cache/appearance/`，按 theme 幂等写一次，**不触碰工具自身配置**。
- 已启动的 agent 不热切换（与「下次启动生效」一致）。

**已实测依据**（本机）：`claude --help` 有 `--settings <file-or-json>`；`codex --help` 有 `-c/--config <key=value>`，`-c tui.theme=dark` 经 `doctor` 接受；opencode 无 theme flag，主题属 `tui.json`，`OPENCODE_TUI_CONFIG` 可指向自定义文件；gemini 无 theme flag，主题属 `settings.json` 的 `ui.theme`，`GEMINI_CLI_SYSTEM_SETTINGS_PATH` 为最高优先级系统层。codex/gemini/opencode 主要靠 OSC 10/11 探测终端背景，Claude 亦读 `COLORFGBG`。

**限制**：外部 Windows Terminal 的窗口配色无法强制，opencode 的 `system` 主题依赖承载终端背景——强制模式与 OS 相反时，外部窗口可能不匹配；内嵌 xterm 完全受控。

## 5. 测试

- `appearance`：`ParseMode`/`Resolve` 表驱动；OS 检测经注入函数替换；`Watcher` 用假检测器 + 短 ticker 验证回调。
- `config`：非法 mode 回落 `system`；`Appearance` 字段经 `Save`/`Load` 往返；桌面 `SetAppearanceMode` 写回并触发事件。
- providers / launch：各 provider `ThemeOverrides` 参数与 env 断言；通用 `COLORFGBG`/`COLORTERM` 始终注入。
- `ui`：light/dark 调色板选择；保留 `app_test.go` 的 `NO_COLOR` 断言。
- 前端（vitest）：Settings 选择调用 `SetAppearanceMode`；`data-theme` 随事件更新；`TerminalView` 主题映射与热更新。
- 验证命令：`go test ./... -count=1`、`go vet ./...`、前端 `npm test` / `npm run build`。

## 6. 范围外（YAGNI）

- 不做每界面独立配置；不提供用户自定义配色主题（仅内置浅/深调色板）。
- 不修改任何工具自身配置文件；运行中的 agent / 外部窗口不热切换。
- TUI 不提供切换 UI（只读配置）；Settings 入口仅在桌面端。
- Windows 为主，macOS/Linux 的 OS 检测为 best-effort。

## 7. 依赖与文件清单

- 新增依赖：`golang.org/x/sys/windows/registry`（`x/sys` 已间接依赖，改为直接引用）。
- 新增：`internal/appearance/`（core + detect_windows/other + watcher + 测试）、`internal/desktop/appearance_windows.go` / `appearance_other.go`。
- 改动：`internal/config/{config.go}`、`internal/ui/{theme,app}.go`、`cmd/kshell/main.go`、`internal/launch/launch.go`、`internal/providers/*.go`、`internal/terminal/manager.go`（消费侧）、`internal/desktop/{app,terminal,window_win}.go`、`frontend/src/{style.css,components/TerminalView.tsx,pages/Settings.tsx,state/*,App.tsx}`、`frontend/index.html`。
