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

// Gemini：bypass 时注入 --approval-mode yolo（官方统一 flag，--yolo 已弃用；
// 已知副作用：yolo 模式默认连带启用 sandbox，shell 命令受沙箱限制）。
func (Gemini) InjectPermission(bypass bool) ([]string, map[string]string) {
	if !bypass {
		return nil, nil
	}
	return []string{"--approval-mode", "yolo"}, nil
}

// Cursor：bypass 时注入 --force（force allow 未显式 deny 的命令；deny 列表仍生效）。
func (Cursor) InjectPermission(bypass bool) ([]string, map[string]string) {
	if !bypass {
		return nil, nil
	}
	return []string{"--force"}, nil
}

// CodeBuddy：bypass 时注入 --permission-mode bypassPermissions
//（-y/--dangerously-skip-permissions 仍会询问 HIGH/CRITICAL，故选 bypass 模式）。
func (CodeBuddy) InjectPermission(bypass bool) ([]string, map[string]string) {
	if !bypass {
		return nil, nil
	}
	return []string{"--permission-mode", "bypassPermissions"}, nil
}

// Opencode：bypass 时注入 --auto（自动放行未显式 deny 的权限，opencode 1.x 根级 flag）。
func (Opencode) InjectPermission(bypass bool) ([]string, map[string]string) {
	if !bypass {
		return nil, nil
	}
	return []string{"--auto"}, nil
}
