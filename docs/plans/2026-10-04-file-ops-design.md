# 文件区域操作增强（右键菜单 / 树内拖拽 / 拖入对话）设计

日期：2026-10-04
状态：已与用户确认

## 背景与目标

kshell 桌面端右栏文件区域（`frontend/src/components/FileTree.tsx`）目前仅支持点击预览、行内重命名、搜索。本设计为文件区域补齐常见文件操作，并支持把文件拖入对话（终端/聊天页签）自动转换为全路径文本，方便在对话中引用文件。

**范围：仅桌面端。** TUI（Bubbletea）不接收 OS 级拖放事件，不做。

## 需求清单（已确认）

1. **右键菜单**：新建文件 / 新建文件夹 / 重命名（复用现有行内重命名）/ 删除（确认框 + 永久删除）/ 复制路径
2. **树内拖拽**：拖文件或文件夹到目录行上移动（`MoveEntry`）
3. **拖入对话**：从文件树或系统资源管理器拖文件到**任意终端 + 聊天页签**，插入全路径文本（等效键入）
4. **删除策略**：弹确认框（显示文件名），确认后永久删除（`os.RemoveAll`），不进回收站

## 后端设计（internal/desktop/files.go）

新增三个绑定方法，与现有 `RenameEntry` 同层：

| 方法 | 签名 | 说明 |
|---|---|---|
| `CreateEntry` | `(dir, name string, isDir bool) error` | 新建文件/目录；name 不含路径分隔符；已存在报错 |
| `DeleteEntry` | `(path string) error` | 永久删除（`os.RemoveAll`） |
| `MoveEntry` | `(src, dst string) error` | 树内移动（亦可用于跨目录重命名）；dst 已存在报错 |

**共同安全约束**：

- 路径必须解析后落在当前 workspace root 内（防 `..` 逃逸），与 `RenameEntry` 既有做法对齐
- 删除/移动/新建成功后复用 `RenameEntry` 现有的缓存作废逻辑；前端实现采用各调用方 `refreshRoot()` 重建树镜像（桌面端单窗口下等价，未引入刷新事件链路）
- Windows 大小写不敏感：`src` 与 `dst` 大小写折叠后相同则直接跳过（no-op）

**无需新后端的能力**：复制路径走前端 `navigator.clipboard.writeText`；插入终端走现有 `writeTerminal(id, base64)`（`frontend/src/lib/api.ts:487`，即键盘输入通道）。

## 前端设计

### 右键菜单（FileTree）

- 手写全局 `ContextMenu` 组件（仿 `NewSessionMenu` 定位思路，**不新增** dropdown-menu 依赖）
- 状态：`{x, y, entry} | null`，`TreeRow` 与树空白区 `onContextMenu` 写入并 `preventDefault`；点击别处 / Esc 关闭
- 菜单项按落点区分：
  - 目录行：新建文件 / 新建文件夹（该目录内）/ 重命名 / 删除 / 复制路径
  - 文件行：重命名 / 删除 / 复制路径
  - 空白处：新建文件 / 新建文件夹（树根目录）
- 新建走行内编辑（复用现有重命名行内输入样式），回车确认、Esc 取消
- 删除复用现有 Radix dialog 确认框；复制路径失败时 toast 提示

### 树内拖拽（原生 HTML5 DnD，不引第三方库）

- `TreeRow` 加 `draggable`；`dragstart` 设 `dataTransfer.setData("application/x-kshell-path", 绝对路径)`
- 目录行 `dragover` 时 `preventDefault` + 高亮边框；`drop` 调 `MoveEntry(src, dstDir/原名)`
- 拖到自身或自身子孙目录上时忽略（防死循环）
- 树内移动与拖出终端共用同一 dragstart，由落点决定行为

### 拖入终端 / 聊天页签

**内部拖入（文件树）**：中栏页签正文区（TerminalView / ChatView 容器）加 `onDragOver` / `onDrop`：

- `dragover` 检查 `dataTransfer.types` 含 `application/x-kshell-path`，`preventDefault` + 半透明遮罩提示"松开插入路径"
- `drop`：终端页签 → `writeTerminal(当前终端id, encodeTerminalInput(路径))`；聊天页签 → 路径追加到 ChatView 输入框光标处
- 插入后自动聚焦该页签
- 路径含空格自动包双引号（如 `D:\my file\a.go` → `"D:\my file\a.go"`）

**外部拖入（资源管理器）**：浏览器 `dataTransfer.files` 拿不到真实全路径，走 Wails 运行时：

- 应用挂载时 `OnFileDrop(callback, false)`（`useDropTarget=false` 关闭 Wails 自带全窗口遮罩；API 已确认存在于 `frontend/wailsjs/runtime/runtime.d.ts`）
- 回调 `(x, y, paths)`：用 `document.elementFromPoint(x, y)` 判断落点是否在终端/聊天容器内（容器挂 `data-drop-zone` 标记），命中才插入，否则忽略
- Wails 只拦截含真实文件的拖放，树内 HTML5 拖拽不受影响，两条通道天然不冲突

**页签条落点**：拖到未激活页签标题上，先切换到该终端再插入，避免"拖到了但插进后台会话"的困惑。

## 错误处理

统一 toast 提示，沿用现有错误展示方式：

- 后端：目标已存在 → "已存在同名条目"；路径逃逸 workspace root → 拒绝；删除失败（文件被占用）→ 原样返回错误
- 前端：树不做乐观更新，失败仅 toast，无需回滚

## 测试策略

- **Go**：`internal/desktop/files_test.go` 补 `CreateEntry` / `DeleteEntry` / `MoveEntry` 单测（含路径逃逸、目标已存在、大小写同名用例），临时目录构造 workspace
- **前端**：ContextMenu 渲染与动作分发；树内 drop 的 `MoveEntry` 参数拼接；终端 drop 的编码与引号包裹逻辑（vitest，mock api）
- **手工冒烟**：补 `docs/smoke-test*.md` 条目（右键菜单五项、树内移动、拖入终端/聊天、外部拖入）

## 验证命令

```powershell
go build ./... ; go vet ./... ; go test ./... -count=1
cd frontend; npm test; npm run build
```

全绿后合并回 master，`.\build.ps1 -Desktop` 出可运行桌面版（构建前征得用户同意再请求旧实例退出）。
