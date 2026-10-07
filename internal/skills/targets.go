package skills

import "path/filepath"

// targetRel 是各 tool 用户级 skills 根相对 home 的路径段。
var targetRel = map[string][]string{
	"claude":    {".claude", "skills"},
	"cursor":    {".cursor", "skills"},
	"codex":     {".agents", "skills"},
	"gemini":    {".gemini", "skills"},
	"opencode":  {".config", "opencode", "skills"},
	"codebuddy": {".codebuddy", "skills"},
}

// TargetRoot 返回 toolID 对应用户级 skills 根目录；未知 tool 返回 false。
func TargetRoot(toolID, home string) (string, bool) {
	rel, ok := targetRel[toolID]
	if !ok || home == "" {
		return "", false
	}
	parts := append([]string{home}, rel...)
	return filepath.Join(parts...), true
}

// KnownToolIDs 返回支持 skill 安装的内置 tool 列表（稳定顺序）。
func KnownToolIDs() []string {
	return []string{"claude", "cursor", "codex", "gemini", "opencode", "codebuddy"}
}
