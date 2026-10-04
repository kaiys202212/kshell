# 实现计划：设置页布局 + 模型预设 + 会话/权限模式

日期：2026-10-04  
分支：`feat/settings-model-presets`  
设计：`2026-10-04-settings-model-presets-design.md`

## Task 1：配置层

- 扩展 `internal/config/config.go`：默认 dark、session_mode、permission_mode、双 BaseURL + 旧字段迁移
- 测试：默认值、非法枚举回落、base_url 迁移、round-trip

## Task 2：预设表 + InjectModel

- `internal/providers/model_presets.go`
- 扩展 `model.go` 双 URL 注入；更新 model_test / launch_test
- desktop `Get/SetModelConfig` + `ListModelPresets`；更新 resolver

## Task 3：会话模式

- `OpenSession`/`OpenWorkspace` 按偏好分流
- 显式 ACP 绑定；前端菜单「以 ACP 打开」

## Task 4：权限 bypass

- `InjectPermission` + launch `PermissionOptions`
- chat.Manager 自动放行

## Task 5：设置页 UI

- Settings 分区布局 + 预设表单 + 会话/权限控件
- api.ts + 测试

## Task 6：验证

```powershell
go build ./... ; go vet ./... ; go test ./... -count=1
cd frontend; npm test; npm run build
.\build.ps1 -Desktop
```

冒烟：`docs/smoke/feat-settings-model-presets.md`
