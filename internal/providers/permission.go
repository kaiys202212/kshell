package providers

// PermissionInjector 可选接口：按权限模式追加启动参数（仅 bypass 时有内容）。
type PermissionInjector interface {
	InjectPermission(bypass bool) (args []string, env map[string]string)
}

// Claude：bypass 时追加 --dangerously-skip-permissions。
func (Claude) InjectPermission(bypass bool) ([]string, map[string]string) {
	if !bypass {
		return nil, nil
	}
	return []string{"--dangerously-skip-permissions"}, nil
}

// Codex：bypass 时跳过审批提示，保留 sandbox（不用 --yolo）。
func (Codex) InjectPermission(bypass bool) ([]string, map[string]string) {
	if !bypass {
		return nil, nil
	}
	return []string{"--ask-for-approval", "never"}, nil
}

// Gemini：无稳定 skip 权限 flag，v1 no-op。
func (Gemini) InjectPermission(bool) ([]string, map[string]string) { return nil, nil }

// Opencode：无稳定 skip 权限 flag，v1 no-op。
func (Opencode) InjectPermission(bool) ([]string, map[string]string) { return nil, nil }
