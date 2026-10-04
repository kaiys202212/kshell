# CodeBuddy 内置 + 自定义工具表单 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans（任务强耦合：同一设置页与同一 SaveProvidersYAML 热重载）。Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** CodeBuddy 成为带 npm 安装配方的内置 agent；自定义工具用表单+源码编辑，保存后热重载。

**Architecture:** `CodeBuddy` 进 `Builtins()` 并委托已实测 GenericSpec 做会话；`SaveProvidersYAML` 写盘后 `MergeProviders` 替换运行中的 provider 表并重扫。设置页表单与 YAML 通过 `ParseProvidersYAML` / `FormatProvidersYAML` 互转；路径用 Wails 文件/目录对话框。

**Tech Stack:** Go 1.23、providers/discovery/desktop、React 19 + vitest、Wails v2 runtime dialogs。

## Global Constraints

- 工作目录：`D:\data\workspace\moxi\kshell\.worktrees\feat\codebuddy-providers-form`
- 中文注释与 `type: 简述` 提交；不 push
- TDD：先写失败测试再写实现
- 自定义 Generic 不实现 Installer
- 切表单/源码不保留 yaml 注释
- 前端不得传入安装命令字符串
- 不终止已运行的 kshell-desktop

---

### Task 1: CodeBuddy 内置 provider + 安装配方 + 默认 yaml

**Files:**
- Create: `internal/providers/codebuddy.go`
- Create: `internal/providers/codebuddy_test.go`
- Modify: `internal/providers/builtins.go`
- Modify: `internal/providers/builtins_test.go`
- Modify: `internal/providers/generic.go`（`DefaultProvidersYAML` 去掉 codebuddy 段）
- Modify: `internal/providers/generic_test.go`（默认模板断言）
- Modify: `internal/providers/install_test.go`（CodeBuddy 配方；Generic 拒绝用例改用不冲突 ID）

**Interfaces:**
- Produces: `type CodeBuddy struct{}` 实现 `Provider` + `Installer` + `PathMatcher`（经 Generic 委托）
- `codeBuddySpec` 与现 `codebuddyYAML` 字段一致
- `InstallRecipe`：`npm install -g @tencent-ai/codebuddy-code`，PurgeDirs `~/.codebuddy`

- [ ] **Step 1: 写失败测试** `codebuddy_test.go`

```go
func TestCodeBuddyIdentityAndDetect(t *testing.T) {
	p := CodeBuddy{}
	if p.ID() != "codebuddy" || p.DisplayName() != "CodeBuddy" {
		t.Fatalf("id/name = %s %s", p.ID(), p.DisplayName())
	}
	spec := p.DetectSpec("")
	if spec.BinName != "codebuddy" {
		t.Fatalf("bin = %q", spec.BinName)
	}
	if len(spec.AltBinNames) != 1 || spec.AltBinNames[0] != "cbc" {
		t.Fatalf("alt = %v", spec.AltBinNames)
	}
	if len(spec.ConfigDirs) != 1 || spec.ConfigDirs[0] != "~/.codebuddy" {
		t.Fatalf("dirs = %v", spec.ConfigDirs)
	}
}

func TestCodeBuddyResumeAndMatch(t *testing.T) {
	p := CodeBuddy{}
	gHome := t.TempDir()
	_ = gHome
	launch := p.ResumeCmd(Session{ID: "cb-1", Workspace: `D:\ws`}, "codebuddy")
	if len(launch.Args) != 2 || launch.Args[0] != "--resume" || launch.Args[1] != "cb-1" {
		t.Fatalf("args = %v", launch.Args)
	}
	if !p.MatchSessionRel(`ws-1/01a0.jsonl`) {
		t.Fatal("session file must match")
	}
	if p.MatchSessionRel(`ws-1/01a0.jsonl/subagents/agent-1.jsonl`) {
		t.Fatal("subagent must not match")
	}
}

func TestCodeBuddyParsesSummaryTitle(t *testing.T) {
	content := `{"type":"session-meta","id":"m-1","sessionId":"cb-9","timestamp":1790242904766,"cwd":"D:\\ws\\demo"}` + "\n" +
		`{"id":"m-3","timestamp":1790243000000,"type":"summary","summary":"设计 RPC 任务架构","providerData":{"source":"initial-user-message"}}` + "\n"
	got, err := (CodeBuddy{}).ParseSession("cb.jsonl", []byte(content))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "cb-9" || got.Title != "设计 RPC 任务架构" {
		t.Fatalf("got %+v", got)
	}
}

func TestCodeBuddyRecipeNpm(t *testing.T) {
	r, ok := RecipeOf(CodeBuddy{})
	if !ok {
		t.Fatal("missing recipe")
	}
	if !strings.Contains(r.InstallCmd, "@tencent-ai/codebuddy-code") {
		t.Fatalf("install = %q", r.InstallCmd)
	}
	if len(r.PurgeDirs) != 1 || r.PurgeDirs[0] != "~/.codebuddy" {
		t.Fatalf("purge = %v", r.PurgeDirs)
	}
}

func TestDefaultYAMLOmitsCodeBuddyID(t *testing.T) {
	if strings.Contains(DefaultProvidersYAML(), "id: codebuddy") {
		t.Fatal("default yaml must not declare builtin codebuddy")
	}
	if !strings.Contains(DefaultProvidersYAML(), "id: cline") {
		t.Fatal("cline preset must remain")
	}
}

func TestMergeProvidersPrefersBuiltinCodeBuddy(t *testing.T) {
	specs := []GenericSpec{{ID: "codebuddy", Name: "YAML 覆盖名"}}
	ps := MergeProviders(Builtins(), specs, `C:\home`)
	var p Provider
	for _, x := range ps {
		if x.ID() == "codebuddy" {
			p = x
		}
	}
	if _, ok := p.(CodeBuddy); !ok {
		t.Fatalf("got %T", p)
	}
	if p.DisplayName() != "CodeBuddy" {
		t.Fatalf("name = %q", p.DisplayName())
	}
}
```

同时改 `TestBuiltinsCoverKnownTools` 加入 `codebuddy`；`TestMergeProvidersSkipsBuiltinIDs` 断言 `ps` 中 codebuddy 是 `CodeBuddy` 而非 Generic；`TestDefaultProvidersYAMLCoversPresetTools` 不再要求 `codebuddy` 字符串作为 provider id（可改为注释里提到 builtin 即可，或不包含）；`TestGenericHasNoInstaller` 把 ID 改成 `cline`。

- [ ] **Step 2: 跑测试确认失败**

```
go test ./internal/providers -count=1 -run "TestCodeBuddy|TestDefaultYAMLOmits|TestMergeProvidersPrefers|TestBuiltinsCover|TestBuiltinsImplement"
```

Expected: FAIL（`CodeBuddy` 未定义或未进 Builtins）

- [ ] **Step 3: 实现**

`builtins.go`：`return []Provider{Claude{}, Codex{}, Cursor{}, CodeBuddy{}, Gemini{}, Opencode{}}`

`codebuddy.go`：内部 `generic(home string) Generic` 返回 `Generic{Spec: codeBuddySpec, Home: home}`；`DetectSpec` 单独写 BinName/Alt/ConfigDirs；其余 SessionRoots/Pattern/Parse/Match/New/Resume 委托 `generic(home)`（Parse/Match 用 `generic("")` 或传入的 home——`MatchSessionRel` Generic 用 `g.Home` 展开 glob，测试不依赖 home 时用空 home 与绝对 glob 展开）。注意 `CodeBuddy.MatchSessionRel` 必须设 `Generic.Home`：用 `generic("").MatchSessionRel` 即可，因为 glob 以 `~` 开头，空 home 时 `expandTilde` 不替换，`rootFromGlob` 仍能算相对 glob。

`DefaultProvidersYAML`：删除整个 codebuddy 列表项，保留 cline 与「opencode 已内置」注释，可加一行「codebuddy 已是内置」。

- [ ] **Step 4: 测试全绿** `go test ./internal/providers -count=1`

- [ ] **Step 5: Commit** `feat: 将 CodeBuddy 作为内置 agent`

---

### Task 2: FormatProvidersYAML + 保存后热重载 + 路径选择绑定

**Files:**
- Modify: `internal/providers/generic.go` / `generic_test.go`
- Modify: `internal/desktop/settings.go` / `files_ssh_settings_test.go`
- Modify: `internal/desktop/projects.go`（抽出或并列 `pickFile`）
- Create or modify: `internal/desktop/pickpath.go`（`pickDirectory` 迁入此处以免 settings 依赖项目）

**Interfaces:**
- `FormatProvidersYAML(specs []GenericSpec) (string, error)`
- `(a *App) PickDirectory(title string) (string, error)`
- `(a *App) PickFile(title string) (string, error)`
- `SaveProvidersYAML` 成功后更新 `a.opts.Providers` 并 `DetectAll` + `ScanSessions`

- [ ] **Step 1: 测试**

`TestFormatProvidersYAMLRoundTrip`：Parse → Format → Parse，id/name 保留。

`TestSaveProvidersYAMLReloadsProviders`：`NewAppWith` 设 `Home`、`ProvidersPath`、`Providers: providers.Builtins()`（或空然后 reload）、`Scan` 可 stub。保存含 `id: demo-agent` 的 yaml 后，snapshot 的 Providers 含该 ID，且类型为 Generic。

`TestPickDirectoryCancel`：注入 `pickDirectory` 返回 `""`，`PickDirectory` 返回 `""` 且 error nil。

- [ ] **Step 2: 跑红** `go test ./internal/providers ./internal/desktop -count=1 -run "TestFormatProvidersYAML|TestSaveProvidersYAMLReload|TestPick"`

- [ ] **Step 3: 实现**

`yaml.NewEncoder` `SetIndent(2)` encode `genericSpecFile`。

`reloadProviders`：`LoadGenericSpecs` 失败则忽略自定义（空 specs）；`MergeProviders`；锁内写 `opts.Providers`；`DetectAll` + `publishDetectedTools`；`ScanSessions()`。

`pickFile` 用 `OpenFileDialog`。

把现有 `pickDirectory` 从 `projects.go` 挪到 `pickpath.go` 以免重复，`CreateProject` 继续调用同一变量。

- [ ] **Step 4: 测试绿** 含原 `TestSaveAndLoadProvidersYAML`

- [ ] **Step 5: Commit** `feat: 自定义工具保存后热重载并支持选路径`

---

### Task 3: 前端表单 + 源码模式 + 设置页测试

**Files:**
- Create: `frontend/src/lib/providersForm.ts` / `providersForm.test.ts`
- Create: `frontend/src/components/ProvidersEditor.tsx`（若 Settings 过大则抽出）
- Modify: `frontend/src/lib/api.ts`（`pickDirectory`/`pickFile`/`format` 若走 Go；表单侧也可用纯 TS format 但必须以 Go 保存的 yaml 为准——前端保存仍提交 yaml 字符串）
- Modify: `frontend/src/pages/Settings.tsx`
- Modify: `frontend/src/pages/Settings.test.tsx`
- Create: `docs/smoke/feat-codebuddy-providers-form.md`

**Interfaces:**
- TS `CustomProviderSpec` 对齐 GenericSpec
- `parseProvidersYAML`/`formatProvidersYAML`：前端用轻量实现或调用 Go。为避免新增前端 yaml 依赖：表单状态用对象数组；切到源码时调用 Go `FormatProvidersYAML`；从源码切回调用 Go `ParseProvidersYAML`。Wails 绑定：在 App 上包一层：

```go
func (a *App) ParseProvidersYAML(content string) ([]providers.GenericSpec, error)
func (a *App) FormatProvidersYAML(specs []providers.GenericSpec) (string, error)
```

测试里 mock 这两个 + pick 函数。

`sessionGlobFromDir(dir: string): string` → 斜杠规范化 + `/*/*.jsonl`

- [ ] **Step 1: 前端单测先失败**

`providersForm.test.ts`：`sessionGlobFromDir('C:\\Users\\a\\.foo\\projects') === 'C:/Users/a/.foo/projects/*/*.jsonl'`

Settings 测试更新：
- 默认进入表单模式，能「添加工具」、填 id、保存时 `saveProvidersYAML` 被调用
- 点「源码」出现 `providers.yaml 编辑器`
- 保存成功文案匹配 `/已保存/` 且不含「重启」；无「立即重启」
- CodeBuddy 在 recipes mock 中有条目时显示「卸载」；`mytool` 仍无按钮

原先「提示重启生效 / 立即重启」用例改为热加载文案。

- [ ] **Step 2: `cd frontend; npm test` 红**

- [ ] **Step 3: 实现 UI**

字段：id, name, detect.command（按钮「选择文件」）, detect.dirs 列表（「选择文件夹」）, sessions.glob（「选择会话目录」）, sessions.format select jsonl|json, fields, resume args 用空格分隔一行, verified checkbox

空列表时保存 `providers: []`（format 空切片）。

- [ ] **Step 4: `npm test` 与 `npm run build` 绿**

- [ ] **Step 5: Commit** `feat: 自定义工具表单与源码编辑`

---

### Task 4: 冒烟文档与回归

- [ ] 追加 `docs/smoke/feat-codebuddy-providers-form.md`
- [ ] `go vet ./...`；`go test ./... -count=1`；`cd frontend; npm test`
- [ ] Commit `docs: CodeBuddy 内置与自定义表单冒烟`
